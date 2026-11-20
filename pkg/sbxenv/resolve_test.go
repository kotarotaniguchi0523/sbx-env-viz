package sbxenv

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

const base = `schemaVersion: "1"
name: web-app
agent: claude
workspace: ./web-app
entrypoint: [/bin/sh, -c]
cmd: [echo, base]
env:
  NODE_ENV: development
  KEEP: base
sandboxOptions:
  cpus: 4
  memory: 8g
ports:
  - sandbox: 8080
    host: 3000
  - sandbox: 9000
additionalWorkspaces:
  - path: ../shared
    readOnly: true
mcp:
  servers:
    - name: docs
      url: https://mcp.example.com
lifecycle:
  initialize:
    - command: echo one
`

const local = `env:
  NODE_ENV: test
  LOCAL: "1"
sandboxOptions:
  memory: 12g
cmd: null
ports:
  - sandbox: 8080
    host: 4000
  - sandbox: 7000
additionalWorkspaces:
  - path: ../shared
mcp:
  servers:
    - name: docs
      command: docs-server
lifecycle:
  initialize:
    - command: echo two
`

func TestMergeRules(t *testing.T) {
	p := resolve(t, ResolveOptions{Redaction: RedactHome},
		layer(testProj+"/base.sbxenv.yaml", base), layer(testProj+"/local.sbxenv.yaml", local))

	if v := item(t, p, "sandboxOptions.memory").Value; v != "12g" {
		t.Errorf("scalar override: memory = %q, want 12g", v)
	}
	if v := item(t, p, "sandboxOptions.cpus").Value; v != "4" {
		t.Errorf("untouched key kept: cpus = %q", v)
	}
	if v := item(t, p, "env.NODE_ENV").Value; v != "test" {
		t.Errorf("map key override: NODE_ENV = %q", v)
	}
	if !hasItem(p, "env.KEEP") || !hasItem(p, "env.LOCAL") {
		t.Error("map merge must keep keys from both layers")
	}
	// ports: keyed on sandbox port + protocol; last wins, first position kept.
	var ports []string
	for _, it := range p.Items {
		if it.Section == SectionPorts {
			ports = append(ports, it.ID+"="+it.Value)
		}
	}
	want := []string{
		"ports[8080/tcp4]=loopback:4000 -> sandbox:8080/tcp4",
		"ports[9000/tcp4]=loopback:ephemeral -> sandbox:9000/tcp4",
		"ports[7000/tcp4]=loopback:ephemeral -> sandbox:7000/tcp4",
	}
	if !reflect.DeepEqual(ports, want) {
		t.Errorf("ports = %v\nwant %v", ports, want)
	}
	// additionalWorkspaces keyed on target; the replacement drops readOnly.
	aw := item(t, p, "additionalWorkspaces[~/shared]")
	if strings.Contains(aw.Value, "(ro)") || !contains(aw.Badges, BadgeRW) {
		t.Errorf("replacement workspace should be writable: %+v", aw)
	}
	// mcp servers keyed on name.
	if v := item(t, p, "mcp.servers[docs]").Value; v != "command: docs-server" {
		t.Errorf("mcp replace: %q", v)
	}
	// cmd: null inherits; entrypoint untouched.
	if v := item(t, p, "cmd").Value; v != "[echo, base]" {
		t.Errorf("cmd null must inherit: %q", v)
	}
	// lifecycle lists append.
	if !hasItem(p, "lifecycle.initialize[0]") || !hasItem(p, "lifecycle.initialize[1]") {
		t.Error("lifecycle lists must append")
	}
	if v := item(t, p, "lifecycle.initialize[1]").Value; v != "echo two" {
		t.Errorf("appended lifecycle: %q", v)
	}
}

func TestListReplaceAndClear(t *testing.T) {
	p := resolve(t, ResolveOptions{Redaction: RedactHome},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: shell\nentrypoint: [a, b]\ncmd: [x]\n"),
		layer(testProj+"/b.yaml", "entrypoint: [c]\ncmd: []\n"))
	if v := item(t, p, "entrypoint").Value; v != "[c]" {
		t.Errorf("entrypoint should be replaced, got %q", v)
	}
	if v := item(t, p, "cmd").Value; v != "[]" {
		t.Errorf("cmd [] should clear, got %q", v)
	}
}

