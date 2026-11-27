package live_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// fakeSbx answers sbx commands from fixture JSON, keyed by the joined
// arguments. A key mapped to an error fails that command; a missing key
// fails like an unknown command would.
type fakeSbx struct {
	out   map[string]string
	fail  map[string]error
	calls []string
}

func (f *fakeSbx) Run(_ context.Context, args ...string) ([]byte, error) {
	k := strings.Join(args, " ")
	f.calls = append(f.calls, k)
	if err, ok := f.fail[k]; ok {
		return nil, err
	}
	if s, ok := f.out[k]; ok {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("unexpected command %q", k)
}

// fixture reads testdata/NAME and replaces {{WS}} and {{OTHER}}.
func fixture(t *testing.T, name string, vars map[string]string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

// world is a project directory with an environment file and a fake sbx.
type world struct {
	dir, ws, other string
	sbx            *fakeSbx
	// strict resolves the plans with strict redaction (--redact, --json).
	strict bool
}

func newWorld(t *testing.T, env string) *world {
	t.Helper()
	dir := t.TempDir()
	ws := filepath.Join(dir, "web-app")
	other := filepath.Join(dir, "elsewhere")
	for _, d := range []string{ws, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if env != "" {
		if err := os.WriteFile(filepath.Join(dir, "sbxenv.yaml"), []byte(env), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vars := map[string]string{"WS": ws, "OTHER": other}
	return &world{dir: dir, ws: ws, other: other, sbx: &fakeSbx{
		out: map[string]string{
			"ls --json":                          fixture(t, "ls.json", vars),
			"policy ls web-app --json":           fixture(t, "policy-web-app.json", vars),
			"policy ls orphan --json":            `{"rules": []}`,
			"secret ls --sandbox web-app --json": fixture(t, "secrets-web-app.json", vars),
			"secret ls --sandbox orphan --json":  `{"secrets": [], "custom_secrets": [], "shadowed_services": [], "env_only_count": 0}`,
			"mcp ls --json":                      fixture(t, "mcp.json", vars),
		},
		fail: map[string]error{},
	}}
}

func (w *world) redaction() sbxenv.Redaction {
	if w.strict {
		return sbxenv.RedactStrict
	}
	return sbxenv.RedactHome
}

func (w *world) resolve(g live.Group) (*sbxenv.Plan, error) {
	res, err := loader.Load(loader.Request{Paths: g.Paths, WorkDir: g.Dir, HomeDir: filepath.Join(w.dir, "no-home")})
	if err != nil {
		return nil, err
	}
	return sbxenv.Resolve(res.Layers, sbxenv.ResolveOptions{ProjectDir: res.ProjectDir, HomeDir: filepath.Join(w.dir, "no-home"), Kits: loader.FSKits{}, Redaction: w.redaction()})
}

func (w *world) discover(t *testing.T, r live.Runner) []live.Target {
	t.Helper()
	targets, _ := live.Discover(context.Background(), live.DiscoverRequest{WorkDir: w.dir, Runner: r, Resolve: w.resolve, Redaction: w.redaction()})
	return targets
}

func find(t *testing.T, targets []live.Target, name string) live.Target {
	t.Helper()
	for _, x := range targets {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no target %q in %v", name, names(targets))
	return live.Target{}
}

func names(ts []live.Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name+"("+t.State+")")
	}
	return out
}

// drift returns the drift whose title contains s.
func drift(c *live.Comparison, s string) (live.Drift, bool) {
	for _, d := range c.Drifts {
		if strings.Contains(d.Title, s) {
			return d, true
		}
	}
	return live.Drift{}, false
}

// matching is a declaration that agrees with the web-app fixture.
const matching = `schemaVersion: "1"
name: web-app
agent: claude
workspace: ./web-app
ports:
  - {sandbox: 3000, host: 4000}
secrets:
  github:
    command: gh auth token
mcp:
  servers:
    - {name: docs, url: "https://mcp.example.com/sse"}
`

// --- collecting --------------------------------------------------------------

func TestCollectFallsBackWhenSbxIsMissing(t *testing.T) {
	t.Parallel()
	// Arrange
	r := &fakeSbx{fail: map[string]error{"ls --json": live.ErrNoSbx}}

	// Act
	s := live.Collect(context.Background(), r)

	// Assert
	if s.Available || !strings.Contains(s.Problem, "sbx コマンドが見つからない") {
		t.Errorf("snapshot = %+v", s)
	}
	if len(r.calls) != 1 {
		t.Errorf("nothing else should run without sbx: %v", r.calls)
	}
}

func TestCollectFallsBackWhenTheDaemonFails(t *testing.T) {
	t.Parallel()
	// Arrange
	r := &fakeSbx{fail: map[string]error{"ls --json": errors.New("exit status 1: cannot connect to sandboxd")}}

	// Act
	s := live.Collect(context.Background(), r)

	// Assert
	if s.Available || !strings.Contains(s.Problem, "デーモンが停止") || !strings.Contains(s.Problem, "cannot connect") {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestCollectWithANilRunnerIsOffline(t *testing.T) {
	t.Parallel()
	// Act
	s := live.Collect(context.Background(), nil)

	// Assert
	if s.Available {
		t.Error("a nil runner must not report live data")
	}
}

func TestCollectReadsSandboxesPoliciesSecretNamesAndMCP(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, "")

	// Act
	s := live.Collect(context.Background(), w.sbx)

	// Assert
	if !s.Available || len(s.Sandboxes) != 2 || !s.MCPKnown {
		t.Fatalf("snapshot = %+v", s)
	}
	web := s.Sandboxes[1]
	if web.Name != "web-app" || web.Agent != "claude" || web.Status != "running" || len(web.Workspaces) != 1 || web.Workspaces[0].Path != w.ws {
		t.Errorf("web-app = %+v", web)
	}
	if len(web.Ports) != 1 || web.Ports[0] != (live.Port{HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp"}) {
		t.Errorf("ports = %+v", web.Ports)
	}
	if !web.RulesKnown || len(web.Rules) == 0 || !web.SecretsKnown || strings.Join(web.Secrets, ",") != "github" {
		t.Errorf("rules %v (%v), secrets %v (%v)", web.Rules, web.RulesKnown, web.Secrets, web.SecretsKnown)
	}
	var rules []string
	for _, r := range web.Rules {
		rules = append(rules, r.Decision+" "+r.Resource+" "+r.CreatedVia)
	}
	if got := strings.Join(rules, ", "); got != "allow api.anthropic.com default, allow api.example.com provisioned, allow manual.example.com added" {
		t.Errorf("network rules = %s (filesystem and inactive rules must be left out)", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", s), "do-not-read-me") {
		t.Error("a secret value reached the snapshot")
	}
	if orphan := s.Sandboxes[0]; orphan.Workspaces[0].Path != w.other || orphan.Status != "stopped" {
		t.Errorf("string workspaces should be read too: %+v", orphan)
	}
}

func TestCollectAcceptsTheOlderObjectShape(t *testing.T) {
	t.Parallel()
	// Arrange: before v0.48.0 `sbx ls --json` wrapped the array.
	r := &fakeSbx{out: map[string]string{
		"ls --json":          `{"sandboxes": [{"name": "a", "agent": "claude", "status": "running", "ports": [{"host_ip": "0.0.0.0", "host_port": 8080, "sandbox_port": 80, "protocol": "tcp"}]}]}`,
		"policy ls a --json": `{"rules": []}`, "secret ls --sandbox a --json": `{"secrets": []}`, "mcp ls --json": `[]`,
	}}

	// Act
	s := live.Collect(context.Background(), r)

	// Assert
	if len(s.Sandboxes) != 1 || len(s.Sandboxes[0].Ports) != 1 || s.Sandboxes[0].Ports[0].HostIP != "0.0.0.0" || s.Sandboxes[0].Ports[0].SandboxPort != 80 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestCollectMarksWhatItCouldNotRead(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, "")
	w.sbx.fail["policy ls web-app --json"] = errors.New("boom")
	w.sbx.fail["mcp ls --json"] = errors.New("boom")

	// Act
	s := live.Collect(context.Background(), w.sbx)

	// Assert
	if !s.Available || s.Sandboxes[1].RulesKnown || s.MCPKnown || !strings.Contains(s.Problem, "通信ルール") {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestCollectEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	// Arrange
	r := &fakeSbx{out: map[string]string{
		"ls --json": `[{"name": "x\u001b[2Jy", "status": "running"}]`, "policy ls x\x1b[2Jy --json": `{"rules": []}`,
		"secret ls --sandbox x\x1b[2Jy --json": `{"secrets": []}`, "mcp ls --json": "[]",
	}}

	// Act
	s := live.Collect(context.Background(), r)

	// Assert
	if strings.ContainsRune(fmt.Sprint(s.Sandboxes), 0x1b) {
		t.Errorf("escape reached the snapshot: %+v", s.Sandboxes)
	}
}

// --- discovery and pairing -----------------------------------------------------

func TestDiscoverPairsByNameAndListsLiveOnlySandboxes(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, matching)

	// Act
	targets := w.discover(t, w.sbx)

	// Assert
	if len(targets) != 2 {
		t.Fatalf("targets = %v", names(targets))
	}
	web := find(t, targets, "web-app")
	if web.State != "稼働中" || web.Plan == nil || web.Sandbox == nil || web.Comparison == nil {
		t.Errorf("web-app = %+v", web)
	}
	orphan := find(t, targets, "orphan")
	if orphan.Plan != nil || orphan.State != "停止中・宣言ファイル未検出" {
		t.Errorf("orphan = %+v", orphan)
	}
	if targets[0].Name != "web-app" {
		t.Errorf("paired targets come first: %v", names(targets))
	}
}

func TestDiscoverPairsByWorkspaceWhenNoNameIsDeclared(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, strings.Replace(matching, "name: web-app\n", "", 1))

	// Act
	targets := w.discover(t, w.sbx)

	// Assert
	if web := find(t, targets, "web-app"); web.Plan == nil || web.Sandbox == nil {
		t.Errorf("web-app should pair by workspace: %+v", web)
	}
}

func TestDiscoverListsDeclarationsNotYetCreated(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, matching)
	if err := os.WriteFile(filepath.Join(w.dir, "sbxenv.api.yaml"), []byte("schemaVersion: \"1\"\nname: api\nagent: codex\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	targets := w.discover(t, w.sbx)

	// Assert
	if api := find(t, targets, "api"); api.State != "宣言のみ（未作成）" || api.Sandbox != nil {
		t.Errorf("api = %+v", api)
	}
}

func TestDiscoverWithoutSbxShowsTheFilesOnly(t *testing.T) {
	t.Parallel()
	// Arrange
	w := newWorld(t, matching)
	r := &fakeSbx{fail: map[string]error{"ls --json": live.ErrNoSbx}}

	// Act
	targets, snap := live.Discover(context.Background(), live.DiscoverRequest{WorkDir: w.dir, Runner: r, Resolve: w.resolve})

	// Assert
	if len(targets) != 1 || targets[0].Sandbox != nil || targets[0].Plan == nil || targets[0].State != "宣言" {
		t.Errorf("targets = %v", names(targets))
	}
	if snap.Available || snap.Problem == "" {
		t.Errorf("the problem should be reported: %+v", snap)
	}
}

func TestDiscoverFindsFilesNextToASandboxWorkspace(t *testing.T) {
	t.Parallel()
	// Arrange: the env file sits beside the workspace, not in the working
	// directory the command runs from.
	w := newWorld(t, "")
	if err := os.WriteFile(filepath.Join(w.dir, "sbxenv.yaml"), []byte(matching), 0o644); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()

	// Act
	targets, _ := live.Discover(context.Background(), live.DiscoverRequest{WorkDir: elsewhere, Runner: w.sbx, Resolve: w.resolve})

	// Assert
	if web := find(t, targets, "web-app"); web.Plan == nil {
		t.Errorf("the declaration beside the workspace was not found: %v", names(targets))
	}
}

func TestFileGroupsPairEachFileWithItsLocalCompanion(t *testing.T) {
	t.Parallel()
	// Arrange
	dir := t.TempDir()
	for _, n := range []string{"sbxenv.yaml", "sbxenv.dev.yaml", "sbxenv.dev.local.yaml", "sbxenv.ci.local.yaml", "other.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	groups := live.FileGroups(dir)

	// Assert
	var got []string
	for _, g := range groups {
		var files []string
		for _, p := range g.Paths {
			files = append(files, filepath.Base(p))
		}
		got = append(got, "["+strings.Join(files, "+")+"]")
	}
	want := "[sbxenv.dev.yaml+sbxenv.dev.local.yaml] [] [sbxenv.ci.local.yaml]"
	if strings.Join(got, " ") != want {
		t.Errorf("groups = %s, want %s", strings.Join(got, " "), want)
	}
}

// --- drift -----------------------------------------------------------------

func compare(t *testing.T, env string, edit func(*world)) *live.Comparison {
	t.Helper()
	w := newWorld(t, env)
	if edit != nil {
		edit(w)
	}
	web := find(t, w.discover(t, w.sbx), "web-app")
	if web.Comparison == nil {
		t.Fatalf("web-app was not compared: %+v", web)
	}
	return web.Comparison
}

func TestAMatchingSandboxHasNoDrift(t *testing.T) {
	t.Parallel()
	// Act
	c := compare(t, matching, nil)

	// Assert: only the manual network rule of the fixture differs.
	for _, d := range c.Drifts {
		if !strings.Contains(d.Title, "宣言にない通信許可") {
			t.Errorf("unexpected drift %+v", d)
		}
	}
	for _, area := range []string{"エージェント", "ワークスペース・マウント", "ポート公開", "通信の許可・拒否", "シークレット（名前のみ）", "MCP サーバー"} {
		if !strings.Contains(strings.Join(c.Checked, ","), area) {
			t.Errorf("%s was not compared: %v", area, c.Checked)
		}
	}
	if !strings.Contains(strings.Join(c.Skipped, ""), "メモリ・CPU") {
		t.Errorf("what cannot be compared must be said: %v", c.Skipped)
	}
}

func TestDriftAgent(t *testing.T) {
	t.Parallel()
	// Act
	c := compare(t, strings.Replace(matching, "agent: claude", "agent: codex", 1), nil)

	// Assert
	d, ok := drift(c, "違うエージェント")
	if !ok || d.Declared != "codex" || d.Actual != "claude" || !strings.Contains(d.Fix, "sbx env rm") {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
}

func TestDriftWorkspaceMissingAndExtra(t *testing.T) {
	t.Parallel()
	// Arrange: the declaration names another directory than the sandbox mounts.
	env := strings.Replace(matching, "workspace: ./web-app", "workspace: ./web-app\nadditionalWorkspaces:\n  - {path: ./elsewhere, readOnly: true}", 1)

	// Act
	c := compare(t, env, func(w *world) {
		w.sbx.out["ls --json"] = strings.Replace(w.sbx.out["ls --json"], `"workspaces": ["`+w.ws+`"]`,
			`"workspaces": ["`+w.ws+`", "/srv/extra"]`, 1)
	})

	// Assert
	if d, ok := drift(c, "宣言したフォルダがマウントされていない"); !ok || !strings.Contains(d.Declared, "elsewhere") {
		t.Errorf("missing mount: %+v (%v)", d, c.Drifts)
	}
	if d, ok := drift(c, "宣言にないフォルダ"); !ok || !strings.Contains(d.Actual, "/srv/extra") || !strings.Contains(d.Actual, "読み書きの区別は sbx から取得できません") {
		t.Errorf("extra mount: %+v", d)
	}
}

func TestStrictRedactionHidesAnUndeclaredWorkspacePath(t *testing.T) {
	t.Parallel()
	// Arrange: under --redact, sbx mounts a directory the files do not
	// declare; its path can carry values the plan redacts.
	env := strings.Replace(matching, "workspace: ./web-app\n", "", 1)

	// Act
	c := compare(t, env, func(w *world) { w.strict = true })

	// Assert
	d, ok := drift(c, "宣言にないフォルダ")
	if !ok || !strings.HasPrefix(d.Actual, "<redacted>") || strings.Contains(d.Actual, "web-app") {
		t.Errorf("undeclared workspace under strict redaction: %+v (%v)", d, c.Drifts)
	}
}

func TestStrictRedactionHidesTheMountsOfAnUndeclaredSandbox(t *testing.T) {
	t.Parallel()
	for _, strict := range []bool{false, true} {
		// Arrange: no file declares the sandbox "orphan", so its Overview
		// is drawn from sbx's own paths.
		w := newWorld(t, matching)
		w.strict = strict

		// Act
		orphan := find(t, w.discover(t, w.sbx), "orphan")

		// Assert
		if len(orphan.Sandbox.Workspaces) == 0 {
			t.Fatalf("the fixture's orphan mounts a workspace: %+v", orphan.Sandbox)
		}
		for _, m := range append(orphan.Sandbox.Workspaces, orphan.Sandbox.Mounts...) {
			if hidden := m.Path == "<redacted>"; hidden != strict {
				t.Errorf("strict=%v: path %q", strict, m.Path)
			}
		}
	}
}

func TestCompareConfirmsOnlyTheItemsItHeldAgainstSbx(t *testing.T) {
	t.Parallel()
	// Arrange: a clone workspace keeps no mount path to compare, and sbx
	// policy ls lists the rules in force, not the install-time ones.
	env := strings.Replace(matching, "workspace: ./web-app", "workspace: {path: ./web-app, clone: true}", 1) + "kits:\n  - ./kits/net\n"
	kit := "schemaVersion: \"3\"\nkind: mixin\nname: net\ncapabilities:\n  - type: com.docker.sandbox/network-policy@1\n    config:\n      install:\n        allow: [build.example.com]\n      runtime:\n        allow: [api.example.com]\n"

	// Act
	c := compare(t, env, func(w *world) {
		if err := os.MkdirAll(filepath.Join(w.dir, "kits", "net"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w.dir, "kits", "net", "net.yaml"), []byte(kit), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	// Assert
	got := strings.Join(c.Compared, " ")
	for _, want := range []string{"agent", "ports[", "secrets.github", "mcp.servers[docs]", "runtime.allow/api.example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was compared: %v", want, c.Compared)
		}
	}
	if !slices.Contains(c.Compared, "name") {
		t.Errorf("the name the sandbox was found by was compared: %v", c.Compared)
	}
	for _, not := range []string{"workspace", "build.example.com"} {
		if strings.Contains(got, not) {
			t.Errorf("%s was not held against sbx's output: %v", not, c.Compared)
		}
	}
}

func TestStrictRedactionKeepsArgumentNamedMCPServersApart(t *testing.T) {
	t.Parallel()
	// Arrange: two servers named by arguments; under --redact both show
	// as <redacted>, and only the second (jira) needs authorization.
	env := "schemaVersion: \"1\"\nname: web-app\nagent: claude\nworkspace: ./web-app\nargs:\n  a: {default: docs}\n  b: {default: jira}\n" +
		"mcp:\n  servers:\n    - {name: \"${{ env.args.a }}\", url: \"https://a.example.com\"}\n    - {name: \"${{ env.args.b }}\", url: \"https://b.example.com\"}\n"

	// Act
	c := compare(t, env, func(w *world) { w.strict = true })

	// Assert: regression; both servers had one ID, so the drift landed on
	// the first, and its fix printed the hidden name.
	d, ok := drift(c, "使える状態ではない")
	if !ok || d.ItemID != "mcp.servers[<redacted>]#2" {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
	if strings.Contains(d.Fix, "jira") || !strings.Contains(d.Fix, "sbx mcp auth <redacted>") {
		t.Errorf("fix shows the redacted name: %q", d.Fix)
	}
	for _, id := range []string{"mcp.servers[<redacted>]", "mcp.servers[<redacted>]#2"} {
		if !slices.Contains(c.Compared, id) {
			t.Errorf("%s was compared: %v", id, c.Compared)
		}
	}
}

func TestStrictRedactionKeepsArgumentPortsApart(t *testing.T) {
	t.Parallel()
	// Arrange: two ports set by arguments both show as <redacted>/tcp4;
	// sbx publishes only the first.
	env := "schemaVersion: \"1\"\nname: web-app\nagent: claude\nworkspace: ./web-app\nargs:\n  a: {default: \"3000\"}\n  b: {default: \"3001\"}\n" +
		"ports:\n  - {sandbox: \"${{ env.args.a }}\", host: 4000}\n  - {sandbox: \"${{ env.args.b }}\", host: 4001}\n"

	// Act
	c := compare(t, env, func(w *world) { w.strict = true })

	// Assert: the drift is the second port's, under its own item ID.
	d, ok := drift(c, "宣言したポートが公開されていない")
	if !ok || d.ItemID != "ports[<redacted>/tcp4]#2" {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
}

func TestDriftMCPOfAnotherKind(t *testing.T) {
	t.Parallel()
	// Arrange: docs is declared as a command server, but sbx has a remote
	// server of that name.
	env := strings.Replace(matching, `{name: docs, url: "https://mcp.example.com/sse"}`, `{name: docs, command: "docs-mcp"}`, 1)

	// Act
	c := compare(t, env, nil)

	// Assert
	if d, ok := drift(c, "宣言と違う種類"); !ok || d.Actual != "種類: remote（sse）" {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
}

func TestMCPWithoutAKindIsNotConfirmed(t *testing.T) {
	t.Parallel()
	// Act: sbx lists docs but not what kind of server it is.
	c := compare(t, matching, func(w *world) {
		w.sbx.out["mcp ls --json"] = `[{"name": "docs", "status": "ready"}]`
	})

	// Assert: registered, but url versus command is unknown.
	if slices.Contains(c.Compared, "mcp.servers[docs]") || len(c.Drifts) > 1 {
		t.Errorf("compared %v, drifts %v", c.Compared, c.Drifts)
	}
}

func TestReadOnlyIsNotComparedBecauseSbxDoesNotReportIt(t *testing.T) {
	t.Parallel()
	// Arrange: the declaration wants a read-only mount; sbx ls lists paths
	// only, so whether it is read-only cannot be known.
	env := strings.Replace(matching, "workspace: ./web-app", "workspace: ./web-app\nadditionalWorkspaces:\n  - {path: ./elsewhere, readOnly: true}", 1)

	// Act
	c := compare(t, env, func(w *world) {
		w.sbx.out["ls --json"] = strings.Replace(w.sbx.out["ls --json"], `"workspaces": ["`+w.ws+`"]`, `"workspaces": ["`+w.ws+`", "`+w.other+`"]`, 1)
	})

	// Assert
	for _, d := range c.Drifts {
		if d.Kind == "ワークスペース・マウント" {
			t.Errorf("no mount drift expected, got %+v", d)
		}
	}
	if !strings.Contains(strings.Join(c.Skipped, ""), "書き込みの可否（RW / RO）は比較していません") {
		t.Errorf("the reason must be stated: %v", c.Skipped)
	}
}

func TestDriftPorts(t *testing.T) {
	t.Parallel()
	// Act: the sandbox publishes on every interface and one more port.
	c := compare(t, matching, func(w *world) {
		w.sbx.out["ls --json"] = strings.Replace(w.sbx.out["ls --json"], `{"host_ip": "127.0.0.1", "host_port": 4000, "sandbox_port": 3000, "protocol": "tcp"}`,
			`{"host_ip": "0.0.0.0", "host_port": 4000, "sandbox_port": 3000, "protocol": "tcp"}, {"host_ip": "127.0.0.1", "host_port": 51234, "sandbox_port": 9229, "protocol": "tcp"}`, 1)
	})

	// Assert
	if d, ok := drift(c, "公開のしかたが宣言と違う"); !ok || d.Severity != sbxenv.SeverityHigh || !strings.Contains(d.Title, "すべてのネットワーク") {
		t.Errorf("wider port: %+v (%v)", d, c.Drifts)
	}
	if d, ok := drift(c, "宣言にないポート"); !ok || !strings.Contains(d.Actual, "sandbox:9229") {
		t.Errorf("extra port: %+v", d)
	}
}

func TestDriftManualNetworkAllowAndUnappliedKitRule(t *testing.T) {
	t.Parallel()
	// Arrange: a local kit allows two hosts; sbx reports only one of them
	// plus a host allowed by hand.
	env := matching + "kits:\n  - ./kits/net\n"
	kit := "schemaVersion: \"3\"\nkind: mixin\nname: net\ncapabilities:\n  - type: com.docker.sandbox/network-policy@1\n    config:\n      install:\n        allow: [manual.example.com]\n      runtime:\n        allow: [api.example.com, cdn.example.com]\n"

	// Act
	c := compare(t, env, func(w *world) {
		if err := os.MkdirAll(filepath.Join(w.dir, "kits", "net"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w.dir, "kits", "net", "net.yaml"), []byte(kit), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	// Assert
	if d, ok := drift(c, "宣言にない通信許可"); !ok || d.Actual != "許可: manual.example.com" || !strings.Contains(d.Title, "手動追加") {
		t.Errorf("manual allow: %+v (%v)", d, c.Drifts)
	}
	if d, ok := drift(c, "宣言した通信の許可が反映されていない"); !ok || !strings.Contains(d.Declared, "cdn.example.com") {
		t.Errorf("unapplied rule: %+v (%v)", d, c.Drifts)
	}
	if _, ok := drift(c, "api.example.com"); ok {
		t.Error("an applied kit rule is not drift")
	}
}

func TestDriftSecrets(t *testing.T) {
	t.Parallel()
	// Arrange
	env := strings.Replace(matching, "secrets:\n", "secrets:\n  anthropic:\n    ref: op://Private/Anthropic/key\n", 1)

	// Act
	c := compare(t, env, func(w *world) {
		w.sbx.out["secret ls --sandbox web-app --json"] = `{"secrets": [{"scope": "sandbox", "type": "service", "name": "github", "secret": "x"}, {"scope": "sandbox", "type": "service", "name": "npm", "secret": "y"}], "custom_secrets": []}`
	})

	// Assert
	if d, ok := drift(c, "宣言したシークレットが登録されていない"); !ok || !strings.HasPrefix(d.Declared, "anthropic") {
		t.Errorf("missing secret: %+v (%v)", d, c.Drifts)
	}
	if d, ok := drift(c, "宣言にないシークレット"); !ok || d.Actual != "npm（値は表示しません）" {
		t.Errorf("extra secret: %+v", d)
	}
}

func TestDriftMCP(t *testing.T) {
	t.Parallel()
	// Arrange
	env := strings.Replace(matching, "    - {name: docs, url: \"https://mcp.example.com/sse\"}\n",
		"    - {name: docs, url: \"https://mcp.example.com/sse\"}\n    - {name: jira, url: \"https://jira.example.com/mcp\"}\n    - {name: wiki, url: \"https://wiki.example.com/mcp\"}\n", 1)

	// Act
	c := compare(t, env, nil)

	// Assert
	if d, ok := drift(c, "MCP サーバーが登録されていない"); !ok || !strings.Contains(d.Declared, "wiki") {
		t.Errorf("missing server: %+v (%v)", d, c.Drifts)
	}
	if d, ok := drift(c, "使える状態ではない"); !ok || !strings.Contains(d.Fix, "sbx mcp auth jira") {
		t.Errorf("auth: %+v", d)
	}
}

func TestDriftSkipsWhatSbxDidNotReport(t *testing.T) {
	t.Parallel()
	// Act
	c := compare(t, matching, func(w *world) {
		w.sbx.fail["secret ls --sandbox web-app --json"] = errors.New("boom")
		w.sbx.fail["mcp ls --json"] = errors.New("boom")
	})

	// Assert
	for _, d := range c.Drifts {
		if d.Kind == "シークレット" || d.Kind == "MCP サーバー" {
			t.Errorf("guessed drift %+v", d)
		}
	}
	skipped := strings.Join(c.Skipped, "")
	if !strings.Contains(skipped, "シークレットは比較していません") || !strings.Contains(skipped, "MCP サーバーは比較していません") {
		t.Errorf("skipped = %v", c.Skipped)
	}
}

func TestDiscoverTreatsSymlinkedAndMissingPathsAsTheSame(t *testing.T) {
	t.Parallel()
	// Arrange: sbx reports the workspace through a symlinked parent (as
	// macOS does for /var → /private/var), and the workspace directory
	// itself does not exist.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	env := "schemaVersion: \"1\"\nagent: claude\nworkspace: ./missing-ws\n"
	if err := os.WriteFile(filepath.Join(real, "sbxenv.yaml"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeSbx{out: map[string]string{
		"ls --json":                    `[{"name": "s", "agent": "claude", "status": "running", "workspaces": ["` + filepath.Join(link, "missing-ws") + `"]}]`,
		"policy ls s --json":           `{"rules": []}`,
		"secret ls --sandbox s --json": `{"secrets": []}`,
		"mcp ls --json":                "[]",
	}}
	w := &world{dir: real}

	// Act
	targets, _ := live.Discover(context.Background(), live.DiscoverRequest{WorkDir: real, Runner: r, Resolve: w.resolve})

	// Assert
	if len(targets) != 1 || targets[0].Sandbox == nil || targets[0].Plan == nil {
		t.Fatalf("one paired target expected, got %v", names(targets))
	}
	if n := targets[0].Drifts(); n != 0 {
		t.Errorf("the same directory reported as drift: %+v", targets[0].Comparison.Drifts)
	}
}

func BenchmarkCompareMissingPorts(b *testing.B) {
	for _, count := range []int{10, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			p := sbxenv.Plan{Declared: &sbxenv.Declared{}}
			for i := range count {
				id := fmt.Sprintf("ports[%d/tcp]", i+1)
				p.Items = append(p.Items, sbxenv.Item{ID: id, Value: id})
				p.Declared.Ports = append(p.Declared.Ports, sbxenv.DeclaredPort{ItemID: id, SandboxPort: i + 1})
			}
			b.ReportAllocs()
			for b.Loop() {
				if c := live.Compare(&p, &live.Sandbox{}, nil, nil); len(c.Drifts) != count {
					b.Fatal(len(c.Drifts))
				}
			}
		})
	}
}

// --- hand-built comparisons --------------------------------------------------

// strictPlan is a strictly redacted plan with the given declaration and
// items, as Resolve would leave it.
func strictPlan(d *sbxenv.Declared, items ...sbxenv.Item) *sbxenv.Plan {
	return &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactStrict}, Declared: d, Items: items}
}

func TestStrictRedactionHidesALivePortThatMayCarryAnArgument(t *testing.T) {
	t.Parallel()
	// Arrange: the declared UDP port comes from an argument (3000); sbx
	// publishes TCP 3000, which matches nothing declared.
	p := strictPlan(&sbxenv.Declared{Ports: []sbxenv.DeclaredPort{{ItemID: "ports[<redacted>/udp]", SandboxPort: 3000, HostPort: 4000, Protocol: "udp", Redacted: true}}},
		sbxenv.Item{ID: "ports[<redacted>/udp]", Value: "<redacted>"})
	sb := &live.Sandbox{Ports: []live.Port{{HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp"}}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; the extra port printed the hidden number.
	d, ok := drift(&c, "宣言にないポート")
	if !ok || d.Actual != "<redacted>" {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
}

func TestStrictRedactionHidesALiveRuleForAHiddenHost(t *testing.T) {
	t.Parallel()
	// Arrange: a Kit denies a host set by an argument; someone allowed the
	// same host by hand.
	p := strictPlan(&sbxenv.Declared{Network: []sbxenv.DeclaredRule{{ItemID: "r1", Phase: "runtime", Decision: "deny", Host: "acme.example.com"}}},
		sbxenv.Item{ID: "r1", Value: "runtime deny <redacted>.example.com"})
	sb := &live.Sandbox{RulesKnown: true, Rules: []live.Rule{
		{Decision: "allow", Resource: "acme.example.com", CreatedVia: "added"},
		{Decision: "allow", Resource: "other.example.com", CreatedVia: "added"},
	}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; 許可: acme.example.com revealed the argument.
	var actuals []string
	for _, d := range c.Drifts {
		actuals = append(actuals, d.Actual)
	}
	if got := strings.Join(actuals, " "); strings.Contains(got, "acme") || !strings.Contains(got, "許可: <redacted>") || !strings.Contains(got, "許可: other.example.com") {
		t.Errorf("drifts = %v", actuals)
	}
}

func TestAnIPv4ListenerDoesNotConfirmAnIPv6Port(t *testing.T) {
	t.Parallel()
	// Arrange
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactHome}, Declared: &sbxenv.Declared{Ports: []sbxenv.DeclaredPort{{ItemID: "ports[3000/tcp6]", HostIP: "::1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp6"}}}}
	for _, lp := range []live.Port{
		{HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp"},
		{HostIP: "::1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp4"},
	} {
		// Act
		c := live.Compare(p, &live.Sandbox{Ports: []live.Port{lp}}, &live.Snapshot{}, nil)

		// Assert: regression; both loopbacks counted as one address.
		if _, ok := drift(&c, "ポート"); !ok {
			t.Errorf("%+v matched a tcp6 ::1 declaration: %v", lp, c.Drifts)
		}
	}
}

func TestABoundedRuleIsNotConfirmedByItsHost(t *testing.T) {
	t.Parallel()
	// Arrange: an @2 allow limited to GET /limited/**; sbx lists the host.
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactHome}, Declared: &sbxenv.Declared{Network: []sbxenv.DeclaredRule{
		{ItemID: "bounded", Phase: "runtime", Decision: "allow", Host: "api.example.com", Bounded: true},
		{ItemID: "plain", Phase: "runtime", Decision: "allow", Host: "cdn.example.com"},
	}}}
	sb := &live.Sandbox{RulesKnown: true, Rules: []live.Rule{
		{Decision: "allow", Resource: "api.example.com", Origin: "kit"},
		{Decision: "allow", Resource: "cdn.example.com", Origin: "kit"},
	}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; the host alone read as the whole rule.
	if slices.Contains(c.Compared, "bounded") || !slices.Contains(c.Compared, "plain") || len(c.Drifts) > 0 {
		t.Errorf("compared %v, drifts %v", c.Compared, c.Drifts)
	}
}

func TestStrictRedactionHidesAHiddenHostWithAPort(t *testing.T) {
	t.Parallel()
	// Arrange: the manual rule names the hidden host with a port.
	p := strictPlan(&sbxenv.Declared{Network: []sbxenv.DeclaredRule{{ItemID: "r1", Phase: "runtime", Decision: "deny", Host: "private.example"}}},
		sbxenv.Item{ID: "r1", Value: "runtime deny <redacted>"})
	sb := &live.Sandbox{RulesKnown: true, Rules: []live.Rule{{Decision: "allow", Resource: "private.example:443", CreatedVia: "added"}}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; the whole resource was looked up, port and all.
	if d, ok := drift(&c, "宣言にない通信許可"); !ok || d.Actual != "許可: <redacted>" {
		t.Errorf("drift = %+v (%v)", d, c.Drifts)
	}
}

func TestPortsPairByTheFamilyOfTheirAddress(t *testing.T) {
	t.Parallel()
	// Arrange: sbx reports both listeners as tcp; the address tells them
	// apart. The IPv6 one comes first.
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactHome}, Declared: &sbxenv.Declared{Ports: []sbxenv.DeclaredPort{
		{ItemID: "v4", HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp4"},
		{ItemID: "v6", HostIP: "::1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp6"},
	}}}
	sb := &live.Sandbox{Ports: []live.Port{
		{HostIP: "::1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 3000, Protocol: "tcp"},
	}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; each declaration took the other's listener.
	if len(c.Drifts) > 0 || !slices.Contains(c.Compared, "v4") || !slices.Contains(c.Compared, "v6") {
		t.Errorf("compared %v, drifts %v", c.Compared, c.Drifts)
	}

	// Act / Assert: a default tcp4 port is not confirmed by an IPv6-only listener.
	p.Declared.Ports = []sbxenv.DeclaredPort{{ItemID: "v4", SandboxPort: 3000, Protocol: "tcp4"}}
	c = live.Compare(p, &live.Sandbox{Ports: sb.Ports[:1]}, &live.Snapshot{}, nil)
	if _, ok := drift(&c, "宣言したポートが公開されていない"); !ok {
		t.Errorf("drifts %v", c.Drifts)
	}
}

func TestAFixedHostPortIsNotConfirmedWithoutOne(t *testing.T) {
	t.Parallel()
	// Arrange: sbx gives no host port for the sandbox port.
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactHome}, Declared: &sbxenv.Declared{Ports: []sbxenv.DeclaredPort{{ItemID: "p", HostPort: 8080, SandboxPort: 3000, Protocol: "tcp4"}}}}

	// Act
	c := live.Compare(p, &live.Sandbox{Ports: []live.Port{{SandboxPort: 3000, Protocol: "tcp"}}}, &live.Snapshot{}, nil)

	// Assert: regression; the missing number read as a match.
	if slices.Contains(c.Compared, "p") || len(c.Drifts) > 0 {
		t.Errorf("compared %v, drifts %v", c.Compared, c.Drifts)
	}
}

func TestABoundedRuleDoesNotHideAManualRuleForItsHost(t *testing.T) {
	t.Parallel()
	// Arrange: the Kit allows GET /health only; someone allowed the whole host.
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Redaction: sbxenv.RedactHome}, Declared: &sbxenv.Declared{Network: []sbxenv.DeclaredRule{
		{ItemID: "bounded", Phase: "runtime", Decision: "allow", Host: "api.example.com", Bounded: true},
	}}}
	sb := &live.Sandbox{RulesKnown: true, Rules: []live.Rule{
		{Decision: "allow", Resource: "api.example.com", Origin: "kit"},
		{Decision: "allow", Resource: "api.example.com", CreatedVia: "added"},
	}}

	// Act
	c := live.Compare(p, sb, &live.Snapshot{}, nil)

	// Assert: regression; the bounded declaration covered the manual rule.
	if d, ok := drift(&c, "宣言にない通信許可"); !ok || d.Actual != "許可: api.example.com" {
		t.Errorf("drifts %v", c.Drifts)
	}
}
