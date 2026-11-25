package sbxenv_test

import (
	"os"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

func TestItemsCarryJapaneseDocsWithTheMergeRuleThatApplies(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := layered(t)

	// Assert
	cases := map[string]sbxenv.MergeRule{
		"env.NODE_ENV":            sbxenv.MergeMap,
		"sandboxOptions.memory":   sbxenv.MergeReplace,
		"ports[8080/tcp4]":        sbxenv.MergeKeyed,
		"mcp.servers[docs]":       sbxenv.MergeKeyed,
		"lifecycle.initialize[0]": sbxenv.MergeAppend,
		"cmd":                     sbxenv.MergeListReplace,
	}
	for id, rule := range cases {
		doc := item(t, p, id).Doc
		if doc == nil {
			t.Errorf("%s has no doc", id)
			continue
		}
		if doc.Merge != rule || doc.MergeLabel != sbxenv.MergeLabel(rule) {
			t.Errorf("%s merge = %s (%s), want %s", id, doc.Merge, doc.MergeLabel, rule)
		}
		if !containsJapanese(doc.Meaning) || !containsJapanese(doc.MergeDetail) {
			t.Errorf("%s doc should explain in Japanese: %+v", id, doc)
		}
	}
}

func TestEveryDocEntryIsComplete(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	docs := sbxenv.AllDocs()

	// Assert
	if len(docs) < 30 {
		t.Fatalf("only %d entries", len(docs))
	}
	for _, d := range docs {
		if !containsJapanese(d.Meaning) || d.MergeDetail == "" || d.MergeLabel == "" || !strings.HasPrefix(d.Reference, "https://") {
			t.Errorf("incomplete entry %s: %+v", d.Key, d)
		}
	}
}

func TestCapabilityDocsFallBackForUnknownTypes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"cap:network-policy@1":   "cap:network-policy@1",
		"cap:io.example/x@1":     "cap:*",
		"cap:volume@1":           "cap:*",
		"bindings.github.apiKey": "bindings.*.apiKey",
		"env.ANYTHING":           "env.*",
	}
	for path, wantKey := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			// Act
			doc, ok := sbxenv.LookupDoc(path)

			// Assert
			if !ok || doc.Key != wantKey {
				t.Errorf("LookupDoc(%q) = %q, %v; want %q", path, doc.Key, ok, wantKey)
			}
		})
	}
}

func TestTargetVersionMatchesTheWatcherState(t *testing.T) {
	t.Parallel()
	// Arrange
	b, err := os.ReadFile("../../.github/sbx-version")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	tracked := strings.TrimSpace(string(b))

	// Assert
	if tracked != sbxenv.TargetSbxVersion {
		t.Fatalf(".github/sbx-version = %q but TargetSbxVersion = %q; bump both together", tracked, sbxenv.TargetSbxVersion)
	}
}

func TestSummaryLineReportsEveryCount(t *testing.T) {
	t.Parallel()
	// Arrange
	s := sbxenv.Summary{HostExec: 1, HostPersistent: 2, RWMounts: 3, EgressAllows: 4, Errors: 5, Warnings: 6, Unexpanded: 7}

	// Act
	line := sbxenv.SummaryLine(s)

	// Assert
	for _, want := range []string{"Host実行 1", "Host永続 2", "RWマウント 3", "外への許可 4", "エラー 5", "警告 6", "未展開 7"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing %q", line, want)
		}
	}
}

func containsJapanese(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30ff || r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
