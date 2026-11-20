package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tuitest "github.com/microsoft/tui-test/bindings/go"
)

func TestEightyColumnsShowsOneRoundedPanel(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 80})

	// Assert
	lines := s.lines()
	if n := len(s.locations("╭")); n != 1 {
		t.Errorf("%d rounded panels at 80 columns, want 1", n)
	}
	if !regexp.MustCompile(`未展開\d+`).MatchString(lines[1]) {
		t.Errorf("compact summary should keep every count: %q", lines[1])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, " q  終了 ") || !strings.Contains(last, " ?  ヘルプ ") {
		t.Errorf("status bar should lead with quit and help: %q", last)
	}
	s.snapshot("risk-80x40", false)
}

func TestOneHundredThirtyColumnsShowsTwoRoundedPanels(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130})

	// Assert
	lines := s.lines()
	if n := len(s.locations("╭")); n != 2 {
		t.Errorf("%d rounded panels at 130 columns, want 2", n)
	}
	if n := len(s.locations("╰")); n != 2 {
		t.Errorf("%d bottom corners, want 2", n)
	}
	for _, chip := range []string{"[Host実行 ", "[Host永続 ", "[RWマウント ", "[外への許可 ", "[警告 ", "[未展開 "} {
		if !strings.Contains(lines[1], chip) {
			t.Errorf("summary chip %q missing: %q", chip, lines[1])
		}
	}
	s.snapshot("risk-130x40", false)
}

func TestDetailPaneExplainsTheItemInJapanese(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act: Items view, search the variable local.sbxenv.yaml overrides.
	s.press("2", "/", "R", "E", "L", "E", "A", "S", "E", "Enter")

	// Assert
	for _, want := range []string{"env.RELEASE_CHANNEL", "要は:", "▍上書き規則", "マップ結合:", "▍上書き履歴", "shadow", "replace", "~/proj/local.sbxenv.yaml:3"} {
		s.expect(want)
	}
	s.snapshot("items-detail-130x40", false)
}

func TestNarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80})

	// Act / Assert
	s.press("Enter")
	s.expect("● 詳細")
	s.expect("← 戻る")
	s.press("Escape")
	s.gone("← 戻る")
	s.expect("一覧 · Risk")
}

func TestQuitPrintsOneSummaryLine(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 100})

	// Act
	s.press("q")
	if err := s.term.WaitExit(tuitest.WaitOptions{Timeout: tuitest.Ptr(10 * time.Second)}); err != nil {
		t.Fatalf("wait exit: %v", err)
	}

	// Assert
	state, err := s.term.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Exited == nil || *state.Exited != 0 {
		t.Errorf("exit code = %v, want 0", state.Exited)
	}
	full, err := s.term.Text(tuitest.TextOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	var summaries []string
	for _, l := range strings.Split(full, "\n") {
		if strings.HasPrefix(l, "sbx-env-viz: Host実行") {
			summaries = append(summaries, strings.TrimSpace(l))
		}
	}
	if len(summaries) != 1 || !regexp.MustCompile(`警告 \d+ \| 未展開 \d+$`).MatchString(summaries[0]) {
		t.Errorf("want one summary line, got %q\n%s", summaries, full)
	}
}

// activeView returns the tab label drawn in reverse video on the first row.
func activeView(t *testing.T, s *screen) string {
	t.Helper()
	locs, err := s.term.GetByStyle(tuitest.TextStyle{Inverse: tuitest.Ptr(true)}, tuitest.StyleSelectorOptions{}).Locations()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range locs {
		if l.Start.Row == 0 {
			return strings.TrimSpace(l.Text)
		}
	}
	return ""
}

func TestNumberKeysSwitchViews(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	for _, step := range []struct{ key, want string }{{"", "1:Risk"}, {"2", "2:Items"}, {"3", "3:Sources"}, {"1", "1:Risk"}} {
		if step.key != "" {
			s.press(step.key)
		}
		if got := activeView(t, s); got != step.want {
			t.Errorf("after %q active view = %q, want %q", step.key, got, step.want)
		}
	}
}

func TestGroupingCyclesThroughLayerBoundaryKit(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

	// Act / Assert
	s.expect("▸ 層 L1 ~/proj/base.sbxenv.yaml")
	s.press("g")
	s.expect("▸ 境界: host-exec (host)")
	s.press("g")
	s.expect("▸ kit: egress")
	s.press("g")
	s.expect("▸ 層 L1 ~/proj/base.sbxenv.yaml")
}

func TestLayerFilterReportsHiddenWarnings(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act
	s.press("f")

	// Assert
	s.expect("絞り込み（非表示の警告・エラー")
	if !regexp.MustCompile(`絞り込み（非表示の警告・エラー \d+ 件）: L1 ~/proj/base\.sbxenv\.yaml`).MatchString(s.text()) {
		t.Errorf("filter header:\n%s", s.text())
	}
	s.press("f")
	s.expect(": L2 ~/proj/local.sbxenv.yaml")
}