func TestProvenanceAndHistory(t *testing.T) {
	p := resolve(t, ResolveOptions{Redaction: RedactHome},
		layer(testProj+"/base.sbxenv.yaml", base), layer(testProj+"/local.sbxenv.yaml", local))
	mem := item(t, p, "sandboxOptions.memory")
	if len(mem.History) != 2 {
		t.Fatalf("history = %+v", mem.History)
	}
	if mem.History[0].Op != OpShadow || mem.History[0].Value != "8g" || mem.History[0].Origin.SourceID != "L1" || mem.History[0].Origin.Line != 12 {
		t.Errorf("first history entry = %+v", mem.History[0])
	}
	if mem.History[1].Op != OpReplace || mem.History[1].Value != "12g" || mem.History[1].Origin.SourceID != "L2" || mem.History[1].Origin.Line != 5 {
		t.Errorf("second history entry = %+v", mem.History[1])
	}
	if got := mem.Origins; len(got) != 1 || got[0].SourceID != "L2" || got[0].Line != 5 {
		t.Errorf("origins = %+v", got)
	}
	port := item(t, p, "ports[8080/tcp4]")
	if port.History[len(port.History)-1].Origin.Line != 8 || port.History[0].Op != OpShadow {
		t.Errorf("port history = %+v", port.History)
	}
	if s := p.Meta.Sources[0]; s.Path != "~/proj/base.sbxenv.yaml" || len(s.SHA256) != 64 || s.Layer != 1 {
		t.Errorf("source = %+v", s)
	}
}

func TestKitArgsMergeAndCapabilityOrigins(t *testing.T) {
	kit := `# syntax=docker/sandbox-kit:3
schemaVersion: "3"
kind: mixin
args:
  host:
    default: a.example.com
  level:
    default: "1"
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: ["${{ kit.args.host }}"]
`
	p := resolve(t, ResolveOptions{Redaction: RedactHome, Kits: memKits{testProj + "/kits/net": kit}},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nkits:\n  - source: ./kits/net\n    args:\n      host: b.example.com\n      level: \"2\"\n"),
		layer(testProj+"/b.yaml", "kits:\n  - source: ./kits/net\n    args:\n      host: c.example.com\n"))
	entry := item(t, p, "kits[./kits/net]")
	if !strings.Contains(entry.Value, "host=c.example.com") || !strings.Contains(entry.Value, "level=2") {
		t.Errorf("kit args should merge key by key: %q", entry.Value)
	}
	allow := item(t, p, "kit:net/network-policy@1/runtime.allow/c.example.com")
	o := allow.Origins[0]
	if o.SourceID != "K1" || o.Line != 13 || len(o.Via) != 1 || !strings.HasPrefix(o.Via[0], "L1:4 kits[./kits/net]") {
		t.Errorf("capability origin = %+v", o)
	}
	// --kit-arg wins over the entry.
	p = resolve(t, ResolveOptions{Redaction: RedactHome, Kits: memKits{testProj + "/kits/net": kit}, KitArgs: []string{"net.host=d.example.com"}},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nkits:\n  - source: ./kits/net\n    args:\n      host: b.example.com\n"))
	if !hasItem(p, "kit:net/network-policy@1/runtime.allow/d.example.com") {
		t.Error("--kit-arg handle.name should override the entry")
	}
}

func TestDuplicateKeysReportBothPositions(t *testing.T) {
	p := resolve(t, ResolveOptions{},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nenv:\n  A: x\n  B: y\n  A: z\nagent: codex\n"))
	ds := diags(p, "DUPLICATE_KEY")
	if len(ds) != 2 {
		t.Fatalf("want 2 duplicate key diagnostics, got %+v", ds)
	}
	var nested Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, `"env.A"`) {
			nested = d
		}
	}
	if nested.Level != LevelError || len(nested.Origins) != 2 || nested.Origins[0].Line != 4 || nested.Origins[1].Line != 6 {
		t.Errorf("nested duplicate = %+v", nested)
	}
}

