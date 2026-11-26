package tui

import (
	"strconv"
	"strings"
	"testing"
)

func TestWideCharactersOccupyTwoColumnsAndKeepPanesAligned(t *testing.T) {
	t.Parallel()
	// Arrange: the Items view puts Japanese group headers in the left pane
	// and a Japanese explanation in the right pane.
	s := start(t, launch{cols: 130})
	s.press("3", "/", "R", "E", "L", "E", "A", "S", "E", "Enter")

	// Act: the single rule between the panes must sit in the same column
	// on every body row, whatever mix of wide and narrow glyphs is left of it.
	rows, columns := s.separators()
	label := s.locations("要は: ")

	// Assert
	if body := len(s.lines()) - 5; rows != body || len(columns) != 1 || !columns[130*45/100+1] {
		t.Errorf("separator on %d of %d rows in columns %v; want column %d on every row\n%s", rows, body, columns, 130*45/100+1, s.text())
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
	s.expectMatch(`危険度\s+内容`)
	s.snapshot("risk-130x40-color", true)

	// Assert: only the 16 ANSI colours, so the terminal theme picks the
	// shades, and no background anywhere.
	at := map[[2]int]styleChange{}
	for _, c := range s.styles("risk-130x40-color") {
		if c.BG != "" && c.BG != "default" {
			t.Errorf("background %s painted at %d,%d", c.BG, c.Col, c.Row)
		}
		if n, err := strconv.Atoi(c.FG); c.FG != "" && c.FG != "default" && (err != nil || n < 0 || n > 15) {
			t.Errorf("colour %s at %d,%d is not one of the 16 ANSI colours", c.FG, c.Col, c.Row)
		}
		at[[2]int{c.Col, c.Row}] = c
	}
	// The margin marks carry the severity colour: ✗ red, ! yellow. The
	// cursor row is reverse video and keeps its mark unstyled.
	for mark, want := range map[string]string{"✗": "1", "!": "3"} {
		n := 0
		for _, l := range s.locations(mark) {
			row := int(l.Start.Row)
			if l.Start.Column != 1 || at[[2]int{0, row}].Inverse != nil {
				continue
			}
			n++
			if got := at[[2]int{1, row}].FG; got != want {
				t.Errorf("mark %s on row %d has colour %q, want %s", mark, row, got, want)
			}
		}
		if n == 0 {
			t.Errorf("no %s mark in the list margin:\n%s", mark, s.text())
		}
	}
	// The active tab and the keys of the status bar are blue.
	for _, text := range []string{"2 リスク", "q 終了"} {
		loc := s.locations(text)
		if len(loc) != 1 {
			t.Fatalf("%q on screen %d times", text, len(loc))
		}
		if got := at[[2]int{int(loc[0].Start.Column), int(loc[0].Start.Row)}].FG; got != "4" {
			t.Errorf("%q starts with colour %q, want blue (4)", text, got)
		}
	}
}

func TestNoColorLeavesDefaultColours(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130})
	s.press("2")
	s.expectMatch(`危険度\s+内容`)
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
	s.expectMatch(`env\.EVIL\s+ok\\x1b\[2J\\x1b\]0;pwned\\x07`)
	s.expectMatch(`env\.MULTI\s+line1\\nforged`)
	s.expectMatch(`env\.BIDI\s+a\\u202eb`)
	for _, l := range s.lines() {
		if strings.HasPrefix(strings.TrimSpace(l), "forged") {
			t.Errorf("a value started its own row: %q", l)
		}
	}
	red := s.locations("env.RED")
	if len(red) != 1 {
		t.Fatalf("env.RED not on screen:\n%s", s.text())
	}
	// The list draws the whole value in its muted ink (ANSI 8): the colour
	// may change only where the escaped value starts and ends, not at the
	// "\x1b[31m" inside it. Only the list pane counts, up to the rule.
	value := s.locations(`x\x1b[31mred`)
	if len(value) != 1 || value[0].Start.Row != red[0].Start.Row {
		t.Fatalf("env.RED value not on its row:\n%s", s.text())
	}
	edge := uint32(0)
	for _, l := range s.locations(" | ") {
		if l.Start.Row == red[0].Start.Row {
			edge = l.Start.Column + 1
		}
	}
	if edge == 0 {
		t.Fatalf("no pane separator on the env.RED row:\n%s", s.text())
	}
	start, end := int(value[0].Start.Column), int(value[0].Start.Column)+len(`x\x1b[31mred`)
	for _, c := range s.styles("evil-items-130x40-color") {
		if uint32(c.Row) != red[0].Start.Row || uint32(c.Col) <= red[0].Start.Column || uint32(c.Col) >= edge || c.FG == "" {
			continue
		}
		if !(c.Col == start && c.FG == "8") && !(c.Col == end && c.FG == "default") {
			t.Errorf("the value's colour sequence coloured the row at column %d (fg=%s)", c.Col, c.FG)
		}
	}
}
