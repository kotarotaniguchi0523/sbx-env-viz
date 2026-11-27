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

func TestAPortOnAnyLoopbackAddressIsLow(t *testing.T) {
	t.Parallel()
	for ip, want := range map[string]sbxenv.Severity{"127.0.0.2": sbxenv.SeverityLow, "192.168.1.10": sbxenv.SeverityMedium, "0.0.0.0": sbxenv.SeverityHigh} {
		// Arrange
		f := newFixture(t)
		f.write(t, "sbxenv.yaml", "schemaVersion: \"1\"\nagent: claude\nports:\n  - {host: 8080, sandbox: 80, hostIP: \""+ip+"\"}\n")

		// Act
		p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

		// Assert: regression; 127.0.0.2 counted as a network address.
		for _, it := range p.Items {
			if it.Path == "ports[]" && it.Risk != want {
				t.Errorf("%s: %s, want %s", ip, it.Risk, want)
			}
		}
	}
}
