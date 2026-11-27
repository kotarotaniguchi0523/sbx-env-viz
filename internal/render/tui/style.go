package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// palette is the ink the TUI draws with: the terminal's own paper and
// foreground, one muted ink for labels and context, an official blue for
// focus and keys, and stamp red (plus amber) for findings. Only the 16 ANSI
// colours are used, so the user's theme decides the exact shades on light
// and dark backgrounds alike, and nothing paints a background except the
// reverse-video selection. Under NO_COLOR every style keeps its bold,
// underline and reverse attributes and drops colour; marks are glyphs, so
// meaning never rests on colour. Green is never used: this tool does not
// draw a "safe" state.
//
//declscope:shared // the package's styles: Model holds one, and view.go, overview.go and detail.go draw with its fields
type palette struct {
	// ink is body text; muted is labels, context and anything secondary.
	ink, muted lipgloss.Style
	// strong is a group or section heading; title is a detail title.
	strong, title lipgloss.Style
	// accent marks focus and what can be acted on: the active tab, keys,
	// links and detail headings.
	accent, tabActive, key, heading lipgloss.Style
	// colHead is a table's column-header row.
	colHead lipgloss.Style
	// selected is the cursor row of the focused pane; cursor marks the
	// row of a pane that has lost focus.
	selected, cursor lipgloss.Style
	// stamp is the high-risk mark; caution is the medium one.
	stamp, caution lipgloss.Style
	// drift is an actual value that differs from its declaration: bold and
	// underlined, so it reads without colour too.
	drift lipgloss.Style
	// rule is the column between two panes.
	rule lipgloss.Style
}

// newPalette builds the styles, without colour when noColor is set.
//
//declscope:shared // tui.go's New and NewTargets build the model's palette with it
func newPalette(noColor bool) palette {
	base := lipgloss.NewStyle()
	p := palette{
		ink:       base,
		muted:     base.Faint(true),
		strong:    base.Bold(true),
		title:     base.Bold(true),
		accent:    base.Bold(true),
		tabActive: base.Bold(true).Underline(true),
		key:       base.Bold(true),
		heading:   base.Bold(true).Underline(true),
		colHead:   base.Underline(true),
		selected:  base.Reverse(true),
		cursor:    base.Bold(true),
		stamp:     base.Bold(true),
		drift:     base.Bold(true).Underline(true),
		caution:   base.Bold(true),
		rule:      base.Faint(true),
	}
	if noColor {
		return p
	}
	muted := base.Foreground(lipgloss.BrightBlack)
	p.muted = muted
	p.accent = base.Bold(true).Foreground(lipgloss.Blue)
	p.tabActive = base.Bold(true).Underline(true).Foreground(lipgloss.Blue)
	p.key = base.Bold(true).Foreground(lipgloss.Blue)
	p.heading = base.Bold(true).Foreground(lipgloss.Blue)
	p.colHead = muted.Underline(true)
	p.cursor = base.Bold(true).Foreground(lipgloss.Blue)
	p.stamp = base.Bold(true).Foreground(lipgloss.Red)
	p.caution = base.Bold(true).Foreground(lipgloss.Yellow)
	p.drift = base.Bold(true).Underline(true).Foreground(lipgloss.Red)
	p.rule = muted
	return p
}

// markText is the one-cell margin mark of a severity: ✗ for critical and
// high, ! for medium, a space for the rest. The glyphs carry the meaning,
// so it survives NO_COLOR.
//
//declscope:shared // view.go and overview.go write it unstyled into the reverse-video cursor row
func markText(s sbxenv.Severity) string {
	switch {
	case s.Rank() >= sbxenv.SeverityHigh.Rank():
		return "✗"
	case s == sbxenv.SeverityMedium:
		return "!"
	}
	return " "
}

// mark styles markText.
//
//declscope:shared // view.go's list rows and overview.go's manifest rows start with it
func (p palette) mark(s sbxenv.Severity) string {
	t := markText(s)
	switch t {
	case "✗":
		return p.stamp.Render(t)
	case "!":
		return p.caution.Render(t)
	}
	return t
}
