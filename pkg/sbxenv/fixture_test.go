//declscope:shared // the fixture and assertion helpers that the other test files of this package build on
package sbxenv_test

// Tests in this package follow the classical (Detroit) school: they drive
// the public API with real files on disk, the production loader, and the
// production kit resolver. Nothing is mocked; each test is Arrange / Act /
// Assert.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// fixture is a throwaway home directory with a project inside it, so that
// $HOME shortening can be observed as "~/proj/...".
type fixture struct {
	home string
	proj string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture{home: home, proj: proj}
}

// write creates a file relative to the project directory.
func (f fixture) write(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(f.proj, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// run loads the given paths with the production loader and resolves them
// with the production kit resolver, exactly as the CLI does.
type run struct {
	paths        []string
	envArgsFiles []string
	kitArgsFiles []string
	opts         sbxenv.ResolveOptions
}

func (f fixture) resolveErr(t *testing.T, r run) (*sbxenv.Plan, error) {
	t.Helper()
	res, err := loader.Load(loader.Request{
		Paths: r.paths, EnvArgsFiles: r.envArgsFiles, KitArgsFiles: r.kitArgsFiles,
		WorkDir: f.proj, HomeDir: f.home,
	})
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}
	o := r.opts
	o.ProjectDir = res.ProjectDir
	o.HomeDir = f.home
	o.EnvArgsFiles = res.EnvArgsFiles
	o.KitArgsFiles = res.KitArgsFiles
	o.Kits = loader.FSKits{}
	return sbxenv.Resolve(res.Layers, o)
}

func (f fixture) resolve(t *testing.T, r run) *sbxenv.Plan {
	t.Helper()
	p, err := f.resolveErr(t, r)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return p
}

func item(t *testing.T, p *sbxenv.Plan, id string) sbxenv.Item {
	t.Helper()
	for _, it := range p.Items {
		if it.ID == id {
			return it
		}
	}
	var ids []string
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	t.Fatalf("item %q not found; have %s", id, strings.Join(ids, ", "))
	return sbxenv.Item{}
}

func hasItem(p *sbxenv.Plan, id string) bool {
	for _, it := range p.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func field(it sbxenv.Item, key string) (string, bool) {
	for _, f := range it.Fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}

func diags(p *sbxenv.Plan, code string) []sbxenv.Diagnostic {
	var out []sbxenv.Diagnostic
	for _, d := range p.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func requireDiag(t *testing.T, p *sbxenv.Plan, code string, level sbxenv.DiagLevel) sbxenv.Diagnostic {
	t.Helper()
	for _, d := range diags(p, code) {
		if d.Level == level {
			return d
		}
	}
	var got []string
	for _, d := range p.Diagnostics {
		got = append(got, string(d.Level)+":"+d.Code)
	}
	t.Fatalf("diagnostic %s (%s) not found; have %v", code, level, got)
	return sbxenv.Diagnostic{}
}

func noDiag(t *testing.T, p *sbxenv.Plan, codes ...string) {
	t.Helper()
	for _, code := range codes {
		if ds := diags(p, code); len(ds) > 0 {
			t.Errorf("unexpected %s: %s", code, ds[0].Message)
		}
	}
}

func coverage(p *sbxenv.Plan, kind sbxenv.CoverageKind) []sbxenv.Coverage {
	var out []sbxenv.Coverage
	for _, c := range p.Coverage {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// containsJapanese reports whether s holds kana or kanji, so a test can
// check that a message was written in Japanese.
func containsJapanese(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30ff || r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

const minimal = "schemaVersion: \"1\"\nagent: claude\n"
