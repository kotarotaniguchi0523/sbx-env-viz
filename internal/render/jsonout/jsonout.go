// Package jsonout writes the stable schemaVersion "1" JSON IR.
//
// Ordering is deterministic: sources by ID, items by (section, ID),
// boundaries by (item, kind), diagnostics by (level, code, origin), and
// coverage by (kind, ref). The Plan is already redacted by sbxenv.Resolve;
// this package adds nothing that Resolve did not copy.
package jsonout

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// Write encodes p as indented JSON followed by a newline.
func Write(w io.Writer, p *sbxenv.Plan) error {
	cp := *p
	cp.Meta.Sources = append([]sbxenv.Source(nil), p.Meta.Sources...)
	sort.SliceStable(cp.Meta.Sources, func(i, j int) bool { return sourceKey(cp.Meta.Sources[i]) < sourceKey(cp.Meta.Sources[j]) })
	cp.Items = append([]sbxenv.Item(nil), p.Items...)
	order := sectionOrder()
	sort.SliceStable(cp.Items, func(i, j int) bool {
		a, b := cp.Items[i], cp.Items[j]
		if order[a.Section] != order[b.Section] {
			return order[a.Section] < order[b.Section]
		}
		return a.ID < b.ID
	})
	cp.Boundaries = append([]sbxenv.Boundary(nil), p.Boundaries...)
	sort.SliceStable(cp.Boundaries, func(i, j int) bool {
		a, b := cp.Boundaries[i], cp.Boundaries[j]
		if a.ItemID != b.ItemID {
			return a.ItemID < b.ItemID
		}
		return a.Kind < b.Kind
	})
	if cp.Items == nil {
		cp.Items = []sbxenv.Item{}
	}
	if cp.Boundaries == nil {
		cp.Boundaries = []sbxenv.Boundary{}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(&cp)
}

func sourceKey(s sbxenv.Source) string {
	rank := map[sbxenv.SourceKind]string{
		sbxenv.SourceUserDefaults: "0", sbxenv.SourceEnv: "0", sbxenv.SourceEnvArgsFile: "1",
		sbxenv.SourceKitArgsFile: "2", sbxenv.SourceKit: "3", sbxenv.SourceCLI: "4", sbxenv.SourceDefault: "5",
	}[s.Kind]
	// Zero-pad numeric suffixes so L10 sorts after L9.
	id := s.ID
	num := ""
	for i := len(id) - 1; i >= 0 && id[i] >= '0' && id[i] <= '9'; i-- {
		num = string(id[i]) + num
	}
	prefix := id[:len(id)-len(num)]
	for len(num) < 6 {
		num = "0" + num
	}
	return rank + prefix + num
}

func sectionOrder() map[sbxenv.Section]int {
	m := map[sbxenv.Section]int{}
	for i, s := range []sbxenv.Section{sbxenv.SectionTop, sbxenv.SectionArgs, sbxenv.SectionWorkspace, sbxenv.SectionMounts,
		sbxenv.SectionEnv, sbxenv.SectionSandbox, sbxenv.SectionSecrets, sbxenv.SectionBindings, sbxenv.SectionRegistries,
		sbxenv.SectionMCP, sbxenv.SectionPorts, sbxenv.SectionLifecycle, sbxenv.SectionKits, sbxenv.SectionCapabilities} {
		m[s] = i
	}
	return m
}
