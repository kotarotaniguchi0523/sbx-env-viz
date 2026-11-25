package render_test

// The plan fixture shared by the renderer tests: a two-layer environment
// with a local kit, written into a fresh temporary home.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
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
//
//declscope:shared // render_test.go and tui_test.go build their plans with it
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