func TestSkillsOffStaysAString(t *testing.T) {
	p := resolve(t, ResolveOptions{Redaction: RedactHome},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nsandboxOptions:\n  skills: off\n"))
	if v := item(t, p, "sandboxOptions.skills").Value; v != "off" {
		t.Fatalf("skills = %q, want the string off", v)
	}
	noDiag(t, p, "INVALID_VALUE")
	res, err := parser.Parse([]byte("a: off\nb: yes\nc: 007\n"))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"a": "off", "b": "yes", "c": "007"} {
		if got := res.Root.Get(k).Value; got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestMultipleDocumentsRejected(t *testing.T) {
	_, err := Resolve([]LayerInput{layer(testProj+"/a.yaml", "schemaVersion: \"1\"\n---\nagent: x\n")}, ResolveOptions{HomeDir: testHome})
	var pe *ParseError
	if !errors.As(err, &pe) || !errors.Is(err, parser.ErrMultipleDocuments) {
		t.Fatalf("want multi-document parse error, got %v", err)
	}
	if pe.Path != "~/proj/a.yaml" {
		t.Errorf("path should be home-shortened: %q", pe.Path)
	}
}

func TestEnvArgPrecedence(t *testing.T) {
	decl := "schemaVersion: \"1\"\nagent: claude\nargs:\n  ch:\n    default: d\nenv:\n  CH: ${{ env.args.ch }}\n  DIR: ${{ env.fileDir }}\n  PROJ: ${{ env.projectDir }}/src\n  LIT: $${{ env.args.ch }}\n  SHELL: $PATH:/opt\n"
	files := []ArgFile{{Path: testProj + "/1.args", Content: []byte("ch=file1\n")}, {Path: testProj + "/2.args", Content: []byte("# c\n\nch=file2=with=equals\n")}}
	p := resolve(t, ResolveOptions{Redaction: RedactHome, EnvArgsFiles: files}, layer("/etc/shared/env.yaml", decl))
	if v := item(t, p, "env.CH").Value; v != "file2=with=equals" {
		t.Errorf("later args file should win: %q", v)
	}
	p = resolve(t, ResolveOptions{Redaction: RedactHome, EnvArgsFiles: files, EnvArgs: []string{"ch=flag"}}, layer("/etc/shared/env.yaml", decl))
	if v := item(t, p, "env.CH").Value; v != "flag" {
		t.Errorf("--env-arg should win over files: %q", v)
	}
	if v := item(t, p, "env.DIR").Value; v != "/etc/shared" {
		t.Errorf("fileDir = %q", v)
	}
	if v := item(t, p, "env.PROJ").Value; v != "~/proj/src" {
		t.Errorf("projectDir = %q", v)
	}
	if v := item(t, p, "env.LIT").Value; v != "${{ env.args.ch }}" {
		t.Errorf("escaped expression = %q", v)
	}
	if v := item(t, p, "env.SHELL").Value; v != "$PATH:/opt" {
		t.Errorf("shell syntax must stay literal: %q", v)
	}
	args := item(t, p, "args.ch")
	if args.Value != "flag" || args.History[0].Op != OpShadow || args.History[0].Value != "d" {
		t.Errorf("args item = %+v", args)
	}
}

func TestDiagnostics(t *testing.T) {
	kitCred := `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: ["*.example.com"]
  - type: com.docker.sandbox/credential@1
    config:
      service: example
      phase: runtime
      apiKey:
        name: EXAMPLE_TOKEN
        proxyManaged: true
        inject:
          - {domain: api.example.com, header: Authorization, format: "Bearer %s"}
  - type: com.docker.sandbox/credential@1
    optional: true
    config:
      service: other
      phase: runtime
      oauth:
        resourceHosts: [api.other.example]
  - type: com.docker.sandbox/host-mount@1
    config:
      path: /home/agent/.cache/tool
  - type: com.docker.sandbox/ssh-agent@1
    config:
      phase: runtime
      unrestricted: false
      sign: [git]
  - type: com.docker.sandbox/privileged@1
  - type: com.docker.sandbox/git-identity@1
    config: {}
`
	kits := memKits{testProj + "/kits/cred": kitCred}
	env := `schemaVersion: "1"
agent: claude
kits:
  - ./kits/cred
args:
  region:
    default: eu
    enum: [eu, us]
  token:
    required: true
env:
  REGION: ${{ env.args.region }}
  BAD: ${{ env.args.nope }}
  OTHER: ${{ env.home }}
sandboxOptions:
  template: ghcr.io/example/base:dev
  pullPolicy: sometimes
  writableEnvFiles: true
secrets:
  unused:
    value: hunter2
  mixed:
    ref: op://Shared/item/field
bindings:
  stray:
    apiKey:
      domains: [x.example.com]
mounts:
  /home/agent/nowhere: ./dir
unknownTop: 1
`
	env2 := `env:
  REGION: fixed
secrets:
  mixed:
    command: echo hi
`
	p := resolve(t, ResolveOptions{Kits: kits, EnvArgs: []string{"region=us", "ghost=1"}},
		layer(testProj+"/a.yaml", env), layer(testProj+"/b.yaml", env2))

	requireDiag(t, p, "ENV_ARG_INEFFECTIVE", LevelWarning)
	requireDiag(t, p, "ENV_ARG_UNDECLARED", LevelError)
	requireDiag(t, p, "ENV_ARG_REQUIRED", LevelError)
	requireDiag(t, p, "UNRESOLVED_EXPR", LevelError)
	requireDiag(t, p, "UNKNOWN_FIELD", LevelError)
	requireDiag(t, p, "INVALID_VALUE", LevelError)
	requireDiag(t, p, "WRITABLE_ENV_FILES", LevelWarning)
	requireDiag(t, p, "SECRET_LITERAL", LevelWarning)
	requireDiag(t, p, "SECRET_UNUSED", LevelWarning)
	requireDiag(t, p, "SECRET_SOURCE_COUNT", LevelError)
	requireDiag(t, p, "BINDING_UNUSED", LevelWarning)
	requireDiag(t, p, "CREDENTIAL_UNBOUND", LevelError)   // required example, no binding
	requireDiag(t, p, "CREDENTIAL_UNBOUND", LevelWarning) // optional other, no binding
	requireDiag(t, p, "CREDENTIAL_INJECT_NOT_ALLOWED", LevelError)
	requireDiag(t, p, "SECRET_NOT_DECLARED", LevelInfo)
	requireDiag(t, p, "HOST_MOUNT_SHARED", LevelWarning)
	requireDiag(t, p, "MOUNT_TARGET_UNKNOWN", LevelError)
	requireDiag(t, p, "PRIVILEGED", LevelWarning)
	requireDiag(t, p, "KIT_CONFIG_NOT_ALLOWED", LevelError)
	noDiag(t, p, "SSH_AGENT_UNRESTRICTED")
	if p.Summary.Errors == 0 || p.Summary.Warnings == 0 {
		t.Errorf("summary = %+v", p.Summary)
	}
	// Coverage always lists what was not analysed.
	kinds := map[CoverageKind]bool{}
	for _, c := range p.Coverage {
		kinds[c.Kind] = true
	}
	for _, k := range []CoverageKind{CoverageBuiltinAgent, CoverageMutableImage, CoverageHostPolicy, CoverageUnmodeledCap, CoverageGlobalSecrets} {
		if !kinds[k] {
			t.Errorf("missing coverage %s: %+v", k, p.Coverage)
		}
	}
	// Items carrying warnings get the [!] badge.
	if !contains(item(t, p, "sandboxOptions.writableEnvFiles").Badges, BadgeWarn) {
		t.Error("warning badge missing")
	}
}

func TestCredentialTriadClean(t *testing.T) {
	kit := `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: [api.example.com]
  - type: com.docker.sandbox/credential@1
    config:
      service: example
      phase: runtime
      apiKey:
        inject:
          - {domain: api.example.com, header: Authorization}
  - type: com.docker.sandbox/host-mount@1
    config:
      path: /home/agent/.cache/tool
`
	env := `schemaVersion: "1"
agent: claude
kits: [./kits/k]
secrets:
  example:
    ref: op://Private/Example/token
bindings:
  example:
    apiKey:
      domains: [api.example.com]
mounts:
  /home/agent/.cache/tool: ./cache
`
	p := resolve(t, ResolveOptions{Kits: memKits{testProj + "/kits/k": kit}}, layer(testProj+"/a.yaml", env))
	for _, c := range []string{"CREDENTIAL_UNBOUND", "CREDENTIAL_INJECT_NOT_ALLOWED", "BINDING_DOMAIN_MISSING", "SECRET_NOT_DECLARED", "SECRET_UNUSED", "BINDING_UNUSED", "HOST_MOUNT_SHARED", "MOUNT_TARGET_UNKNOWN"} {
		noDiag(t, p, c)
	}
	hm := item(t, p, "kit:k/host-mount@1//home/agent/.cache/tool")
	found := false
	for _, f := range hm.Fields {
		if f.Key == "host directory" && f.Value == "~/proj/cache" {
			found = true
		}
	}
	if !found {
		t.Errorf("host-mount should show its mounts: binding: %+v", hm.Fields)
	}
}

func TestUnknownCapabilityPreserved(t *testing.T) {
	kit := `schemaVersion: "3"
kind: mixin
capabilities:
  - type: io.example/telemetry@2
    optional: true
    config:
      endpoint: https://collector.example.com
      nested:
        list: [a, b]
  - group:
      optional: true
      capabilities:
        - type: com.docker.sandbox/volume@1
          config: {path: /data, size: 1G}
`
	p := resolve(t, ResolveOptions{Redaction: RedactHome, Kits: memKits{testProj + "/kits/t": kit}},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nkits: [./kits/t]\n"))
	it := item(t, p, "kit:t/io.example/telemetry@2")
	got := map[string]string{}
	for _, f := range it.Fields {
		got[f.Key] = f.Value
	}
	if got["endpoint"] != "https://collector.example.com" || got["nested.list[1]"] != "b" || got["optional"] != "true" {
		t.Errorf("unknown capability config must be preserved verbatim: %+v", it.Fields)
	}
	vol := item(t, p, "kit:t/volume@1")
	if !contains(vol.Badges, BadgeUnknown) {
		t.Error("unmodelled capability should carry [UNKNOWN]")
	}
	var optional bool
	for _, f := range vol.Fields {
		if f.Key == "optional" && f.Value == "true" {
			optional = true
		}
	}
	if !optional {
		t.Error("group optional should apply to members")
	}
}

func TestPullAlwaysDev(t *testing.T) {
	for _, tc := range []struct {
		opts string
		want bool
	}{
		{"  template: ghcr.io/example/base:dev\n", true},
		{"  template: ghcr.io/example/base:dev\n  pullPolicy: always\n", true},
		{"  template: ghcr.io/example/base:dev\n  pullPolicy: missing\n", false},
		{"  template: ghcr.io/example/base:1.2.3\n", false},
		{"  template: ghcr.io/example/base@sha256:abcd\n", false},
	} {
		p := resolve(t, ResolveOptions{}, layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nsandboxOptions:\n"+tc.opts))
		if got := len(diags(p, "PULL_ALWAYS_DEV")) > 0; got != tc.want {
			t.Errorf("%q: PULL_ALWAYS_DEV = %v, want %v", tc.opts, got, tc.want)
		}
	}
	p := resolve(t, ResolveOptions{}, layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\nsandboxOptions:\n  template: x/y:dev\n"))
	if it := item(t, p, "sandboxOptions.pullPolicy"); it.Value != "always" || it.History[0].Op != OpDefault {
		t.Errorf("implicit pullPolicy should be shown as a default: %+v", it)
	}
}

func TestUserDefaultsRules(t *testing.T) {
	ud := LayerInput{Path: testHome + "/.sbxenv.yaml", Kind: LayerUserDefaults,
		Content: []byte("schemaVersion: \"1\"\nagent: claude\nname: nope\nworkspace: /elsewhere\n")}
	p := resolve(t, ResolveOptions{}, ud, layer(testProj+"/sbxenv.yaml", "schemaVersion: \"1\"\nagent: codex\n"))
	requireDiag(t, p, "USER_DEFAULTS_NAME", LevelError)
	requireDiag(t, p, "USER_DEFAULTS_WORKSPACE", LevelError)
	if v := item(t, p, "agent").Value; v != "codex" {
		t.Errorf("project file must override user defaults: %q", v)
	}
}

func TestCloudAndFlags(t *testing.T) {
	env := "schemaVersion: \"1\"\nagent: claude\nworkspace: .\nports:\n  - sandbox: 80\nsecrets:\n  github:\n    command: gh auth token\n"
	p := resolve(t, ResolveOptions{Cloud: true, TTL: "2h", OnTimeout: "delete"}, layer(testProj+"/a.yaml", env))
	requireDiag(t, p, "CLOUD_UNSUPPORTED", LevelError)
	requireDiag(t, p, "CLOUD_SECRET_SNAPSHOT", LevelWarning)
	noDiag(t, p, "FLAG_REQUIRES_CLOUD")
	p = resolve(t, ResolveOptions{TTL: "2h"}, layer(testProj+"/a.yaml", env))
	requireDiag(t, p, "FLAG_REQUIRES_CLOUD", LevelError)

	clone := true
	p = resolve(t, ResolveOptions{Name: "override", Clone: &clone, SkipHostCommands: true},
		layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nname: file\nagent: claude\nworkspace: .\nlifecycle:\n  initialize:\n    - command: echo\n"))
	if v := item(t, p, "name").Value; v != "override" {
		t.Errorf("--name = %q", v)
	}
	ws := item(t, p, "workspace")
	if contains(ws.Badges, BadgeRW) {
		t.Error("--clone should drop the RW badge")
	}
	if p.Summary.HostExec != 0 {
		t.Errorf("--skip-host-commands should not count lifecycle as host exec: %+v", p.Summary)
	}
	if !contains(item(t, p, "lifecycle.initialize[0]").Badges, BadgeSkip) {
		t.Error("skipped lifecycle needs [SKIP]")
	}
}

func TestEnvFileInsideWritableWorkspace(t *testing.T) {
	p := resolve(t, ResolveOptions{}, layer(testProj+"/sub/sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nworkspace: ..\n"))
	requireDiag(t, p, "ENV_FILE_WRITABLE", LevelWarning)
	p = resolve(t, ResolveOptions{}, layer(testProj+"/sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nworkspace: .\n"))
	noDiag(t, p, "ENV_FILE_WRITABLE")
}

func TestDocsAttached(t *testing.T) {
	p := resolve(t, ResolveOptions{Redaction: RedactHome}, layer(testProj+"/base.sbxenv.yaml", base))
	for id, rule := range map[string]MergeRule{
		"env.NODE_ENV": MergeMap, "sandboxOptions.memory": MergeReplace, "ports[8080/tcp4]": MergeKeyed,
		"lifecycle.initialize[0]": MergeAppend, "cmd": MergeListReplace,
	} {
		it := item(t, p, id)
		if it.Doc == nil || it.Doc.Merge != rule || it.Doc.Meaning == "" || it.Doc.MergeLabel == "" {
			t.Errorf("%s doc = %+v", id, it.Doc)
		}
	}
	for _, d := range AllDocs() {
		if d.Meaning == "" || d.MergeDetail == "" || d.MergeLabel == "" || d.Reference == "" {
			t.Errorf("incomplete doc entry %s", d.Key)
		}
	}
	if d, ok := LookupDoc("cap:io.example/x@1"); !ok || d.Key != "cap:*" {
		t.Error("unknown capabilities should fall back to cap:*")
	}
}

func TestDeterministic(t *testing.T) {
	a := mustJSON(t, resolve(t, ResolveOptions{}, layer(testProj+"/base.sbxenv.yaml", base), layer(testProj+"/local.sbxenv.yaml", local)))
	for i := 0; i < 5; i++ {
		b := mustJSON(t, resolve(t, ResolveOptions{}, layer(testProj+"/base.sbxenv.yaml", base), layer(testProj+"/local.sbxenv.yaml", local)))
		if a != b {
			t.Fatal("Resolve output is not deterministic")
		}
	}
}

func TestTargetVersionMatchesWatchFile(t *testing.T) {
	b, err := os.ReadFile("../../.github/sbx-version")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != TargetSbxVersion {
		t.Fatalf(".github/sbx-version = %q but TargetSbxVersion = %q; bump both together", got, TargetSbxVersion)
	}
}

func TestOnTimeoutValues(t *testing.T) {
	env := layer(testProj+"/a.yaml", "schemaVersion: \"1\"\nagent: claude\n")
	for _, tc := range []struct {
		action, ttl string
		bad         bool
	}{{"stop", "", false}, {"restart", "2h", false}, {"restart", "30m", true}, {"delete", "", false}, {"pause", "", true}} {
		p := resolve(t, ResolveOptions{Cloud: true, OnTimeout: tc.action, TTL: tc.ttl}, env)
		if got := len(diags(p, "INVALID_VALUE")) > 0; got != tc.bad {
			t.Errorf("%s/%s: invalid = %v", tc.action, tc.ttl, got)
		}
	}
}