func TestSearchNarrowsAndEscapeClears(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

	// Act
	s.press("/", "a", "g", "e", "n", "t", "Enter")

	// Assert
	s.expect("検索: agent（Esc で解除）")
	s.expect("agent = claude")
	if strings.Contains(s.text(), "ports[") {
		t.Error("unmatched items should be hidden while searching")
	}
	s.press("Escape")
	s.gone("検索:")
	s.expect("一覧 · Items（34 件）")
}

func TestHelpOpensAndAnyKeyCloses(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	s.press("?")
	s.expect("キー操作")
	s.expect("[HOST] ホストで実行")
	s.press("x")
	s.gone("キー操作")
}

// click presses the left button on the first occurrence of text.
func (s *screen) click(text string) {
	s.t.Helper()
	before := s.text()
	if err := s.term.Mouse.Click(tuitest.MouseClickOptions{OnText: tuitest.Ptr(text)}); err != nil {
		s.t.Fatalf("click %q: %v\n%s", text, err, s.text())
	}
	s.settle(before)
}

// wheel scrolls at the first occurrence of text. tui-test v0.1.1's
// Mouse.Scroll always reports column 0, row 0, so the wheel event is written
// as the SGR mouse sequence a terminal sends (button 64 up, 65 down,
// 1-based coordinates).
func (s *screen) wheel(text string, down bool, notches int) {
	s.t.Helper()
	loc := s.locations(text)
	if len(loc) == 0 {
		s.t.Fatalf("%q not on screen", text)
	}
	button := 64
	if down {
		button = 65
	}
	before := s.text()
	for i := 0; i < notches; i++ {
		seq := fmt.Sprintf("\x1b[<%d;%d;%dM", button, loc[0].Start.Column+1, loc[0].Start.Row+1)
		if err := s.term.Write(seq); err != nil {
			s.t.Fatalf("wheel: %v", err)
		}
	}
	s.settle(before)
}

func TestTerminalReportsMouseInCellMotionMode(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 100})

	// Assert: the program asked the terminal for button-and-drag reporting
	// (cell motion); without it a real terminal sends no clicks at all.
	state, err := s.term.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.MouseMode != "drag" {
		t.Errorf("terminal mouse mode = %q, want drag (cell motion)", state.MouseMode)
	}
}

func TestMouseClickOnTabsSwitchesViews(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	for _, tab := range []struct{ label, title string }{{"2:Items", "一覧 · Items"}, {"3:Sources", "一覧 · Sources"}, {"1:Risk", "一覧 · Risk"}} {
		s.click(tab.label)
		s.expect(tab.title)
		if got := activeView(t, s); got != tab.label {
			t.Errorf("after clicking %s the highlighted tab is %q", tab.label, got)
		}
	}
}

func TestMouseClickOnGroupAndFilterChips(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

	// Act / Assert
	s.click("境界別")
	s.expect("▸ 境界: host-exec (host)")
	s.click("kit別")
	s.expect("▸ kit: egress")
	s.click("L2")
	s.expect(": L2 ~/proj/local.sbxenv.yaml")
	s.click("全て")
	s.gone("絞り込み（")
}

func TestMouseClickOnARowShowsItsDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

	// Act
	s.click("agent = claude")

	// Assert
	s.expect("▍解説（agent）")
	s.expect("● 一覧")
}

func TestMouseOnANarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80})
	s.press("2")

	// Act / Assert
	s.click("agent = claude")
	s.expect("← 戻る")
	s.expect("▍解説（agent）")
	s.click("← 戻る")
	s.expect("一覧 · Items")
}

func TestMouseClickOnThePanelsMovesFocus(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	s.click("詳細")
	s.expect("● 詳細")
	s.click("一覧 · Risk")
	s.expect("● 一覧")
}

func TestMouseWheelScrollsThePanelUnderThePointer(t *testing.T) {
	t.Parallel()
	// Arrange: 20 rows, so the detail is taller than its panel and can scroll.
	s := start(t, launch{cols: 130, rows: 20})
	s.press("2")
	// "INFO  " only prefixes list rows; the detail title repeats the ID.
	row := "INFO  kit:egress/network-policy@1/runtime.deny"
	s.click(row)
	s.expect("▍解説（network-policy@1）")

	// Act: the wheel over the list moves the selection to the next row.
	s.wheel(row, true, 1)

	// Assert
	s.expect("▍解説（credential@1）")
	s.expect("● 一覧")

	// Act: the wheel over the detail scrolls it past its first heading.
	s.wheel("▍解説（credential@1）", true, 3)

	// Assert
	s.gone("▍解説（credential@1）")
	s.expect("● 一覧")
}
