package sbxenv_test

// Bounded exhaustive verification of the sbx env merge rules.
//
// Each test enumerates every layer stack up to a bound, runs the real
// Resolve on it, and compares the result with a reference model written
// from the documented rules (docs.docker.com "Merge rules" and the
// `sbx env create --help` text in .github/sbx-snapshots/env-create.txt).
// The reference models share no code with the implementation.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// portVariant is one possible ports entry. host is unique per entry so the
// effective value shows which entry won.
type portVariant struct {
	sandbox  int
	protocol string // "" means omitted
}

var portVariants = []portVariant{{1, ""}, {1, "TCP4"}, {1, "udp"}, {2, ""}, {2, "tcp6"}}

type portEntry struct {
	portVariant
	host int
}

// referencePorts applies the documented rule: entries with the same sandbox
// port and effective protocol (case-insensitive, omitted = tcp4) are
// replaced by the last one, which keeps the first one's position; all other
// entries append in order. This holds within one file as well.
func referencePorts(layers [][]portEntry) []string {
	type slot struct {
		key   string
		entry portEntry
	}
	var out []slot
	for _, layer := range layers {
		for _, e := range layer {
			proto := strings.ToLower(e.protocol)
			if proto == "" {
				proto = "tcp4"
			}
			key := fmt.Sprintf("%d/%s", e.sandbox, proto)
			replaced := false
			for i := range out {
				if out[i].key == key {
					out[i].entry = e
					replaced = true
					break
				}
			}
			if !replaced {
				out = append(out, slot{key, e})
			}
		}
	}
	var got []string
	for _, s := range out {
		got = append(got, fmt.Sprintf("ports[%s]=loopback:%d -> sandbox:%s", s.key, s.entry.host, s.key))
	}
	return got
}

func portsYAML(entries []portEntry, first bool) string {
	var b strings.Builder
	if first {
		b.WriteString("schemaVersion: \"1\"\nagent: claude\n")
	}
	if len(entries) == 0 {
		return b.String()
	}
	b.WriteString("ports:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "  - sandbox: %d\n    host: %d\n", e.sandbox, e.host)
		if e.protocol != "" {
			fmt.Fprintf(&b, "    protocol: %s\n", e.protocol)
		}
	}
	return b.String()
}

// layerChoices enumerates every list of 0..maxLen variants.
func layerChoices(maxLen int) [][]portVariant {
	choices := [][]portVariant{{}}
	frontier := [][]portVariant{{}}
	for n := 1; n <= maxLen; n++ {
		var next [][]portVariant
		for _, prefix := range frontier {
			for _, v := range portVariants {
				next = append(next, append(append([]portVariant(nil), prefix...), v))
			}
		}
		choices = append(choices, next...)
		frontier = next
	}
	return choices
}

func TestPortsMergeMatchesTheReferenceModelExhaustively(t *testing.T) {
	t.Parallel()
	const maxLayers, maxEntries = 3, 2
	choices := layerChoices(maxEntries)
	if testing.Short() {
		choices = choices[:len(portVariants)+1]
	}
	checked := 0
	var walk func(stack [][]portVariant)
	walk = func(stack [][]portVariant) {
		if len(stack) > 0 {
			// Arrange
			var layers []sbxenv.LayerInput
			var model [][]portEntry
			host := 1000
			for i, choice := range stack {
				var entries []portEntry
				for _, v := range choice {
					host++
					entries = append(entries, portEntry{v, host})
				}
				model = append(model, entries)
				layers = append(layers, sbxenv.LayerInput{Path: fmt.Sprintf("/w/l%d.yaml", i), Content: []byte(portsYAML(entries, i == 0)), Kind: sbxenv.LayerEnv})
			}

			// Act
			p, err := sbxenv.Resolve(layers, sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome})

			// Assert
			if err != nil {
				t.Fatalf("stack %v: %v", stack, err)
			}
			var got []string
			for _, it := range p.Items {
				if it.Section == sbxenv.SectionPorts {
					got = append(got, it.ID+"="+it.Value)
				}
			}
			if want := referencePorts(model); !reflect.DeepEqual(got, want) {
				t.Fatalf("stack %v\n got  %v\n want %v", stack, got, want)
			}
			checked++
		}
		if len(stack) == maxLayers {
			return
		}
		for _, c := range choices {
			walk(append(stack, c))
		}
	}
	walk(nil)
	t.Logf("checked %d layer stacks", checked)
}

