package main_test

// Black-box tests: the real binary is built once and executed as a
// subprocess. Every case gets its own HOME, working directory, and a
// minimal environment, so cases cannot influence each other.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sbxray-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "sbxray")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// sandbox is one isolated invocation environment.
type sandbox struct {
	home, work string
}

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	home := t.TempDir()
	work := filepath.Join(home, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	return sandbox{home: home, work: work}
}

func (s sandbox) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(s.work, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type result struct {
	code           int
	stdout, stderr string
}

func (s sandbox) run(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = s.work
	cmd.Env = []string{"HOME=" + s.home, "USERPROFILE=" + s.home, "PATH=" + os.Getenv("PATH"), "TERM=dumb", "NO_COLOR=1"}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, r.stdout)
	}
	return v
}

const minimal = "schemaVersion: \"1\"\nagent: claude\n"

func TestExitCodesFollowFailOn(t *testing.T) {
	t.Parallel()
	const warn = minimal + "ports:\n  - {sandbox: 1, hostIP: 0.0.0.0}\n"
	const fail = minimal + "bogus: 1\n"
	const rwWorkspace = minimal + "workspace: .\n"
	cases := []struct {
		name string
		file string
		args []string
		want int
	}{
		{"clean file, default", minimal, nil, 0},
		{"clean file, fail on warning", minimal, []string{"--fail-on=warning"}, 0},
		{"warning, fail on warning", warn, []string{"--fail-on", "warning"}, 1},
		{"warning, fail on error", warn, []string{"--fail-on", "error"}, 0},
		{"error, fail on error", fail, []string{"--fail-on=error"}, 1},
		{"error, fail on warning", fail, []string{"--fail-on=warning"}, 1},
		{"error, fail on none", fail, []string{"--fail-on=none"}, 0},
		// The risk scale of the Risk view.
		{"clean file, fail on low", minimal, []string{"--fail-on=low"}, 0},
		{"rw workspace (medium), fail on medium", rwWorkspace, []string{"--fail-on=medium"}, 1},
		{"rw workspace (medium), fail on high", rwWorkspace, []string{"--fail-on=high"}, 0},
		{"warning (high), fail on high", warn, []string{"--fail-on=high"}, 1},
		{"warning (high), fail on critical", warn, []string{"--fail-on=critical"}, 0},
		{"error (critical), fail on critical", fail, []string{"--fail-on=critical"}, 1},
		// A remote kit is an analysis gap, not a finding.
		{"coverage gap only, fail on low", minimal + "kits:\n  - ghcr.io/example/kit:1\n", []string{"--fail-on=low"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			s := newSandbox(t)
			s.write(t, "sbxenv.yaml", tc.file)

			// Act
			r := s.run(t, append([]string{"--text"}, tc.args...)...)

			// Assert
			if r.code != tc.want {
				t.Errorf("exit %d, want %d\nstderr: %s", r.code, tc.want, r.stderr)
			}
		})
	}
}

func TestUsageAndParseErrorsExitTwo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		args []string
		msg  string
	}{
		{"unknown flag", minimal, []string{"--nope"}, "unknown flag"},
		{"bad fail-on", minimal, []string{"--fail-on=sometimes"}, "--fail-on"},
		{"text and json", minimal, []string{"--text", "--json"}, "cannot be combined"},
		{"missing value", minimal, []string{"--env-arg"}, "needs a value"},
		{"missing file", minimal, []string{"nope.yaml"}, "nope.yaml"},
		{"two documents", "a: 1\n---\nb: 2\n", nil, "multiple YAML documents"},
		{"no environment file", "", nil, "no environment file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			s := newSandbox(t)
			if tc.file != "" {
				s.write(t, "sbxenv.yaml", tc.file)
			}

			// Act
			r := s.run(t, tc.args...)

			// Assert
			if r.code != 2 {
				t.Errorf("exit %d, want 2", r.code)
			}
			if !strings.Contains(r.stderr, tc.msg) {
				t.Errorf("stderr %q should mention %q", r.stderr, tc.msg)
			}
		})
	}
}

func TestNonTTYDefaultsToText(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", minimal+"env:\n  A: b\n")

	// Act
	r := s.run(t)

	// Assert
	if r.code != 0 || !strings.Contains(r.stdout, "宣言の静的解析") || !strings.Contains(r.stdout, "env.A = b") {
		t.Errorf("exit %d\n%s", r.code, r.stdout)
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Error("text output must not contain escape sequences")
	}
}

