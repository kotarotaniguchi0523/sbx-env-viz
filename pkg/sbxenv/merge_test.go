package sbxenv_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

const baseEnv = `schemaVersion: "1"
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

const localEnv = `env:
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

// layered resolves baseEnv under localEnv, the two-layer plan the merge
// tests below are written against.
//
//declscope:shared // docs_test.go and explain_test.go check the docs and explanations of this same plan
func layered(t *testing.T) *sbxenv.Plan {
	t.Helper()
	f := newFixture(t)
	f.write(t, "base.sbxenv.yaml", baseEnv)
	f.write(t, "local.sbxenv.yaml", localEnv)
	return f.resolve(t, run{paths: []string{"base.sbxenv.yaml", "local.sbxenv.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})
}

func TestScalarOverrideLaterLayerWins(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	if got := item(t, p, "sandboxOptions.memory").Value; got != "12g" {
		t.Errorf("memory = %q, want 12g", got)
	}
	if got := item(t, p, "sandboxOptions.cpus").Value; got != "4" {
		t.Errorf("cpus = %q, want the untouched 4", got)
	}
}

func TestMapsMergeKeyByKey(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	if got := item(t, p, "env.NODE_ENV").Value; got != "test" {
		t.Errorf("NODE_ENV = %q, want test", got)
	}
	if !hasItem(p, "env.KEEP") || !hasItem(p, "env.LOCAL") {
		t.Error("keys from both layers must survive")
	}
}

func TestPortsMatchOnSandboxPortAndKeepFirstPosition(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	var got []string
	for _, it := range p.Items {
		if it.Section == sbxenv.SectionPorts {
			got = append(got, it.ID+"="+it.Value)
		}
	}
	want := []string{
		"ports[8080/tcp4]=loopback:4000 -> sandbox:8080/tcp4",
		"ports[9000/tcp4]=loopback:ephemeral -> sandbox:9000/tcp4",
		"ports[7000/tcp4]=loopback:ephemeral -> sandbox:7000/tcp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ports =\n%v\nwant\n%v", got, want)
	}
}

func TestPortsInOneFileAlsoDeduplicate(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"ports:\n  - sandbox: 80\n    host: 1\n  - sandbox: 80\n    host: 2\n  - sandbox: 80\n    protocol: UDP\n")

	// Act
	p := f.resolve(t, run{opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "ports[80/tcp4]").Value; got != "loopback:2 -> sandbox:80/tcp4" {
		t.Errorf("repeated entry in one file should win: %q", got)
	}
	if !hasItem(p, "ports[80/udp]") {
		t.Error("a different protocol is a separate entry")
	}
}

func TestAdditionalWorkspaceReplacementDropsReadOnly(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	aw := item(t, p, "additionalWorkspaces[~/shared]")
	if strings.Contains(aw.Value, "(ro)") || !has(aw.Badges, sbxenv.BadgeRW) {
		t.Errorf("the replacement omits readOnly, so it must be writable: %+v", aw)
	}
}

func TestMCPServersMatchOnName(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	if got := item(t, p, "mcp.servers[docs]").Value; got != "command: docs-server" {
		t.Errorf("mcp.servers[docs] = %q", got)
	}
}

func TestOtherListsAppend(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	if got := item(t, p, "lifecycle.initialize[0]").Value; got != "echo one" {
		t.Errorf("first = %q", got)
	}
	if got := item(t, p, "lifecycle.initialize[1]").Value; got != "echo two" {
		t.Errorf("appended = %q", got)
	}
}

func TestLaunchListsReplaceInheritAndClear(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "a.yaml", minimal+"entrypoint: [a, b]\ncmd: [x]\n")
	f.write(t, "b.yaml", "entrypoint: [c]\ncmd: []\n")
	f.write(t, "c.yaml", "cmd: null\n")

	// Act
	replaced := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})
	inherited := f.resolve(t, run{paths: []string{"a.yaml", "c.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, replaced, "entrypoint").Value; got != "[c]" {
		t.Errorf("entrypoint = %q, want [c] (replaced, not appended)", got)
	}
	if got := item(t, replaced, "cmd").Value; got != "[]" {
		t.Errorf("cmd = %q, want [] (cleared)", got)
	}
	if got := item(t, inherited, "cmd").Value; got != "[x]" {
		t.Errorf("cmd = %q, want [x] (null inherits)", got)
	}
}

func TestHistoryRecordsShadowedValuesWithLines(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	mem := item(t, p, "sandboxOptions.memory")
	want := []sbxenv.HistoryEntry{
		{Op: sbxenv.OpShadow, Value: "8g", Origin: sbxenv.Origin{SourceID: "L1", Line: 12, Col: 3}},
		{Op: sbxenv.OpReplace, Value: "12g", Origin: sbxenv.Origin{SourceID: "L2", Line: 5, Col: 3}},
	}
	if !reflect.DeepEqual(mem.History, want) {
		t.Errorf("history =\n%+v\nwant\n%+v", mem.History, want)
	}
	if len(mem.Origins) != 1 || mem.Origins[0].SourceID != "L2" || mem.Origins[0].Line != 5 {
		t.Errorf("origins = %+v", mem.Origins)
	}
}

func TestSourcesCarryRedactedPathAndDigest(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	s := p.Meta.Sources[0]
	if s.ID != "L1" || s.Path != "~/proj/base.sbxenv.yaml" || len(s.SHA256) != 64 || s.Layer != 1 || !s.Expanded {
		t.Errorf("source = %+v", s)
	}
}

func TestUserDefaultsAreUnderProjectAndRestricted(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	writeHome(t, f, ".sbxenv.yaml", minimal+"name: nope\nworkspace: /elsewhere\nenv:\n  FROM_HOME: \"1\"\n")
	f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: codex\n")

	// Act
	p := f.resolve(t, run{opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "agent").Value; got != "codex" {
		t.Errorf("project file must override user defaults: %q", got)
	}
	if !hasItem(p, "env.FROM_HOME") {
		t.Error("user defaults must still contribute")
	}
	if p.Meta.Sources[0].Kind != sbxenv.SourceUserDefaults {
		t.Errorf("first source kind = %s", p.Meta.Sources[0].Kind)
	}
	requireDiag(t, p, "USER_DEFAULTS_NAME", sbxenv.LevelError)
	requireDiag(t, p, "USER_DEFAULTS_WORKSPACE", sbxenv.LevelError)
}

func TestExplicitPathSkipsUserDefaults(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	writeHome(t, f, ".sbxenv.yaml", minimal+"env:\n  FROM_HOME: \"1\"\n")
	f.write(t, "sbxenv.yaml", minimal)

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	if hasItem(p, "env.FROM_HOME") {
		t.Error("an explicit path must skip ~/.sbxenv.yaml")
	}
}

// writeHome creates a file directly in the fixture's home directory.
func writeHome(t *testing.T, f fixture, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.home, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeterministicOutput(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "base.sbxenv.yaml", baseEnv)
	f.write(t, "local.sbxenv.yaml", localEnv)
	r := run{paths: []string{"base.sbxenv.yaml", "local.sbxenv.yaml"}}

	// Act
	first := toJSON(t, f.resolve(t, r))
	var again []string
	for i := 0; i < 5; i++ {
		again = append(again, toJSON(t, f.resolve(t, r)))
	}

	// Assert
	for _, a := range again {
		if a != first {
			t.Fatal("Resolve output differs between identical runs")
		}
	}
}
