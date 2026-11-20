package sbxenv_test

import (
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// credKit requests one required apiKey credential, one optional OAuth
// credential, and the egress the apiKey one needs.
const credKit = `schemaVersion: "3"
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
        name: EXAMPLE_TOKEN
        proxyManaged: true
        inject: [{domain: api.example.com, header: Authorization}]
  - type: com.docker.sandbox/credential@1
    optional: true
    config:
      service: other
      phase: runtime
      oauth:
        resourceHosts: [api.other.example]
`

func TestCredentialBindingSecretTriad(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		env     string
		want    map[string]sbxenv.DiagLevel
		notWant []string
	}{
		{
			name: "fully wired",
			env: `secrets:
  example: {ref: op://Private/x/y}
  other: {ref: op://Private/x/z}
bindings:
  example: {apiKey: {domains: [api.example.com]}}
  other: {oauth: {domains: [api.other.example]}}
`,
			notWant: []string{"CREDENTIAL_UNBOUND", "BINDING_DOMAIN_MISSING", "SECRET_NOT_DECLARED", "SECRET_UNUSED", "BINDING_UNUSED"},
		},
		{
			name: "required unbound fails create, optional unbound is skipped",
			env:  "",
			want: map[string]sbxenv.DiagLevel{"CREDENTIAL_UNBOUND": sbxenv.LevelError, "SECRET_NOT_DECLARED": sbxenv.LevelInfo},
		},
		{
			name: "binding for the wrong mechanism",
			env:  "bindings:\n  example: {oauth: {domains: [api.example.com]}}\n",
			want: map[string]sbxenv.DiagLevel{"CREDENTIAL_UNBOUND": sbxenv.LevelError},
		},
		{
			name: "binding misses an inject domain",
			env:  "bindings:\n  example: {apiKey: {domains: [other.example.com]}}\n  other: {oauth: {domains: [api.other.example]}}\n",
			want: map[string]sbxenv.DiagLevel{"BINDING_DOMAIN_MISSING": sbxenv.LevelWarning},
		},
		{
			name: "secret and binding nobody consumes",
			env:  "secrets:\n  stray: {ref: op://Private/a/b}\n  lonely: {ref: op://Private/a/c}\nbindings:\n  stray: {apiKey: {domains: [x.example]}}\n",
			want: map[string]sbxenv.DiagLevel{"BINDING_UNUSED": sbxenv.LevelWarning, "SECRET_UNUSED": sbxenv.LevelWarning},
		},
		{
			name:    "built-in services need no kit credential",
			env:     "secrets:\n  github: {command: gh auth token}\nbindings:\n  github: {apiKey: {domains: [api.github.com]}}\n",
			notWant: []string{"SECRET_UNUSED", "BINDING_UNUSED"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "kits/cred/cred.yaml", credKit)
			f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/cred]\n"+tc.env)

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			for code, level := range tc.want {
				requireDiag(t, p, code, level)
			}
			noDiag(t, p, tc.notWant...)
		})
	}
}

func TestOptionalUnboundCredentialIsOnlyAWarning(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/cred/cred.yaml", credKit)
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/cred]\nbindings:\n  example: {apiKey: {domains: [api.example.com]}}\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	d := requireDiag(t, p, "CREDENTIAL_UNBOUND", sbxenv.LevelWarning)
	if !strings.Contains(d.Message, "other") {
		t.Errorf("the optional credential is the one reported: %s", d.Message)
	}
	for _, x := range diags(p, "CREDENTIAL_UNBOUND") {
		if x.Level == sbxenv.LevelError {
			t.Errorf("bound required credential reported: %s", x.Message)
		}
	}
}

func TestKitCredentialRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kits map[string]string
		code string
	}{
		{"inject domain not in exact allow list", map[string]string{"kits/a/a.yaml": `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config: {runtime: {allow: ["*.example.com"]}}
  - type: com.docker.sandbox/credential@1
    config: {service: s, phase: runtime, apiKey: {inject: [{domain: api.example.com}]}}
`}, "CREDENTIAL_INJECT_NOT_ALLOWED"},
		{"same service and phase in two kits", map[string]string{
			"kits/a/a.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/credential@1\n    config: {service: s, phase: runtime, apiKey: {name: A}}\n",
			"kits/b/b.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/credential@1\n    config: {service: s, phase: [install, runtime], apiKey: {name: B}}\n",
		}, "CREDENTIAL_CONFLICT"},
		{"neither apiKey nor oauth", map[string]string{"kits/a/a.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/credential@1\n    config: {service: s, phase: runtime}\n"}, "CREDENTIAL_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			var refs []string
			for path, c := range tc.kits {
				f.write(t, path, c)
				refs = append(refs, "./"+strings.TrimSuffix(path, "/"+lastSegment(path)))
			}
			f.write(t, "sbxenv.yaml", minimal+"kits: ["+strings.Join(refs, ", ")+"]\n")

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			requireDiag(t, p, tc.code, sbxenv.LevelError)
		})
	}
}

func lastSegment(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func TestHostMountSharingAndMountsBinding(t *testing.T) {
	t.Parallel()
	const kit = "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/host-mount@1\n    config: {path: /home/agent/.cache/x}\n"
	cases := []struct {
		name     string
		mounts   string
		wantCode string
		level    sbxenv.DiagLevel
		bound    bool
	}{
		{"no mounts entry shares one host dir", "", "HOST_MOUNT_SHARED", sbxenv.LevelWarning, false},
		{"mounts entry gives a private dir", "mounts:\n  /home/agent/.cache/x: ./cache\n", "", "", true},
		{"mounts entry with no matching kit path", "mounts:\n  /home/agent/.cache/x: ./cache\n  /nowhere: ./n\n", "MOUNT_TARGET_UNKNOWN", sbxenv.LevelError, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "kits/m/m.yaml", kit)
			f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/m]\n"+tc.mounts)

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

			// Assert
			if tc.wantCode != "" {
				requireDiag(t, p, tc.wantCode, tc.level)
			} else {
				noDiag(t, p, "HOST_MOUNT_SHARED", "MOUNT_TARGET_UNKNOWN")
			}
			dir, ok := field(item(t, p, "kit:m/host-mount@1//home/agent/.cache/x"), "host directory")
			if ok != tc.bound || (tc.bound && dir != "~/proj/cache") {
				t.Errorf("host directory = %q (bound %v)", dir, ok)
			}
		})
	}
}

func TestPullAlwaysWithDevTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		opts string
		want bool
	}{
		{"  template: ghcr.io/example/base:dev\n", true},
		{"  template: ghcr.io/example/base:dev\n  pullPolicy: always\n", true},
		{"  template: ghcr.io/example/base:dev\n  pullPolicy: missing\n", false},
		{"  template: ghcr.io/example/base:1.2.3\n", false},
		{"  template: ghcr.io/example/base@sha256:abcd\n", false},
	}
	for _, tc := range cases {
		t.Run(strings.TrimSpace(tc.opts), func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", minimal+"sandboxOptions:\n"+tc.opts)

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			if got := len(diags(p, "PULL_ALWAYS_DEV")) > 0; got != tc.want {
				t.Errorf("PULL_ALWAYS_DEV = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImplicitPullPolicyIsShownAsDefault(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"sandboxOptions:\n  template: x/y:dev\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	it := item(t, p, "sandboxOptions.pullPolicy")
	if it.Value != "always" || it.History[0].Op != sbxenv.OpDefault {
		t.Errorf("pullPolicy = %+v", it)
	}
	if len(coverage(p, sbxenv.CoverageMutableImage)) != 1 {
		t.Error("a :dev template is outside the analysis and must be listed")
	}
}

func TestSchemaAndValueChecks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  string
		code string
	}{
		{"unknown top-level field", minimal + "wrokspace: .\n", "UNKNOWN_FIELD"},
		{"unknown nested field", minimal + "sandboxOptions:\n  memroy: 1g\n", "UNKNOWN_FIELD"},
		{"type mismatch", minimal + "env: [A, B]\n", "TYPE_MISMATCH"},
		{"schemaVersion wrong", "schemaVersion: \"2\"\nagent: claude\n", "SCHEMA_VERSION"},
		{"schemaVersion missing", "agent: claude\n", "SCHEMA_VERSION"},
		{"agent missing", "schemaVersion: \"1\"\n", "AGENT_MISSING"},
		{"pullPolicy invalid", minimal + "sandboxOptions:\n  pullPolicy: sometimes\n", "INVALID_VALUE"},
		{"skills invalid", minimal + "sandboxOptions:\n  skills: maybe\n", "INVALID_VALUE"},
		{"port protocol invalid", minimal + "ports:\n  - sandbox: 1\n    protocol: sctp\n", "INVALID_VALUE"},
		{"port without sandbox", minimal + "ports:\n  - host: 1\n", "PORT_SANDBOX_MISSING"},
		{"mcp with url and command", minimal + "mcp:\n  servers:\n    - {name: a, url: https://x, command: y}\n", "MCP_SOURCE_COUNT"},
		{"secret with two sources", minimal + "secrets:\n  s: {ref: op://a/b/c, command: x}\n", "SECRET_SOURCE_COUNT"},
		{"snapshot with refresh", minimal + "secrets:\n  s: {command: x, snapshot: true, refresh: 5m}\n", "SECRET_SNAPSHOT_CONFLICT"},
		{"registry without secret", minimal + "registries:\n  ghcr.io: {username: {value: u}}\n", "REGISTRY_SECRET_MISSING"},
		{"lifecycle without command", minimal + "lifecycle:\n  initialize:\n    - name: x\n", "LIFECYCLE_COMMAND_MISSING"},
		{"not a mapping", "- a\n- b\n", "NOT_A_MAPPING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", tc.env)

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			requireDiag(t, p, tc.code, sbxenv.LevelError)
		})
	}
}

func TestSecretsMergedAcrossLayersCanBreakTheOneSourceRule(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "a.yaml", minimal+"secrets:\n  s: {ref: op://a/b/c}\n")
	f.write(t, "b.yaml", "secrets:\n  s: {command: x}\n")

	// Act
	p := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}})

	// Assert
	requireDiag(t, p, "SECRET_SOURCE_COUNT", sbxenv.LevelError)
}

func TestBoundaryWarnings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  string
		kit  string
		code string
	}{
		{"port on every interface", "ports:\n  - {sandbox: 1, hostIP: 0.0.0.0}\n", "", "PORT_ALL_INTERFACES"},
		{"writable env files", "sandboxOptions:\n  writableEnvFiles: true\n", "", "WRITABLE_ENV_FILES"},
		{"skills readwrite", "sandboxOptions:\n  skills: readwrite\n", "", "SKILLS_READWRITE"},
		{"literal secret", "secrets:\n  s: {value: x}\n", "", "SECRET_LITERAL"},
		{"unrestricted ssh agent", "", "  - type: com.docker.sandbox/ssh-agent@1\n    config: {phase: runtime}\n", "SSH_AGENT_UNRESTRICTED"},
		{"privileged", "", "  - type: com.docker.sandbox/privileged@1\n", "PRIVILEGED"},
		{"all runtime egress", "", "  - type: com.docker.sandbox/network-policy@1\n    config: {runtime: {allow: ['**']}}\n", "EGRESS_UNRESTRICTED"},
		{"oauth passthrough", "", "  - type: com.docker.sandbox/credential@1\n    optional: true\n    config: {service: s, phase: runtime, oauth: {passthrough: true}}\n", "OAUTH_PASSTHROUGH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			env := minimal + tc.env
			if tc.kit != "" {
				f.write(t, "kits/k/k.yaml", "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n"+tc.kit)
				env += "kits: [./kits/k]\n"
			}
			f.write(t, "sbxenv.yaml", env)

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			d := requireDiag(t, p, tc.code, sbxenv.LevelWarning)
			if d.ItemID != "" && !has(item(t, p, d.ItemID).Badges, sbxenv.BadgeWarn) {
				t.Errorf("item %s should carry the [!] badge", d.ItemID)
			}
		})
	}
}

