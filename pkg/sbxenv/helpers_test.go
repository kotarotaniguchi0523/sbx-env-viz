package sbxenv

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testHome = "/home/tester"
const testProj = "/home/tester/proj"

type memKits map[string]string

func (m memKits) LoadKit(dir string) (KitInput, error) {
	c, ok := m[dir]
	if !ok {
		return KitInput{}, errors.New("not found")
	}
	return KitInput{DescriptorPath: dir + "/kit.yaml", Content: []byte(c)}, nil
}

func layer(path, content string) LayerInput {
	return LayerInput{Path: path, Content: []byte(content), Kind: LayerEnv}
}

func resolve(t *testing.T, opts ResolveOptions, layers ...LayerInput) *Plan {
	t.Helper()
	if opts.ProjectDir == "" {
		opts.ProjectDir = testProj
	}
	if opts.HomeDir == "" {
		opts.HomeDir = testHome
	}
	p, err := Resolve(layers, opts)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return p
}

func item(t *testing.T, p *Plan, id string) Item {
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
	return Item{}
}

func hasItem(p *Plan, id string) bool {
	for _, it := range p.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func diags(p *Plan, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range p.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func requireDiag(t *testing.T, p *Plan, code string, level DiagLevel) Diagnostic {
	t.Helper()
	ds := diags(p, code)
	for _, d := range ds {
		if d.Level == level {
			return d
		}
	}
	var got []string
	for _, d := range p.Diagnostics {
		got = append(got, string(d.Level)+":"+d.Code)
	}
	t.Fatalf("diagnostic %s (%s) not found; have %v", code, level, got)
	return Diagnostic{}
}

func noDiag(t *testing.T, p *Plan, code string) {
	t.Helper()
	if ds := diags(p, code); len(ds) > 0 {
		t.Fatalf("unexpected %s: %s", code, ds[0].Message)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
