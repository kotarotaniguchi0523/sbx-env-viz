package sbxenv_test

import (
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

const netKit = `# syntax=docker/sandbox-kit:3
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
      install:
        allow: [registry.npmjs.org]
      runtime:
        allow: ["${{ kit.args.host }}", "*.cdn.example.com"]
        deny: [telemetry.example.com]
`

func TestKitEntriesWithSameSourceMergeArgs(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/net/net.yaml", netKit)
	f.write(t, "a.yaml", minimal+"kits:\n  - source: ./kits/net\n    args:\n      host: b.example.com\n      level: \"2\"\n")
	f.write(t, "b.yaml", "kits:\n  - source: ./kits/net\n    args:\n      host: c.example.com\n")

	// Act
	p := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	entry := item(t, p, "kits[./kits/net]")
	if v, _ := field(entry, "arg host"); !strings.HasPrefix(v, "c.example.com") {
		t.Errorf("later host should win: %q", v)
	}
	if v, _ := field(entry, "arg level"); !strings.HasPrefix(v, "2") {
		t.Errorf("earlier level should survive: %q", v)
	}
	if !hasItem(p, "kit:net/network-policy@1/runtime.allow/c.example.com") {
		t.Error("the merged arg must flow into the expanded capability")
	}
}

func TestKitArgFlagsOverrideEntryArgs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		flags     []string
		file      string
		wantAllow string
	}{
		{"scoped flag", []string{"net.host=d.example.com"}, "", "d.example.com"},
		{"unscoped flag", []string{"host=e.example.com"}, "", "e.example.com"},
		{"kit args file", nil, "host=f.example.com\n", "f.example.com"},
		{"flag beats file", []string{"host=g.example.com"}, "host=f.example.com\n", "g.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "kits/net/net.yaml", netKit)
			f.write(t, "sbxenv.yaml", minimal+"kits:\n  - source: ./kits/net\n    args:\n      host: b.example.com\n")
			var files []string
			if tc.file != "" {
				f.write(t, "k.args", tc.file)
				files = []string{"k.args"}
			}

			// Act
			p := f.resolve(t, run{paths: []string{"."}, kitArgsFiles: files, opts: sbxenv.ResolveOptions{KitArgs: tc.flags, Redaction: sbxenv.RedactHome}})

			// Assert
			if !hasItem(p, "kit:net/network-policy@1/runtime.allow/"+tc.wantAllow) {
				t.Errorf("expected runtime allow %s", tc.wantAllow)
			}
		})
	}
}

func TestKitArgProblems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		flags []string
		code  string
		level sbxenv.DiagLevel
	}{
		{"undeclared entry arg", "    args:\n      nope: x\n", nil, "KIT_ARG_UNDECLARED", sbxenv.LevelError},
		{"flag matches nothing", "", []string{"nope=x"}, "KIT_ARG_UNUSED", sbxenv.LevelWarning},
		{"malformed flag", "", []string{"novalue"}, "KIT_ARG_SYNTAX", sbxenv.LevelError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "kits/net/net.yaml", netKit)
			f.write(t, "sbxenv.yaml", minimal+"kits:\n  - source: ./kits/net\n"+tc.entry)

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{KitArgs: tc.flags}})

			// Assert
			requireDiag(t, p, tc.code, tc.level)
		})
	}
}

func TestCapabilityOriginsPointIntoTheKitViaTheEnvFile(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/net/net.yaml", netKit)
	f.write(t, "sbxenv.yaml", minimal+"kits:\n  - ./kits/net\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	o := item(t, p, "kit:net/network-policy@1/runtime.allow/a.example.com").Origins[0]
	if o.SourceID != "K1" || o.Line != 15 {
		t.Errorf("origin = %s:%d, want K1:15", o.SourceID, o.Line)
	}
	if len(o.Via) != 1 || !strings.Contains(o.Via[0], "L1:4") || !strings.Contains(o.Via[0], "./kits/net") {
		t.Errorf("via = %v, want the kits entry at L1:4", o.Via)
	}
	var kitSrc *sbxenv.Source
	for i := range p.Meta.Sources {
		if p.Meta.Sources[i].ID == "K1" {
			kitSrc = &p.Meta.Sources[i]
		}
	}
	if kitSrc == nil || kitSrc.Path != "~/proj/kits/net/net.yaml" || !kitSrc.Expanded {
		t.Errorf("kit source = %+v", kitSrc)
	}
}

func TestNetworkPolicyIsSplitByPhaseAndRated(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/net/net.yaml", netKit)
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/net]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	cases := map[string]sbxenv.Severity{
		"kit:net/network-policy@1/install.allow/registry.npmjs.org":   sbxenv.SeverityLow,
		"kit:net/network-policy@1/runtime.allow/a.example.com":        sbxenv.SeverityLow,
		"kit:net/network-policy@1/runtime.allow/*.cdn.example.com":    sbxenv.SeverityMedium,
		"kit:net/network-policy@1/runtime.deny/telemetry.example.com": sbxenv.SeverityInfo,
	}
	for id, risk := range cases {
		if got := item(t, p, id).Risk; got != risk {
			t.Errorf("%s risk = %s, want %s", id, got, risk)
		}
	}
	if p.Summary.EgressAllows != 3 {
		t.Errorf("egress allows = %d, want 3 (deny is not an allow)", p.Summary.EgressAllows)
	}
}