func TestJSONIsStrictlyRedactedAndCarriesInvocation(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", minimal+"args:\n  x:\n    default: a\nenv:\n  X: ${{ env.args.x }}\n  TOKEN: PSEUDOSECRET01\n")
	s.write(t, "x.args", "x=PSEUDOSECRET02\n")

	// Act
	r := s.run(t, "--json", "--env-args-file", "x.args", "--env-arg=x=PSEUDOSECRET03", "--name", "n1", "--clone=false", "--skip-host-commands", ".")

	// Assert
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, secret := range []string{"PSEUDOSECRET01", "PSEUDOSECRET02", "PSEUDOSECRET03", s.home} {
		if strings.Contains(r.stdout, secret) {
			t.Errorf("--json leaks %q", secret)
		}
	}
	v := r.json(t)
	meta := v["meta"].(map[string]any)
	if meta["schemaVersion"] != "1" || meta["redaction"] != "strict" {
		t.Errorf("meta = %v", meta)
	}
	inv := v["invocation"].(map[string]any)
	if inv["name"] != "n1" || inv["clone"] != false || inv["skipHostCommands"] != true || inv["projectDir"] != "~/proj" {
		t.Errorf("invocation = %v", inv)
	}
}

func TestJSONOutputIsByteStable(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", minimal+"env:\n  B: 1\n  A: 2\nports:\n  - {sandbox: 2}\n  - {sandbox: 1}\n")

	// Act
	first := s.run(t, "--json")
	second := s.run(t, "--json")

	// Assert
	if first.stdout != second.stdout {
		t.Error("two runs on the same input produced different JSON")
	}
	var ids []string
	for _, it := range first.json(t)["items"].([]any) {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "env.A,env.B") || !strings.Contains(joined, "ports[1/tcp4],ports[2/tcp4]") {
		t.Errorf("items are not sorted within sections: %s", joined)
	}
}

func TestRedactFlagAppliesToText(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", minimal+"env:\n  TOKEN: PSEUDOSECRET04\n")

	// Act
	plain := s.run(t, "--text")
	redacted := s.run(t, "--text", "--redact")

	// Assert
	if !strings.Contains(plain.stdout, "PSEUDOSECRET04") {
		t.Error("without --redact the local text view shows values")
	}
	if strings.Contains(redacted.stdout, "PSEUDOSECRET04") {
		t.Error("--redact must hide values in text output")
	}
}

func TestDefaultsMergeUserFileUnderProjectFile(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	if err := os.WriteFile(filepath.Join(s.home, ".sbxenv.yaml"), []byte(minimal+"env:\n  FROM_HOME: \"1\"\n  SHARED: home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.write(t, "sbxenv.yaml", "env:\n  SHARED: project\n")

	// Act
	implicit := s.run(t, "--text")
	explicit := s.run(t, "--text", ".")

	// Assert
	if !strings.Contains(implicit.stdout, "env.FROM_HOME = 1") || !strings.Contains(implicit.stdout, "env.SHARED = project") {
		t.Errorf("without a path, ~/.sbxenv.yaml is merged under the project file:\n%s", implicit.stdout)
	}
	if strings.Contains(explicit.stdout, "FROM_HOME") {
		t.Error("an explicit path skips ~/.sbxenv.yaml")
	}
}

func TestShippedExampleResolvesEndToEnd(t *testing.T) {
	t.Parallel()
	// Arrange
	example, err := filepath.Abs("../../testdata/layered")
	if err != nil {
		t.Fatal(err)
	}
	s := newSandbox(t)

	// Act
	r := s.run(t, "--json",
		filepath.Join(example, "base.sbxenv.yaml"), filepath.Join(example, "local.sbxenv.yaml"),
		"--env-args-file", filepath.Join(example, "prod.args"), "--env-arg", "channel=stable")

	// Assert
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	v := r.json(t)
	codes := map[string]bool{}
	for _, d := range v["diagnostics"].([]any) {
		codes[d.(map[string]any)["code"].(string)] = true
	}
	for _, want := range []string{"ENV_ARG_INEFFECTIVE", "HOST_MOUNT_SHARED", "PORT_ALL_INTERFACES", "PULL_ALWAYS_DEV", "SSH_AGENT_UNRESTRICTED", "BINDING_DOMAIN_MISSING"} {
		if !codes[want] {
			t.Errorf("example should report %s; got %v", want, codes)
		}
	}
	summary := v["summary"].(map[string]any)
	if summary["egressAllows"].(float64) != 3 || summary["hostExec"].(float64) != 2 {
		t.Errorf("summary = %v", summary)
	}
}

func TestHelpAndVersion(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"--help": "Usage: sbxray", "-h": "Usage: sbxray", "--version": "targets sbx v"}
	for flag, want := range cases {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			// Arrange
			s := newSandbox(t)

			// Act
			r := s.run(t, flag)

			// Assert
			if r.code != 0 || !strings.Contains(r.stdout, want) {
				t.Errorf("exit %d, stdout %q", r.code, r.stdout)
			}
		})
	}
}

