// Package model derives renderer-neutral views (risk list, origin labels,
// groupings) from a Plan so that the TUI and text output agree.
package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// EntryKind tells what a RiskEntry points at.
type EntryKind string

const (
	KindDiagnostic EntryKind = "diagnostic"
	KindItem       EntryKind = "item"
	KindCoverage   EntryKind = "coverage"
)

// RiskEntry is one line of the Risk view.
type RiskEntry struct {
	Severity sbxenv.Severity
	Kind     EntryKind
	Label    string
	Badges   []string
	ItemID   string
	Diag     *sbxenv.Diagnostic
	Cover    *sbxenv.Coverage
	Item     *sbxenv.Item
}

// diagSeverity maps a diagnostic level onto the risk scale.
func diagSeverity(l sbxenv.DiagLevel) sbxenv.Severity {
	switch l {
	case sbxenv.LevelError:
		return sbxenv.SeverityCritical
	case sbxenv.LevelWarning:
		return sbxenv.SeverityHigh
	}
	return sbxenv.SeverityLow
}

// RiskList returns diagnostics, risky items, and coverage gaps ordered by
// severity. Items rated info are left out; nothing else is.
func RiskList(p *sbxenv.Plan) []RiskEntry {
	var out []RiskEntry
	for i := range p.Diagnostics {
		dg := &p.Diagnostics[i]
		out = append(out, RiskEntry{Severity: diagSeverity(dg.Level), Kind: KindDiagnostic,
			Label: fmt.Sprintf("%s %s", dg.Code, dg.Message), Badges: []string{"[" + strings.ToUpper(string(dg.Level)) + "]"}, ItemID: dg.ItemID, Diag: dg})
	}
	for i := range p.Items {
		it := &p.Items[i]
		if it.Risk == sbxenv.SeverityInfo {
			continue
		}
		out = append(out, RiskEntry{Severity: it.Risk, Kind: KindItem, Label: it.ID + " = " + it.Value, Badges: it.Badges, ItemID: it.ID, Item: it})
	}
	for i := range p.Coverage {
		c := &p.Coverage[i]
		out = append(out, RiskEntry{Severity: sbxenv.SeverityMedium, Kind: KindCoverage,
			Label: fmt.Sprintf("未展開 %s: %s", c.Kind, c.Ref), Badges: []string{"[COVERAGE]"}, Cover: c})
	}
	kindOrder := map[EntryKind]int{KindDiagnostic: 0, KindItem: 1, KindCoverage: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		return kindOrder[out[i].Kind] < kindOrder[out[j].Kind]
	})
	return out
}

// SourceIndex maps source IDs to sources.
func SourceIndex(p *sbxenv.Plan) map[string]sbxenv.Source {
	m := map[string]sbxenv.Source{}
	for _, s := range p.Meta.Sources {
		m[s.ID] = s
	}
	return m
}

// FormatOrigin renders an origin as path:line (via ...).
func FormatOrigin(idx map[string]sbxenv.Source, o sbxenv.Origin) string {
	label := o.SourceID
	if s, ok := idx[o.SourceID]; ok {
		label = s.Path
	}
	if o.SourceID == "host" {
		label = "host settings"
	}
	if o.Line > 0 {
		label = fmt.Sprintf("%s:%d", label, o.Line)
	}
	if len(o.Via) > 0 {
		label += " (via " + strings.Join(o.Via, " → ") + ")"
	}
	return label
}

// LastOrigin returns the origin that set the effective value.
func LastOrigin(it *sbxenv.Item) sbxenv.Origin {
	if len(it.Origins) == 0 {
		return sbxenv.Origin{}
	}
	return it.Origins[len(it.Origins)-1]
}

// BoundariesFor returns the boundaries attached to an item.
func BoundariesFor(p *sbxenv.Plan, itemID string) []sbxenv.Boundary {
	var out []sbxenv.Boundary
	for _, b := range p.Boundaries {
		if b.ItemID == itemID {
			out = append(out, b)
		}
	}
	return out
}

// DiagnosticsFor returns diagnostics attached to an item.
func DiagnosticsFor(p *sbxenv.Plan, itemID string) []sbxenv.Diagnostic {
	var out []sbxenv.Diagnostic
	for _, d := range p.Diagnostics {
		if d.ItemID == itemID {
			out = append(out, d)
		}
	}
	return out
}

// TouchesSource reports whether any history step of the item came from src.
func TouchesSource(it *sbxenv.Item, src string) bool {
	for _, h := range it.History {
		if h.Origin.SourceID == src {
			return true
		}
	}
	for _, o := range it.Origins {
		if o.SourceID == src {
			return true
		}
	}
	return false
}

// DiagTouchesSource reports whether a diagnostic points into src.
func DiagTouchesSource(d *sbxenv.Diagnostic, src string) bool {
	for _, o := range d.Origins {
		if o.SourceID == src {
			return true
		}
	}
	return false
}

// SeverityTag is the fixed-width ASCII tag of a severity.
func SeverityTag(s sbxenv.Severity) string {
	switch s {
	case sbxenv.SeverityCritical:
		return "CRIT"
	case sbxenv.SeverityHigh:
		return "HIGH"
	case sbxenv.SeverityMedium:
		return "MED "
	case sbxenv.SeverityLow:
		return "LOW "
	}
	return "INFO"
}

// ArgsLine summarises the applied env-args.
func ArgsLine(p *sbxenv.Plan) string {
	if len(p.Invocation.EnvArgs) == 0 {
		return "env-args: (なし)"
	}
	var parts []string
	for _, a := range p.Invocation.EnvArgs {
		v := a.Value
		if a.From == "unset" {
			v = "<unset>"
		}
		parts = append(parts, fmt.Sprintf("%s=%s (%s)", a.Name, v, a.From))
	}
	return "env-args: " + strings.Join(parts, ", ")
}