func TestEveryModelledCapabilityIsExpanded(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/all/all.yaml", `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: [api.example.com]
  - type: com.docker.sandbox/credential@1
    config:
      service: example
      phase: [install, runtime]
      apiKey:
        name: EXAMPLE_TOKEN
        proxyManaged: true
        inject: [{domain: api.example.com, header: Authorization}]
  - type: com.docker.sandbox/host-mount@1
    config: {path: /home/agent/.cache/x, mode: "0700"}
  - type: com.docker.sandbox/ssh-agent@1
    config: {phase: runtime, unrestricted: false, sign: [git], authenticate: [git@github.com]}
  - type: com.docker.sandbox/git-identity@1
  - type: com.docker.sandbox/lifecycle@1
    config:
      install:
        - command: [sh, -c, "echo hi"]
          user: "0"
          env: [HTTPS_PROXY]
      startup:
        - command: echo boot
      files:
        - path: /home/agent/.config/x.json
          content: "{}"
          mode: "0644"
      interactive: [--tui]
`)
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/all]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	want := map[string]string{
		"kit:all/network-policy@1/runtime.allow/api.example.com": "runtime allow api.example.com",
		"kit:all/credential@1/example/install,runtime":           "service=example phase=install,runtime env=EXAMPLE_TOKEN inject=api.example.com",
		"kit:all/host-mount@1//home/agent/.cache/x":              "/home/agent/.cache/x",
		"kit:all/ssh-agent@1/runtime":                            "phase=runtime sign=git authenticate=git@github.com",
		"kit:all/git-identity@1":                                 "user.name / user.email from the host",
		"kit:all/lifecycle@1/install[0]":                         "sh -c echo hi",
		"kit:all/lifecycle@1/startup[0]":                         "echo boot",
		"kit:all/lifecycle@1/files[0]":                           "/home/agent/.config/x.json",
		"kit:all/lifecycle@1/interactive":                        "--tui",
	}
	for id, v := range want {
		if got := item(t, p, id).Value; got != v {
			t.Errorf("%s = %q, want %q", id, got, v)
		}
	}
	if len(coverage(p, sbxenv.CoverageUnknownCap))+len(coverage(p, sbxenv.CoverageUnmodeledCap)) != 0 {
		t.Errorf("modelled capabilities must not be listed as unknown: %+v", p.Coverage)
	}
}

func TestUnknownCapabilitiesAreKeptVerbatim(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/t/t.yaml", `schemaVersion: "3"
kind: mixin
capabilities:
  - type: io.example/telemetry@2
    config:
      endpoint: https://collector.example.com
      nested:
        list: [a, b]
  - group:
      optional: true
      capabilities:
        - type: com.docker.sandbox/volume@1
          config: {path: /data, size: 1G}
`)
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/t]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	tel := item(t, p, "kit:t/io.example/telemetry@2")
	for k, v := range map[string]string{"endpoint": "https://collector.example.com", "nested.list[0]": "a", "nested.list[1]": "b"} {
		if got, _ := field(tel, k); got != v {
			t.Errorf("unknown config %s = %q, want %q", k, got, v)
		}
	}
	if !has(tel.Badges, sbxenv.BadgeUnknown) {
		t.Error("unknown capability needs the [UNKNOWN] badge")
	}
	requireDiag(t, p, "UNKNOWN_CAPABILITY_REQUIRED", sbxenv.LevelWarning)
	if len(coverage(p, sbxenv.CoverageUnknownCap)) != 1 || len(coverage(p, sbxenv.CoverageUnmodeledCap)) != 1 {
		t.Errorf("coverage = %+v", p.Coverage)
	}
	if v, _ := field(item(t, p, "kit:t/volume@1"), "optional"); v != "true" {
		t.Error("a group's optional applies to its members")
	}
}

func TestRemoteKitsAreCoverageNotGuesses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		source     string
		handle     string
		unpinned   bool
		reasonPart string
	}{
		{"docker.io/sbx/playwright:1.61.1", "playwright", true, "not digest-pinned"},
		{"docker.io/sbx/playwright:1.61.1@sha256:0123", "playwright", false, "OCI kits"},
		{"git+https://github.com/org/repo.git#ref=abc&dir=kits/tool", "tool", false, "Git kits"},
		{"git+ssh://git@github.com/org/thing.git", "thing", true, "no ref= pin"},
		{"./dist/bundle.zip", "bundle", false, "ZIP"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: "+tc.handle+"\nkits:\n  - "+tc.source+"\n")

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

			// Assert
			cov := coverage(p, sbxenv.CoverageRemoteKit)
			if len(cov) != 1 || !strings.Contains(cov[0].Reason, tc.reasonPart) {
				t.Fatalf("coverage = %+v", cov)
			}
			if tc.unpinned != strings.Contains(cov[0].Reason, "pin") {
				t.Errorf("pinning note mismatch: %q", cov[0].Reason)
			}
			it := item(t, p, "kits["+tc.source+"]")
			if h, _ := field(it, "handle"); h != tc.handle {
				t.Errorf("handle = %q, want %q", h, tc.handle)
			}
			if len(coverage(p, sbxenv.CoverageBuiltinAgent)) != 0 {
				t.Error("agent matching a kit handle is not a built-in agent")
			}
		})
	}
}

func TestLocalKitFailuresAreVisible(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		files  map[string]string
		code   string
		cover  sbxenv.CoverageKind
		status string
	}{
		{"missing directory", nil, "KIT_UNREADABLE", sbxenv.CoverageKitLoadFailed, ""},
		{"legacy v2 spec", map[string]string{"kits/k/spec.yaml": "schemaVersion: \"2\"\nkind: sandbox\n"}, "", sbxenv.CoverageLegacyKit, "legacy or unknown kit schema"},
		{"broken yaml", map[string]string{"kits/k/k.yaml": "schemaVersion: \"3\"\ncapabilities: [\n"}, "KIT_PARSE", "", "parse error"},
		{"unknown field", map[string]string{"kits/k/k.yaml": "schemaVersion: \"3\"\nkind: mixin\nnmae: x\n"}, "KIT_UNKNOWN_FIELD", "", ""},
		{"strict capability config", map[string]string{"kits/k/k.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/host-mount@1\n    config: {path: /x, hostPath: /etc}\n"}, "KIT_UNKNOWN_FIELD", "", ""},
		{"git-identity with config", map[string]string{"kits/k/k.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/git-identity@1\n    config: {}\n"}, "KIT_CONFIG_NOT_ALLOWED", "", ""},
		{"undeclared kit expression", map[string]string{"kits/k/k.yaml": "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/host-mount@1\n    config: {path: \"${{ kit.args.nope }}\"}\n"}, "KIT_UNRESOLVED_EXPR", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			for name, c := range tc.files {
				f.write(t, name, c)
			}
			f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/k]\n")

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			if tc.code != "" {
				requireDiag(t, p, tc.code, sbxenv.LevelError)
			}
			if tc.cover != "" && len(coverage(p, tc.cover)) != 1 {
				t.Errorf("coverage %s missing: %+v", tc.cover, p.Coverage)
			}
			if tc.status != "" {
				found := false
				for _, s := range p.Meta.Sources {
					if s.Kind == sbxenv.SourceKit && s.Status == tc.status && !s.Expanded {
						found = true
					}
				}
				if !found {
					t.Errorf("kit source should be marked %q: %+v", tc.status, p.Meta.Sources)
				}
			}
		})
	}
}

func TestAgentMustBeWorkloadWhenItNamesALocalKit(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/tool/tool.yaml", "schemaVersion: \"3\"\nkind: mixin\n")
	f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: tool\nkits: [./kits/tool]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	requireDiag(t, p, "AGENT_NOT_WORKLOAD", sbxenv.LevelError)
}

func TestKitEnvReferencesStayUnexpandedAndAreListed(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/h/h.yaml", "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n  - type: com.docker.sandbox/lifecycle@1\n    config:\n      files:\n        - path: \"${{ kit.env.HOME }}/.rc\"\n")
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/h]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "kit:h/lifecycle@1/files[0]").Value; got != "${{ kit.env.HOME }}/.rc" {
		t.Errorf("value = %q", got)
	}
	if len(coverage(p, sbxenv.CoverageKitEnvRef)) != 1 {
		t.Errorf("coverage = %+v", p.Coverage)
	}
}
