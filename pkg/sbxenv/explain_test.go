package sbxenv_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

func TestEveryItemSaysInJapaneseWhyItGotItsRisk(t *testing.T) {
	t.Parallel()
	// Arrange: the repository sample, whose kits request every modelled
	// capability, on top of the merge fixture.
	f := newFixture(t)
	if err := os.CopyFS(f.proj, os.DirFS(filepath.Join("..", "..", "testdata", "layered"))); err != nil {
		t.Fatal(err)
	}

	// Act
	sample := f.resolve(t, run{paths: []string{"base.sbxenv.yaml", "local.sbxenv.yaml"}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})
	items := append(layered(t).Items, sample.Items...)

	// Assert
	if len(sample.Items) < 20 {
		t.Fatalf("sample has only %d items", len(sample.Items))
	}
	for _, it := range items {
		if !containsJapanese(it.RiskReason) {
			t.Errorf("%s (%s) has no Japanese risk reason: %q", it.ID, it.Risk, it.RiskReason)
		}
	}
}

// emitted lists every diagnostic code the library can produce, read from
// its own source so that a new code without wording fails this test.
func emitted(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?:diag\(|code :?= )"([A-Z][A-Z_]+)"`)
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	return out
}

func TestEveryDiagnosticCodeHasPlainJapaneseWording(t *testing.T) {
	t.Parallel()
	// Arrange
	codes := emitted(t)
	if len(codes) < 50 {
		t.Fatalf("found only %d codes; the source scan is broken", len(codes))
	}

	for _, code := range codes {
		// Act
		e := sbxenv.ExplainDiagnostic(code)

		// Assert
		if !containsJapanese(e.Title) || !containsJapanese(e.What) || strings.Contains(e.Title, code) {
			t.Errorf("%s: %+v", code, e)
		}
	}
}

func TestUnknownDiagnosticCodesStillGetJapanese(t *testing.T) {
	t.Parallel()
	// Act
	e := sbxenv.ExplainDiagnostic("NOT_A_REAL_CODE")

	// Assert
	if !containsJapanese(e.What) {
		t.Errorf("fallback = %+v", e)
	}
}

var allCoverage = []sbxenv.CoverageKind{
	sbxenv.CoverageRemoteKit, sbxenv.CoverageBuiltinAgent, sbxenv.CoverageMutableImage,
	sbxenv.CoverageHostPolicy, sbxenv.CoverageUnknownCap, sbxenv.CoverageUnmodeledCap,
	sbxenv.CoverageKitEnvRef, sbxenv.CoverageKitLoadFailed, sbxenv.CoverageLegacyKit,
	sbxenv.CoverageGlobalSecrets, sbxenv.CoverageNoKitResolver, sbxenv.CoverageWorkloadKitArg,
}

func TestEveryCoverageKindIsExplainedForBeginners(t *testing.T) {
	t.Parallel()
	for _, k := range allCoverage {
		// Act
		title, why := sbxenv.CoverageTitle(k), sbxenv.ExplainCoverage(k)

		// Assert
		if !containsJapanese(title) || !containsJapanese(why) || strings.Contains(why, string(k)) {
			t.Errorf("%s: title %q, why %q", k, title, why)
		}
	}
	for _, k := range []sbxenv.CoverageKind{sbxenv.CoverageRemoteKit, sbxenv.CoverageBuiltinAgent, sbxenv.CoverageMutableImage} {
		if why := sbxenv.ExplainCoverage(k); !strings.Contains(why, "静的解析") {
			t.Errorf("%s should say the tool never fetches or runs anything: %q", k, why)
		}
	}
}

func TestBoundariesAreDescribedWithoutMachineWords(t *testing.T) {
	t.Parallel()
	// Arrange
	p := layered(t)
	machine := regexp.MustCompile(`host-exec|host-mount|network-egress|proxy-route|host-persistent|sandbox-scoped|invocation|OPAQUE`)
	if len(p.Boundaries) == 0 {
		t.Fatal("no boundaries")
	}

	for _, b := range p.Boundaries {
		// Act
		title, text := sbxenv.BoundaryTitle(b.Kind), sbxenv.ExplainBoundary(b)

		// Assert
		if !containsJapanese(title) || !containsJapanese(text) || machine.MatchString(title+text) {
			t.Errorf("%s/%s: %q %q", b.ItemID, b.Kind, title, text)
		}
	}
}

func TestDocsAvoidJargonInTheirFirstLine(t *testing.T) {
	t.Parallel()
	jargon := regexp.MustCompile(`ループバック|ダイジェスト|エフェメラル|和集合|シングルトン`)
	for _, d := range sbxenv.AllDocs() {
		if jargon.MatchString(d.Meaning + d.Detail + d.MergeDetail + d.MergeLabel) {
			t.Errorf("%s uses jargon: %q", d.Key, d.Meaning)
		}
	}
}

func TestOfficialNotesQuoteTheDocsAndNameTheirSource(t *testing.T) {
	t.Parallel()
	// Arrange: fields whose security premise the official docs state.
	want := map[string]string{
		"sandboxOptions.writableEnvFiles": "信頼できる場合にだけ",
		"lifecycle.initialize[]":          "--skip-host-commands",
		"mcp.servers[]":                   "--skip-host-commands",
		"secrets.*":                       "書き込み可能な sandbox マウントの外",
		"cap:ssh-agent@1":                 "unrestricted の既定は true",
		"cap:network-policy@1":            "deny が優先",
		"workspace":                       "clone: true",
		"additionalWorkspaces[]":          "readOnly の既定は false",
	}
	docs := map[string]sbxenv.FieldDoc{}
	for _, d := range sbxenv.AllDocs() {
		docs[d.Key] = d
	}

	for key, phrase := range want {
		// Act
		d, ok := docs[key]

		// Assert
		if !ok {
			t.Errorf("no doc %s", key)
			continue
		}
		if !strings.Contains(d.OfficialNote, phrase) {
			t.Errorf("%s official note %q lacks %q", key, d.OfficialNote, phrase)
		}
		if len(d.OfficialRefs) == 0 {
			t.Errorf("%s official note has no source", key)
		}
		for _, ref := range d.OfficialRefs {
			if !strings.HasPrefix(ref, "https://docs.docker.com/ai/sandboxes/") && !strings.HasPrefix(ref, "https://github.com/docker/sandbox-kit-spec/") {
				t.Errorf("%s official source %q is not an official document", key, ref)
			}
		}
	}
	for _, d := range sbxenv.AllDocs() {
		if (d.OfficialNote == "") != (len(d.OfficialRefs) == 0) {
			t.Errorf("%s: a note needs its source and a source needs its note", d.Key)
		}
	}
}

// TestEveryCitedSectionExists checks every documentation link against the
// anchors recorded from the published pages, so a mistyped or invented
// section fails without network access.
func TestEveryCitedSectionExists(t *testing.T) {
	t.Parallel()
	// Arrange
	b, err := os.ReadFile(filepath.Join("testdata", "official-anchors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			known[l] = true
		}
	}
	const env = "https://docs.docker.com/ai/sandboxes/configuration/environment-files/#"
	const spec = "https://github.com/docker/sandbox-kit-spec/blob/main/docs/spec/"

	for _, d := range sbxenv.AllDocs() {
		for _, ref := range append([]string{d.Reference}, d.OfficialRefs...) {
			// Act
			var key string
			switch {
			case strings.HasPrefix(ref, env):
				key = strings.TrimPrefix(ref, env)
			case strings.HasPrefix(ref, spec):
				key = strings.TrimPrefix(ref, spec)
			default:
				t.Errorf("%s cites %q, which is not an official document", d.Key, ref)
				continue
			}

			// Assert
			if !known[key] {
				t.Errorf("%s cites %q, which the published documentation does not have", d.Key, ref)
			}
		}
	}
}

// TestExternalStandardsCiteOnlyCheckedEntries fails when an OWASP
// identifier, title, or link is not one that was read in the source.
func TestExternalStandardsCiteOnlyCheckedEntries(t *testing.T) {
	t.Parallel()
	// Arrange
	b, err := os.ReadFile(filepath.Join("testdata", "external-standards.txt"))
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.Split(l, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		checked[strings.Join(parts, "|")] = true
	}
	cited := 0

	for _, d := range sbxenv.AllDocs() {
		for _, x := range d.External {
			// Act
			key := x.ID + "|" + x.Title + "|" + x.URL
			cited++

			// Assert
			if !checked[key] {
				t.Errorf("%s cites %q, which is not a checked entry", d.Key, key)
			}
			if !containsJapanese(x.Why) || x.Standard == "" {
				t.Errorf("%s: %+v needs a standard and a Japanese reason", d.Key, x)
			}
		}
	}
	if cited == 0 {
		t.Fatal("no external standard is cited")
	}
}

func TestExternalStandardsCoverTheRiskiestFields(t *testing.T) {
	t.Parallel()
	// Arrange
	want := []string{"secrets.*", "env.*", "bindings.*.apiKey", "cap:credential@1", "mcp.servers[]", "kits[]",
		"sandboxOptions.template", "lifecycle.initialize[]", "sandboxOptions.writableEnvFiles", "cap:ssh-agent@1",
		"workspace", "additionalWorkspaces[]", "mounts.*", "sandboxOptions.skills", "cap:host-mount@1", "cap:network-policy@1"}
	docs := map[string]sbxenv.FieldDoc{}
	for _, d := range sbxenv.AllDocs() {
		docs[d.Key] = d
	}

	for _, k := range want {
		// Act / Assert
		if len(docs[k].External) == 0 {
			t.Errorf("%s has no external standard", k)
		}
	}
}