func TestBoundedSSHAgentIsNotWarned(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/k/k.yaml", "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/ssh-agent@1\n    config: {phase: runtime, unrestricted: false, sign: [git]}\n")
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/k]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	noDiag(t, p, "SSH_AGENT_UNRESTRICTED")
}

func TestEnvFilesAndKitsInsideWritableMounts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		path  string
		env   string
		code  string
		quiet bool
	}{
		{"env file in a workspace subdirectory", "sub/sbxenv.yaml", minimal + "workspace: ..\n", "ENV_FILE_WRITABLE", false},
		{"env file at the workspace root", "sbxenv.yaml", minimal + "workspace: .\n", "ENV_FILE_WRITABLE", true},
		{"clone mode is not writable", "sub/sbxenv.yaml", minimal + "workspace: {path: .., clone: true}\n", "ENV_FILE_WRITABLE", true},
		{"local kit inside the workspace", "sbxenv.yaml", minimal + "workspace: .\nkits: [./kits/k]\n", "KIT_IN_WRITABLE_MOUNT", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "kits/k/k.yaml", "schemaVersion: \"3\"\nkind: mixin\n")
			f.write(t, tc.path, tc.env)

			// Act
			p := f.resolve(t, run{paths: []string{tc.path}})

			// Assert
			if tc.quiet {
				noDiag(t, p, tc.code)
			} else {
				requireDiag(t, p, tc.code, sbxenv.LevelWarning)
			}
		})
	}
}

func TestCloudMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  string
		opts sbxenv.ResolveOptions
		want map[string]sbxenv.DiagLevel
		none []string
	}{
		{"local-only fields rejected", minimal + "workspace: .\nports: [{sandbox: 1}]\nsandboxOptions: {gpu: true}\n",
			sbxenv.ResolveOptions{Cloud: true}, map[string]sbxenv.DiagLevel{"CLOUD_UNSUPPORTED": sbxenv.LevelError}, nil},
		{"dynamic secret without snapshot", minimal + "secrets:\n  s: {command: x}\n",
			sbxenv.ResolveOptions{Cloud: true}, map[string]sbxenv.DiagLevel{"CLOUD_SECRET_SNAPSHOT": sbxenv.LevelWarning}, nil},
		{"snapshot secret is fine", minimal + "secrets:\n  s: {command: x, snapshot: true}\n",
			sbxenv.ResolveOptions{Cloud: true}, nil, []string{"CLOUD_SECRET_SNAPSHOT"}},
		{"ttl without cloud", minimal, sbxenv.ResolveOptions{TTL: "2h"},
			map[string]sbxenv.DiagLevel{"FLAG_REQUIRES_CLOUD": sbxenv.LevelError}, nil},
		{"restart needs one hour", minimal, sbxenv.ResolveOptions{Cloud: true, OnTimeout: "restart", TTL: "30m"},
			map[string]sbxenv.DiagLevel{"INVALID_VALUE": sbxenv.LevelError}, nil},
		{"restart with two hours", minimal, sbxenv.ResolveOptions{Cloud: true, OnTimeout: "restart", TTL: "2h"}, nil, []string{"INVALID_VALUE"}},
		{"unknown action", minimal, sbxenv.ResolveOptions{Cloud: true, OnTimeout: "pause"},
			map[string]sbxenv.DiagLevel{"INVALID_VALUE": sbxenv.LevelError}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", tc.env)

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: tc.opts})

			// Assert
			for code, level := range tc.want {
				requireDiag(t, p, code, level)
			}
			noDiag(t, p, tc.none...)
		})
	}
}

