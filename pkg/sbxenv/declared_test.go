package sbxenv_test

import (
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

func TestDeclaredMarksACommandMCPServerSkippedByTheFlag(t *testing.T) {
	t.Parallel()
	for _, skip := range []bool{false, true} {
		// Arrange
		f := newFixture(t)
		f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nmcp:\n  servers:\n    - {name: local, command: local-mcp}\n    - {name: remote, url: \"https://mcp.example.com\"}\n")

		// Act
		p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome, SkipHostCommands: skip}})

		// Assert: only the command server, and only with the flag.
		for _, m := range p.Declared.MCP {
			if want := skip && m.Name == "local"; m.Skipped != want {
				t.Errorf("skip=%v %s: Skipped=%v", skip, m.Name, m.Skipped)
			}
		}
	}
}

func TestAnAdditionalWorkspaceWithoutAPathDoesNotCrash(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"123", "{readOnly: true}", "~"} {
		// Arrange: entries the schema rejects, which sbxray still reads.
		f := newFixture(t)
		f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nadditionalWorkspaces: ["+entry+"]\n")

		// Act: regression; a missing path was formatted and panicked.
		p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

		// Assert
		if p == nil {
			t.Errorf("%s: no plan", entry)
		}
	}
}
