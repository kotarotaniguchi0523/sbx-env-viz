package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaultsWithoutPaths(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".sbxenv.yaml"), "schemaVersion: \"1\"\n")
	write(t, filepath.Join(proj, "sbxenv.yaml"), "agent: claude\n")
	res, err := Load(Request{WorkDir: proj, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 2 || res.Layers[0].Kind != sbxenv.LayerUserDefaults || res.Layers[1].Kind != sbxenv.LayerEnv {
		t.Fatalf("layers = %+v", res.Layers)
	}
	if res.ProjectDir != proj {
		t.Errorf("project dir = %q", res.ProjectDir)
	}
}

func TestLoadPathsSkipUserDefaults(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".sbxenv.yaml"), "schemaVersion: \"1\"\n")
	write(t, filepath.Join(proj, "env", "sbxenv.yaml"), "agent: claude\n")
	write(t, filepath.Join(proj, "local.yaml"), "agent: codex\n")
	write(t, filepath.Join(proj, "a.args"), "x=1\n")
	res, err := Load(Request{Paths: []string{"env", "local.yaml"}, EnvArgsFiles: []string{"a.args"}, WorkDir: proj, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 2 || filepath.Base(res.Layers[0].Path) != "sbxenv.yaml" {
		t.Fatalf("layers = %+v", res.Layers)
	}
	if res.ProjectDir != filepath.Join(proj, "env") {
		t.Errorf("first path sets the project dir: %q", res.ProjectDir)
	}
	if len(res.EnvArgsFiles) != 1 || string(res.EnvArgsFiles[0].Content) != "x=1\n" {
		t.Errorf("args files = %+v", res.EnvArgsFiles)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(Request{WorkDir: dir, HomeDir: dir}); err == nil {
		t.Error("no file anywhere should fail")
	}
	if _, err := Load(Request{Paths: []string{"missing.yaml"}, WorkDir: dir}); err == nil {
		t.Error("missing path should fail")
	}
}

func TestFSKits(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "a.yaml"), "schemaVersion: \"3\"\n")
	write(t, filepath.Join(dir, "b", "notes.yaml"), "x: 1\n")
	write(t, filepath.Join(dir, "b", "kit.yaml"), "# syntax=docker/sandbox-kit:3\nschemaVersion: \"3\"\n")
	write(t, filepath.Join(dir, "c", "spec.yaml"), "schemaVersion: \"2\"\n")
	for name, want := range map[string]string{"a": "a.yaml", "b": "kit.yaml", "c": "spec.yaml"} {
		in, err := FSKits{}.LoadKit(filepath.Join(dir, name))
		if err != nil || filepath.Base(in.DescriptorPath) != want {
			t.Errorf("%s: %v %q", name, err, in.DescriptorPath)
		}
	}
	if _, err := (FSKits{}).LoadKit(filepath.Join(dir, "none")); err == nil {
		t.Error("missing kit dir should fail")
	}
}
