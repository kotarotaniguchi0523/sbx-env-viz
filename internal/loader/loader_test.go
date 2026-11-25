package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
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

func TestWithoutPathsUserDefaultsComeFirst(t *testing.T) {
	t.Parallel()
	// Arrange
	home, proj := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".sbxenv.yaml"), "a: 1\n")
	write(t, filepath.Join(proj, "sbxenv.yaml"), "b: 2\n")

	// Act
	res, err := loader.Load(loader.Request{WorkDir: proj, HomeDir: home})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 2 || res.Layers[0].Kind != sbxenv.LayerUserDefaults || string(res.Layers[1].Content) != "b: 2\n" {
		t.Fatalf("layers = %+v", res.Layers)
	}
	if res.ProjectDir != proj {
		t.Errorf("project dir = %q, want the working directory", res.ProjectDir)
	}
}

func TestWithoutPathsEitherFileAloneIsEnough(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{"home only": true, "project only": false}
	for name, inHome := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			home, proj := t.TempDir(), t.TempDir()
			if inHome {
				write(t, filepath.Join(home, ".sbxenv.yaml"), "a: 1\n")
			} else {
				write(t, filepath.Join(proj, "sbxenv.yaml"), "a: 1\n")
			}

			// Act
			res, err := loader.Load(loader.Request{WorkDir: proj, HomeDir: home})

			// Assert
			if err != nil || len(res.Layers) != 1 {
				t.Errorf("err %v, layers %d", err, len(res.Layers))
			}
		})
	}
}

func TestExplicitPathsSkipDefaultsAndResolveDirectories(t *testing.T) {
	t.Parallel()
	// Arrange
	home, proj := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".sbxenv.yaml"), "a: 1\n")
	write(t, filepath.Join(proj, "env", "sbxenv.yaml"), "b: 2\n")
	write(t, filepath.Join(proj, "local.yaml"), "c: 3\n")
	write(t, filepath.Join(proj, "a.args"), "x=1\n")
	write(t, filepath.Join(proj, "k.args"), "y=2\n")

	// Act
	res, err := loader.Load(loader.Request{Paths: []string{"env", "local.yaml"}, EnvArgsFiles: []string{"a.args"}, KitArgsFiles: []string{"k.args"}, WorkDir: proj, HomeDir: home})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 2 || res.Layers[0].Path != filepath.Join(proj, "env", "sbxenv.yaml") {
		t.Fatalf("layers = %+v", res.Layers)
	}
	if res.ProjectDir != filepath.Join(proj, "env") {
		t.Errorf("project dir = %q, want the first path's directory", res.ProjectDir)
	}
	if string(res.EnvArgsFiles[0].Content) != "x=1\n" || string(res.KitArgsFiles[0].Content) != "y=2\n" {
		t.Error("args files not read")
	}
}

func TestLoadFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]loader.Request{
		"nothing to load":   {},
		"missing path":      {Paths: []string{"nope.yaml"}},
		"directory without": {Paths: []string{"emptydir"}},
		"missing args file": {Paths: []string{"sbxenv.yaml"}, EnvArgsFiles: []string{"nope.args"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "emptydir"), 0o755); err != nil {
				t.Fatal(err)
			}
			if name == "missing args file" {
				write(t, filepath.Join(dir, "sbxenv.yaml"), "a: 1\n")
			}
			req.WorkDir, req.HomeDir = dir, dir

			// Act
			_, err := loader.Load(req)

			// Assert
			if err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestKitDescriptorDiscovery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"companion pair by directory name", map[string]string{"tool.yaml": "schemaVersion: \"3\"\n", "other.yaml": "x: 1\n"}, "tool.yaml"},
		{".yml companion", map[string]string{"tool.yml": "schemaVersion: \"3\"\n"}, "tool.yml"},
		{"any file with the syntax header", map[string]string{"notes.yaml": "x: 1\n", "kit.yaml": "# syntax=docker/sandbox-kit:3\n"}, "kit.yaml"},
		{"legacy spec.yaml", map[string]string{"spec.yaml": "schemaVersion: \"2\"\n"}, "spec.yaml"},
		{"nothing recognisable", map[string]string{"notes.yaml": "x: 1\n"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			dir := filepath.Join(t.TempDir(), "tool")
			for name, c := range tc.files {
				write(t, filepath.Join(dir, name), c)
			}

			// Act
			in, err := loader.FSKits{}.LoadKit(dir)

			// Assert
			if tc.want == "" {
				if err == nil {
					t.Errorf("expected an error, got %q", in.DescriptorPath)
				}
				return
			}
			if err != nil || filepath.Base(in.DescriptorPath) != tc.want {
				t.Errorf("descriptor = %q, err %v; want %s", in.DescriptorPath, err, tc.want)
			}
		})
	}
}

func TestKitDirectoryMustExistAndBeADirectory(t *testing.T) {
	t.Parallel()
	// Arrange
	dir := t.TempDir()
	write(t, filepath.Join(dir, "file"), "x")

	// Act
	_, missing := loader.FSKits{}.LoadKit(filepath.Join(dir, "none"))
	_, notDir := loader.FSKits{}.LoadKit(filepath.Join(dir, "file"))

	// Assert
	if missing == nil || notDir == nil {
		t.Errorf("missing = %v, notDir = %v", missing, notDir)
	}
}