func TestHomeIsShortenedWhenHomeIsASymlink(t *testing.T) {
	t.Parallel()
	// Arrange: HOME points at a symlink and the working directory is the
	// resolved path, as with /var -> /private/var on macOS.
	real := newSandbox(t)
	link := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(real.home, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	real.write(t, "sbxenv.yaml", minimal)
	s := sandbox{home: link, work: real.work}

	// Act
	r := s.run(t, "--json")

	// Assert
	inv := r.json(t)["invocation"].(map[string]any)
	if inv["projectDir"] != "~/proj" {
		t.Errorf("projectDir = %v, want ~/proj", inv["projectDir"])
	}
	if strings.Contains(r.stdout, real.home) || strings.Contains(r.stdout, link) {
		t.Error("--json leaks the home directory through its other spelling")
	}
}

func TestTextOutputNeutralisesTerminalControlCharacters(t *testing.T) {
	t.Parallel()
	// Arrange: values that would clear the screen, retitle the window,
	// reorder text, and forge a provenance line.
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", minimal+"env:\n"+
		"  EVIL: \"ok\\e[2J\\e]0;pwned\\a\"\n"+
		"  BIDI: \"a\\u202Eb\"\n"+
		"  MULTI: |\n    line1\n    forged  ← fake/origin.yaml:1\n")

	// Act
	r := s.run(t, "--text")

	// Assert
	if strings.ContainsAny(r.stdout, "\x1b\x07‮") {
		t.Errorf("raw control characters reached the terminal: %q", r.stdout)
	}
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "forged") {
			t.Errorf("a value started its own output line: %q", line)
		}
	}
	if !strings.Contains(r.stdout, `env.EVIL = ok\x1b[2J`) || !strings.Contains(r.stdout, `line1\nforged`) {
		t.Errorf("escaped forms should stay readable:\n%s", r.stdout)
	}
}

func TestDirectoryWithoutEnvFileNamesTheFileItLookedFor(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "kits/k/k.yaml", "x: 1\n")

	// Act
	r := s.run(t, "kits")

	// Assert
	if r.code != 2 || !strings.Contains(r.stderr, filepath.Join("kits", "sbxenv.yaml")) {
		t.Errorf("exit %d, stderr %q should name kits/sbxenv.yaml", r.code, r.stderr)
	}
}

// fakeSbx puts an "sbx" on PATH that records every call in calls.log and
// answers the read-only listings with a sandbox named web-app running
// codex. It returns the PATH to use and the log path.
func (s sandbox) fakeSbx(t *testing.T) (path, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sbx is a shell script")
	}
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$*" in
  "ls --json --no-input") echo '[{"name":"web-app","id":"sbx_1","agent":"codex","status":"running","last_used_at":"","created_at":"","ports":[],"mounts":[],"workspaces":[]}]' ;;
  "policy ls web-app --json --no-input") echo '{"rules":[]}' ;;
  "secret ls --sandbox web-app --json --no-input") echo '{"secrets":[],"custom_secrets":[],"shadowed_services":[],"env_only_count":0}' ;;
  "mcp ls --json --no-input") echo '[]' ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + "/usr/bin:/bin", log
}

func (s sandbox) runPath(t *testing.T, path string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = s.work
	cmd.Env = []string{"HOME=" + s.home, "PATH=" + path, "TERM=dumb", "NO_COLOR=1"}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

const liveEnv = "schemaVersion: \"1\"\nname: web-app\nagent: claude\n"

func TestTextAndJSONStayOfflineWithoutLive(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--text"}, {"--json"}, {"sbxenv.yaml"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			// Arrange
			s := newSandbox(t)
			s.write(t, "sbxenv.yaml", liveEnv)
			path, log := s.fakeSbx(t)

			// Act
			r := s.runPath(t, path, args...)

			// Assert
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			if _, err := os.Stat(log); err == nil {
				b, _ := os.ReadFile(log)
				t.Errorf("sbx ran without --live: %s", b)
			}
			if strings.Contains(r.stdout, "稼働中の実体との比較") || strings.Contains(r.stdout, `"live"`) {
				t.Errorf("offline output changed:\n%s", r.stdout)
			}
		})
	}
}

