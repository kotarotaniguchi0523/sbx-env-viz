package tui

import (
	"strings"
	"testing"
)

func TestWideCharactersOccupyTwoColumnsAndKeepPanesAligned(t *testing.T) {
	t.Parallel()
	// Arrange: the Items view puts Japanese group headers in the left pane
	// and a Japanese explanation in the right pane.
	s := start(t, launch{cols: 130})
	s.press("3", "/", "R", "E", "L", "E", "A", "S", "E", "Enter")

	// Act
	// The right panel's left border follows the left panel's right border
	// ("││"); both must sit in the same column on every row.
	separators := s.locations("││")
	label := s.locations("要は: ")

	// Assert
	columns := map[uint32]bool{}
	for _, l := range separators {
		if l.Start.Column < 129 { // the last column is the right panel's own edge
			columns[l.Start.Column] = true
		}
	}
	if len(separators) < 30 || len(columns) != 1 {
		t.Errorf("separator on %d rows in columns %v; want one column on every row", len(separators), columns)
	}
	if len(label) != 1 {
		t.Fatalf("Japanese explanation not on screen:\n%s", s.text())
	}
	if span := label[0].Spans[0]; span.End-span.Start != 6 {
		t.Errorf("「要は: 」 spans %d cells, want 6 (two double-width glyphs, colon, space)", span.End-span.Start)
	}
}

func TestColoursByDefault(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130, color: true})
	s.press("2")
	s.snapshot("risk-130x40-color", true)

	// Assert
	n := 0
	for _, c := range s.styles("risk-130x40-color") {
		if coloured(c) {
			n++
		}
	}
	if n == 0 {
		t.Error("severity tags should be coloured")
	}
}

func TestNoColorLeavesDefaultColours(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130})
	s.press("2")
	s.snapshot("risk-130x40-nocolor", true)

	// Assert
	inverse := false
	for _, c := range s.styles("risk-130x40-nocolor") {
		if coloured(c) {
			t.Errorf("colour at %d,%d under NO_COLOR: fg=%s bg=%s", c.Col, c.Row, c.FG, c.BG)
		}
		if c.Inverse != nil && *c.Inverse {
			inverse = true
		}
	}
	if !inverse {
		t.Error("the selection should still be shown in reverse video")
	}
}

func TestControlCharactersCannotDriveTheTerminal(t *testing.T) {
	t.Parallel()
	// Arrange: values that try to clear the screen, retitle the window,
	// colour text, reorder it, and forge a row.
	evil := strings.Join([]string{
		`schemaVersion: "1"`,
		`agent: claude`,
		`env:`,
		`  EVIL: "ok\e[2J\e]0;pwned\a"`,
		`  RED: "x\e[31mred"`,
		`  BIDI: "a\u202Eb"`,
		`  MULTI: |`,
		`    line1`,
		`    forged  ← fake/origin.yaml:1`,
		``,
	}, "\n")
	s := start(t, launch{cols: 130, color: true, args: []string{"evil.yaml"}, files: map[string]string{"evil.yaml": evil}})

	// Act
	s.press("3")
	s.snapshot("evil-items-130x40-color", true)

	// Assert
	if title, _ := s.term.GetTitle(); title != nil && strings.Contains(*title, "pwned") {
		t.Errorf("the window title was changed to %q", *title)
	}
	s.expect(`env.EVIL = ok\x1b[2J\x1b]0;pwned\x07`)
	s.expect(`env.MULTI = line1\nforged`)
	s.expect("env.BIDI = a\\u202eb")
	for _, l := range s.lines() {
		if strings.HasPrefix(strings.TrimSpace(l), "forged") {
			t.Errorf("a value started its own row: %q", l)
		}
	}
	red := s.locations("env.RED")
	if len(red) != 1 {
		t.Fatalf("env.RED not on screen:\n%s", s.text())
	}
	// Only the list panel's interior counts: its border has its own colour.
	edge := uint32(130)
	for _, l := range s.locations("││") {
		if l.Start.Row == red[0].Start.Row && l.Start.Column < edge {
			edge = l.Start.Column
		}
	}
	for _, c := range s.styles("evil-items-130x40-color") {
		if uint32(c.Row) == red[0].Start.Row && uint32(c.Col) > red[0].Start.Column && uint32(c.Col) < edge && coloured(c) {
			t.Errorf("the value's colour sequence coloured the row from column %d (fg=%s)", c.Col, c.FG)
		}
	}
}
