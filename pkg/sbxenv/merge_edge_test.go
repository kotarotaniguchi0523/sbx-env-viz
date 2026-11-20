package sbxenv_test

import (
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

func TestKeyedListsDeduplicateAfterATypeChange(t *testing.T) {
	t.Parallel()
	// Arrange: the earlier layer writes ports and mcp with the wrong type.
	f := newFixture(t)
	f.write(t, "a.yaml", minimal+"mcp: none\nports: 80\n")
	f.write(t, "b.yaml", "ports:\n  - {sandbox: 80, host: 1}\n  - {sandbox: 80, host: 2}\nmcp:\n  servers:\n    - {name: s, url: https://one.test}\n    - {name: s, url: https://two.test}\n")

	// Act
	p := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if hasItem(p, "ports[80/tcp4]#2") || hasItem(p, "mcp.servers[s]#2") {
		t.Error("entries with the same key must collapse to the last one, as in a single file")
	}
	if got := item(t, p, "ports[80/tcp4]").Value; !strings.Contains(got, ":2 ") {
		t.Errorf("port = %q, want host 2", got)
	}
}

func TestArgDeclarationsMergeAcrossLayers(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "a.yaml", minimal+"args:\n  M:\n    default: a\n    enum: [a, b]\nenv:\n  M: ${{ env.args.M }}\n")
	f.write(t, "b.yaml", "args:\n  M:\n    default: c\n    enum: [c]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome, EnvArgs: []string{"M=a"}}})

	// Assert
	noDiag(t, p, "ENV_ARG_INVALID")
	it := item(t, p, "args.M")
	var defaults []string
	for _, h := range it.History {
		defaults = append(defaults, string(h.Op)+":"+h.Value+"@"+h.Origin.SourceID)
	}
	joined := strings.Join(defaults, " ")
	if !strings.Contains(joined, "a@L1") || !strings.Contains(joined, "c@L2") {
		t.Errorf("history should show both layers' defaults: %s", joined)
	}
	if v, _ := field(it, "enum"); v != "a | b | c" {
		t.Errorf("enum = %q, want lists appended", v)
	}
}

func TestYAMLMergeKeysAreResolved(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+`base: &base
  FOO: one
  BAR: two
env:
  <<: *base
  BAR: override
mcp:
  servers:
    - &srv {name: one, url: https://one.test}
    - <<: *srv
      name: two
`)

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "env.FOO").Value; got != "one" {
		t.Errorf("env.FOO = %q, want inherited from the merge key", got)
	}
	if got := item(t, p, "env.BAR").Value; got != "override" {
		t.Errorf("env.BAR = %q, explicit keys win over merged ones", got)
	}
	if got := item(t, p, "mcp.servers[two]").Value; got != "url: https://one.test" {
		t.Errorf("mcp two = %q", got)
	}
	noDiag(t, p, "MCP_SOURCE_COUNT")
	for _, d := range diags(p, "UNKNOWN_FIELD") {
		if strings.Contains(d.Message, "<<") {
			t.Errorf("merge key reported as a field: %s", d.Message)
		}
	}
}

func TestIPv6HostAddressIsBracketed(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"ports:\n  - {sandbox: 9000, host: 2000, hostIP: \"::1\"}\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if got := item(t, p, "ports[9000/tcp6]").Value; got != "[::1]:2000 -> sandbox:9000/tcp6" {
		t.Errorf("port = %q", got)
	}
}
