package main_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

func BenchmarkLayeredKitJSON(b *testing.B) {
	dir := b.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "testdata", "layered"))); err != nil {
		b.Fatal(err)
	}
	req := loader.Request{WorkDir: dir, HomeDir: dir, Paths: []string{"base.sbxenv.yaml", "local.sbxenv.yaml"}, EnvArgsFiles: []string{"prod.args"}}
	b.ReportAllocs()
	for b.Loop() {
		inputs, err := loader.Load(req)
		if err != nil {
			b.Fatal(err)
		}
		plan, err := sbxenv.Resolve(inputs.Layers, sbxenv.ResolveOptions{HomeDir: dir, ProjectDir: inputs.ProjectDir, EnvArgsFiles: inputs.EnvArgsFiles, Kits: loader.FSKits{}, Redaction: sbxenv.RedactStrict})
		if err != nil {
			b.Fatal(err)
		}
		if err := jsonout.Write(io.Discard, plan); err != nil {
			b.Fatal(err)
		}
	}
}
