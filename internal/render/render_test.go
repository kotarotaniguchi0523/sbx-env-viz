package render_test

// Black-box tests of the three renderers. Each case builds its own plan
// from real files in its own temporary directory and inspects only what a
// user would see: the rendered text, the JSON document, or the TUI screen.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/text"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

const baseEnv = `schemaVersion: "1"
agent: claude
workspace: .
env:
  NODE_ENV: test
ports:
  - sandbox: 3000
    hostIP: 0.0.0.0
lifecycle:
  initialize:
    - command: echo hi
sandboxOptions:
  template: ghcr.io/example/x:dev
kits:
  - ./kits/net
`

const localEnv = `env:
  NODE_ENV: prod
kits:
  - docker.io/sbx/playwright:1.61.1
`

const netKit = `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: [api.example.com]
`

// newPlan writes the two-layer example into a fresh home and resolves it.
func newPlan(t *testing.T, mode sbxenv.Redaction) (*sbxenv.Plan, string) {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	for rel, c := range map[string]string{"sbxenv.yaml": baseEnv, "local.yaml": localEnv, "kits/net/net.yaml": netKit} {
		p := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := loader.Load(loader.Request{Paths: []string{"sbxenv.yaml", "local.yaml"}, WorkDir: proj, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sbxenv.Resolve(res.Layers, sbxenv.ResolveOptions{ProjectDir: res.ProjectDir, HomeDir: home, Kits: loader.FSKits{}, Redaction: mode})
	if err != nil {
		t.Fatal(err)
	}
	return p, home
}

// --- JSON ---------------------------------------------------------------

func TestJSONHasTheStableTopLevelShape(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactStrict)
	var buf bytes.Buffer

	// Act
	err := jsonout.Write(&buf, p)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"meta", "invocation", "items", "boundaries", "coverage", "diagnostics", "summary"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing top-level %q", k)
		}
	}
	if doc["meta"].(map[string]any)["schemaVersion"] != "1" {
		t.Error("schemaVersion must be \"1\"")
	}
}

func TestJSONIsIdenticalForIndependentlyBuiltEqualPlans(t *testing.T) {
	t.Parallel()
	// Arrange
	p1, home1 := newPlan(t, sbxenv.RedactStrict)
	p2, home2 := newPlan(t, sbxenv.RedactStrict)
	var a, b bytes.Buffer

	// Act
	_ = jsonout.Write(&a, p1)
	_ = jsonout.Write(&b, p2)

	// Assert: different temp homes, same project → same document after ~ shortening.
	if home1 == home2 || a.String() != b.String() {
		t.Error("JSON must not depend on where the home directory is or on map order")
	}
}

func TestJSONSortsSourcesNumerically(t *testing.T) {
	t.Parallel()
	// Arrange
	home := t.TempDir()
	var layers []sbxenv.LayerInput
	for i := 1; i <= 11; i++ {
		content := "env:\n  K: \"x\"\n"
		if i == 1 {
			content = "schemaVersion: \"1\"\nagent: claude\n"
		}
		layers = append(layers, sbxenv.LayerInput{Path: filepath.Join(home, "f", strings.Repeat("a", i)+".yaml"), Content: []byte(content), Kind: sbxenv.LayerEnv})
	}
	p, err := sbxenv.Resolve(layers, sbxenv.ResolveOptions{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer

	// Act
	_ = jsonout.Write(&buf, p)

	// Assert
	var doc struct {
		Meta struct {
			Sources []struct{ ID string } `json:"sources"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(buf.Bytes(), &doc)
	var ids []string
	for _, s := range doc.Meta.Sources {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids[:11], ","); got != "L1,L2,L3,L4,L5,L6,L7,L8,L9,L10,L11" {
		t.Errorf("source order = %s", got)
	}
}

// --- text -----------------------------------------------------------------

func TestTextListsEveryHighRiskEntry(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactHome)
	var buf bytes.Buffer

	// Act
	err := text.Write(&buf, p)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"宣言の静的解析", "PORT_ALL_INTERFACES", "PULL_ALWAYS_DEV", "lifecycle.initialize[0]", "~/proj/sbxenv.yaml:", "env.NODE_ENV = prod", "(上書き済み) test", "remote-kit", "== Sources =="} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q", want)
		}
	}
}

func TestTextCapsLowEntriesButNeverHighOnes(t *testing.T) {
	t.Parallel()
	// Arrange: more high-risk entries (20 host commands) than the low cap,
	// plus 40 medium-risk ports.
	var b strings.Builder
	b.WriteString("schemaVersion: \"1\"\nagent: claude\nports:\n")
	for i := 1; i <= 40; i++ {
		b.WriteString("  - {sandbox: " + strconv.Itoa(i) + ", hostIP: 192.168.0.1}\n")
	}
	b.WriteString("lifecycle:\n  initialize:\n")
	for i := 0; i < 20; i++ {
		b.WriteString("    - command: step" + strconv.Itoa(i) + "\n")
	}
	home := t.TempDir()
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(home, "e.yaml"), Content: []byte(b.String()), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: home, Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer

	// Act
	_ = text.Write(&buf, p)

	// Assert
	risk := buf.String()[:strings.Index(buf.String(), "== Coverage")]
	for i := 0; i < 20; i++ {
		if id := "lifecycle.initialize[" + strconv.Itoa(i) + "]"; !strings.Contains(risk, id) {
			t.Errorf("high-risk %s must always be listed in Risk", id)
		}
	}
	if !strings.Contains(risk, "件を省略") {
		t.Error("medium entries beyond the cap should be summarised")
	}
}
