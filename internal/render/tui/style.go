package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// palette holds every style the TUI draws with. With NO_COLOR every style
// keeps its structure (borders, bold, reverse video) and drops colour.
// Green is never used: this tool does not draw a "safe" state.
type palette struct {
	noColor bool

	brand, tab, tabActive, chip, chipActive, chipAlert  lipgloss.Style
	chipLabel                                           lipgloss.Style
	summaryLabel, summaryValue, summaryAlert, dim, bold lipgloss.Style
	heading, title, label, selected, groupHeader        lipgloss.Style
	keyCap, keyText                                     lipgloss.Style

	borderFocus, borderBlur color.Color
	sev                     map[sbxenv.Severity]lipgloss.Style
	badge                   map[string]lipgloss.Style
}

func newPalette(noColor bool) palette {
	base := lipgloss.NewStyle()
	p := palette{
		noColor:      noColor,
		brand:        base.Bold(true).Padding(0, 1),
		tab:          base.Padding(0, 1),
		tabActive:    base.Bold(true).Reverse(true).Padding(0, 1),
		chip:         base.Padding(0, 1),
		chipActive:   base.Bold(true).Reverse(true).Padding(0, 1),
		chipAlert:    base.Bold(true).Padding(0, 1),
		chipLabel:    base.Faint(true),
		summaryLabel: base.Faint(true),
		summaryValue: base.Bold(true),
		summaryAlert: base.Bold(true),
		dim:          base.Faint(true),
		bold:         base.Bold(true),
		heading:      base.Bold(true),
		title:        base.Bold(true),
		label:        base.Faint(true),
		selected:     base.Reverse(true),
		groupHeader:  base.Bold(true),
		keyCap:       base.Reverse(true).Padding(0, 1),
		keyText:      base.Faint(true),
		sev:          map[sbxenv.Severity]lipgloss.Style{},
		badge:        map[string]lipgloss.Style{},
	}
	for _, s := range []sbxenv.Severity{sbxenv.SeverityCritical, sbxenv.SeverityHigh, sbxenv.SeverityMedium, sbxenv.SeverityLow, sbxenv.SeverityInfo} {
		p.sev[s] = base.Bold(s.Rank() >= sbxenv.SeverityHigh.Rank())
	}
	if noColor {
		return p
	}

	c := lipgloss.Color
	var (
		accent  = c("#7aa2f7")
		purple  = c("#bb9af7")
		cyan    = c("#7dcfff")
		orange  = c("#ff9e64")
		yellow  = c("#e0af68")
		red     = c("#f7768e")
		ink     = c("#1a1b26")
		panel   = c("#24283b")
		text    = c("#c0caf5")
		muted   = c("#737aa2")
		selectB = c("#2e3c64")
	)
	p.brand = p.brand.Background(accent).Foreground(ink)
	p.tab = p.tab.Background(panel).Foreground(text)
	p.tabActive = base.Bold(true).Padding(0, 1).Background(purple).Foreground(ink)
	p.chip = p.chip.Background(panel).Foreground(text)
	p.chipActive = base.Bold(true).Padding(0, 1).Background(accent).Foreground(ink)
	p.chipAlert = base.Bold(true).Padding(0, 1).Background(orange).Foreground(ink)
	p.chipLabel = base.Foreground(muted)
	p.summaryLabel = base.Foreground(muted)
	p.summaryValue = base.Bold(true).Foreground(text)
	p.summaryAlert = base.Bold(true).Foreground(orange)
	p.dim = base.Foreground(muted)
	p.heading = base.Bold(true).Foreground(accent)
	p.title = base.Bold(true).Foreground(text)
	p.label = base.Foreground(muted)
	p.selected = base.Background(selectB).Foreground(c("#ffffff"))
	p.groupHeader = base.Bold(true).Foreground(purple)
	p.keyCap = base.Bold(true).Padding(0, 1).Background(panel).Foreground(accent)
	p.keyText = base.Foreground(muted)
	p.borderFocus, p.borderBlur = accent, c("#3b4261")

	p.sev[sbxenv.SeverityCritical] = base.Bold(true).Background(red).Foreground(ink)
	p.sev[sbxenv.SeverityHigh] = base.Bold(true).Background(orange).Foreground(ink)
	p.sev[sbxenv.SeverityMedium] = base.Background(yellow).Foreground(ink)
	p.sev[sbxenv.SeverityLow] = base.Background(accent).Foreground(ink)
	p.sev[sbxenv.SeverityInfo] = base.Foreground(muted)

	for name, col := range map[string]color.Color{
		"[HOST]": red, "[SSH]": red, "[LITERAL]": red, "[!]": red, "[ERROR]": red,
		"[RW]": orange, "[WARNING]": orange,
		"[CRED]": yellow,
		"[NET]":  cyan, "[RO]": cyan,
		"[PERSIST]": purple, "[OPAQUE]": purple, "[UNKNOWN]": purple, "[COVERAGE]": purple,
		"[SKIP]": muted, "[INFO]": muted,
	} {
		p.badge[name] = base.Bold(true).Foreground(col)
	}
	return p
}

// sevTag is the fixed-width severity pill text.
func sevTag(s sbxenv.Severity) string {
	switch s {
	case sbxenv.SeverityCritical:
		return " CRIT "
	case sbxenv.SeverityHigh:
		return " HIGH "
	case sbxenv.SeverityMedium:
		return " MED  "
	case sbxenv.SeverityLow:
		return " LOW  "
	}
	return " INFO "
}

// badges styles a run of ASCII badges such as "[HOST][RW]".
func (p palette) badges(list []string) string {
	var b strings.Builder
	for _, x := range list {
		if st, ok := p.badge[x]; ok {
			b.WriteString(st.Render(x))
		} else {
			b.WriteString(p.bold.Render(x))
		}
	}
	return b.String()
}