// launchChoice is how one layer writes cmd.
var launchChoices = []struct {
	yaml string
	set  bool   // whether the layer sets a value (null and omitted inherit)
	want string // rendered value when set
}{
	{"", false, ""},
	{"cmd: null\n", false, ""},
	{"cmd: []\n", true, "[]"},
	{"cmd: [a]\n", true, "[a]"},
	{"cmd: [b, c]\n", true, "[b, c]"},
}

func TestLaunchListsMatchTheReferenceModelExhaustively(t *testing.T) {
	t.Parallel()
	const maxLayers = 4
	checked := 0
	var walk func(stack []int)
	walk = func(stack []int) {
		if len(stack) > 0 {
			// Arrange
			var layers []sbxenv.LayerInput
			want, present := "", false
			for i, c := range stack {
				content := launchChoices[c].yaml
				if i == 0 {
					content = "schemaVersion: \"1\"\nagent: claude\n" + content
				}
				layers = append(layers, sbxenv.LayerInput{Path: fmt.Sprintf("/w/l%d.yaml", i), Content: []byte(content), Kind: sbxenv.LayerEnv})
				// Reference: the last layer that sets a list wins whole;
				// null and omission inherit; [] clears.
				if launchChoices[c].set {
					want, present = launchChoices[c].want, true
				}
			}

			// Act
			p, err := sbxenv.Resolve(layers, sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome})

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			got, ok := "", false
			for _, it := range p.Items {
				if it.ID == "cmd" {
					got, ok = it.Value, true
				}
			}
			if present && (!ok || got != want) {
				t.Fatalf("stack %v: cmd = %q (present %v), want %q", stack, got, ok, want)
			}
			if !present && ok && got != "null" {
				t.Fatalf("stack %v: cmd = %q, want no list", stack, got)
			}
			checked++
		}
		if len(stack) == maxLayers {
			return
		}
		for c := range launchChoices {
			walk(append(stack, c))
		}
	}
	walk(nil)
	t.Logf("checked %d layer stacks", checked)
}

func TestScalarAndMapMergeMatchTheReferenceModelExhaustively(t *testing.T) {
	t.Parallel()
	// Each layer may set env.A and/or env.B to its own value, or leave them.
	const maxLayers = 4
	options := []struct{ a, b bool }{{false, false}, {true, false}, {false, true}, {true, true}}
	checked := 0
	var walk func(stack []int)
	walk = func(stack []int) {
		if len(stack) > 0 {
			// Arrange
			var layers []sbxenv.LayerInput
			want := map[string]string{}
			for i, o := range stack {
				var b strings.Builder
				if i == 0 {
					b.WriteString("schemaVersion: \"1\"\nagent: claude\n")
				}
				if options[o].a || options[o].b {
					b.WriteString("env:\n")
				}
				if options[o].a {
					fmt.Fprintf(&b, "  A: a%d\n", i)
					want["env.A"] = fmt.Sprintf("a%d", i)
				}
				if options[o].b {
					fmt.Fprintf(&b, "  B: b%d\n", i)
					want["env.B"] = fmt.Sprintf("b%d", i)
				}
				layers = append(layers, sbxenv.LayerInput{Path: fmt.Sprintf("/w/l%d.yaml", i), Content: []byte(b.String()), Kind: sbxenv.LayerEnv})
			}

			// Act
			p, err := sbxenv.Resolve(layers, sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome})

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, it := range p.Items {
				if it.Section == sbxenv.SectionEnv {
					got[it.ID] = it.Value
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stack %v: got %v want %v", stack, got, want)
			}
			checked++
		}
		if len(stack) == maxLayers {
			return
		}
		for o := range options {
			walk(append(stack, o))
		}
	}
	walk(nil)
	t.Logf("checked %d layer stacks", checked)
}