func TestCommandLineOverrides(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"name: file\nworkspace: .\nlifecycle:\n  initialize:\n    - command: echo\nmcp:\n  servers:\n    - {name: local, command: srv}\n")
	clone := true

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Name: "override", Clone: &clone, SkipHostCommands: true}})

	// Assert
	name := item(t, p, "name")
	if name.Value != "override" || name.History[0].Op != sbxenv.OpShadow {
		t.Errorf("--name should replace the file value: %+v", name)
	}
	if has(item(t, p, "workspace").Badges, sbxenv.BadgeRW) {
		t.Error("--clone means host files are not written directly")
	}
	if p.Summary.HostExec != 0 {
		t.Errorf("--skip-host-commands: host exec = %d, want 0", p.Summary.HostExec)
	}
	for _, id := range []string{"lifecycle.initialize[0]", "mcp.servers[local]"} {
		if !has(item(t, p, id).Badges, sbxenv.BadgeSkip) {
			t.Errorf("%s should be marked [SKIP]", id)
		}
	}
}

func TestSecretCommandsStillRunWithSkipHostCommands(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"secrets:\n  github: {command: gh auth token}\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{SkipHostCommands: true}})

	// Assert
	if p.Summary.HostExec != 1 {
		t.Errorf("host exec = %d, want 1 (secret commands are not skipped)", p.Summary.HostExec)
	}
}

func TestCoverageIsAlwaysPresent(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal)

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	if len(coverage(p, sbxenv.CoverageHostPolicy)) != 1 || len(coverage(p, sbxenv.CoverageBuiltinAgent)) != 1 {
		t.Errorf("even a minimal file lists host policy and the built-in agent: %+v", p.Coverage)
	}
	if p.Summary.Unexpanded != len(p.Coverage) {
		t.Errorf("summary unexpanded = %d, coverage = %d", p.Summary.Unexpanded, len(p.Coverage))
	}
}

func TestParseFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"two documents":  "schemaVersion: \"1\"\n---\nagent: x\n",
		"broken quoting": "env:\n  A: \"open\n",
		"tab indent":     "a:\n\tb: 1\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", content)

			// Act
			_, err := f.resolveErr(t, run{paths: []string{"."}})

			// Assert
			var pe *sbxenv.ParseError
			if !asParseError(err, &pe) {
				t.Fatalf("want *sbxenv.ParseError, got %v", err)
			}
			if pe.Path != "~/proj/sbxenv.yaml" {
				t.Errorf("path = %q, want it home-shortened", pe.Path)
			}
		})
	}
}

func TestDuplicateKeysReportBothPositions(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nenv:\n  A: x\n  B: y\n  A: z\nports:\n  - sandbox: 1\n    sandbox: 2\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	got := map[string][2]int{}
	for _, d := range diags(p, "DUPLICATE_KEY") {
		if d.Level != sbxenv.LevelError || len(d.Origins) != 2 {
			t.Fatalf("duplicate key diagnostic = %+v", d)
		}
		got[d.Message[:strings.Index(d.Message, " is defined")]] = [2]int{d.Origins[0].Line, d.Origins[1].Line}
	}
	want := map[string][2]int{`key "env.A"`: {4, 6}, `key "ports[0].sandbox"`: {8, 9}}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s lines = %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}

func TestSkillsOffStaysTheStringOff(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"sandboxOptions:\n  skills: off\n  cpus: 007\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "sandboxOptions.skills").Value; got != "off" {
		t.Errorf("skills = %q, want the string off (not a YAML 1.1 boolean)", got)
	}
	if got := item(t, p, "sandboxOptions.cpus").Value; got != "007" {
		t.Errorf("cpus = %q, want verbatim 007", got)
	}
	noDiag(t, p, "INVALID_VALUE")
}