func TestLiveTextComparesWithTheRunningSandbox(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", liveEnv)
	path, log := s.fakeSbx(t)

	// Act
	r := s.runPath(t, path, "--text", "--live")

	// Assert
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{"稼働中の実体との比較（--live）", "対象: web-app（稼働中） ズレ 1 件", "宣言と違うエージェントで動いている", "宣言: claude", "実体: codex"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q:\n%s", want, r.stdout)
		}
	}
	calls, _ := os.ReadFile(log)
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if !strings.HasSuffix(line, "--json --no-input") || !(strings.HasPrefix(line, "ls ") || strings.Contains(line, " ls ")) {
			t.Errorf("only read-only listings may run, got %q", line)
		}
	}
}

func TestFailOnCountsDriftOnlyWithLive(t *testing.T) {
	t.Parallel()
	// Arrange: the declaration alone stays below medium; the running
	// sandbox uses another agent, a medium drift.
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", liveEnv)
	path, _ := s.fakeSbx(t)

	// Act
	offline := s.runPath(t, path, "--text", "--fail-on=medium")
	withLive := s.runPath(t, path, "--text", "--live", "--fail-on=medium")

	// Assert
	if offline.code != 0 {
		t.Errorf("offline exit %d, want 0: %s", offline.code, offline.stderr)
	}
	if withLive.code != 1 {
		t.Errorf("--live exit %d, want 1: %s", withLive.code, withLive.stderr)
	}
}

func TestLiveJSONAddsALiveSection(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", liveEnv)
	path, _ := s.fakeSbx(t)

	// Act
	r := s.runPath(t, path, "--json", "--live")

	// Assert
	doc := r.json(t)
	l, ok := doc["live"].(map[string]any)
	if !ok || l["sandbox"] != "web-app" || len(l["drift"].([]any)) != 1 {
		t.Errorf("live = %v", doc["live"])
	}
	if doc["schemaVersion"] == nil && doc["meta"] == nil {
		t.Error("the plan must stay intact")
	}
}

func TestLiveWithoutSbxReportsAndKeepsGoing(t *testing.T) {
	t.Parallel()
	// Arrange
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", liveEnv)

	// Act: PATH without sbx.
	r := s.runPath(t, "/usr/bin:/bin", "--text", "--live")

	// Assert
	if r.code != 0 || !strings.Contains(r.stdout, "sbx コマンドが見つからない") {
		t.Errorf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestLiveAndOfflineCannotBeCombined(t *testing.T) {
	t.Parallel()
	// Act
	r := newSandbox(t).run(t, "--live", "--offline")

	// Assert
	if r.code != 2 || !strings.Contains(r.stderr, "--live and --offline cannot be combined") {
		t.Errorf("exit %d: %s", r.code, r.stderr)
	}
}

func TestLiveJSONRedactsArgumentDerivedNameAndHostPort(t *testing.T) {
	t.Parallel()
	s := newSandbox(t)
	s.write(t, "sbxenv.yaml", strings.Replace(liveEnv, "name: web-app", "name: ${{ env.args.n }}", 1)+"args:\n  n:\n    default: web-app\n  port:\n    default: '42319'\nports:\n  - sandbox: 8080\n    host: ${{ env.args.port }}\n")
	path, _ := s.fakeSbx(t)
	script := filepath.Join(filepath.SplitList(path)[0], "sbx")
	b, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(strings.Replace(string(b), `"ports":[]`, `"ports":[{"sandbox_port":8080,"host_port":8081,"protocol":"tcp"}]`, 1)), 0755); err != nil {
		t.Fatal(err)
	}
	r := s.runPath(t, path, "--json", "--live")
	l := r.json(t)["live"].(map[string]any)
	if l["sandbox"] == nil || !strings.Contains(r.stdout, "PC 側のポート番号が異なる") || strings.Contains(r.stdout, "web-app") || strings.Contains(r.stdout, "42319") {
		t.Fatalf("unexpected live output: %v", l)
	}
}
