package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tuitest "github.com/microsoft/tui-test/bindings/go"
)

func TestEightyColumnsShowsOnePane(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 80})

	// Assert
	lines := s.lines()
	if strings.Contains(s.text(), "│") {
		t.Error("no pane separator at 80 columns")
	}
	if !regexp.MustCompile(`未展開\d+`).MatchString(lines[1]) {
		t.Errorf("compact summary should keep every count: %q", lines[1])
	}
	if !strings.Contains(lines[len(lines)-1], "q:終了 ?:ヘルプ") {
		t.Errorf("footer should lead with quit and help: %q", lines[len(lines)-1])
	}
	s.snapshot("risk-80x40", false)
}

func TestOneHundredThirtyColumnsShowsTwoPanes(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130})

	// Assert
	lines := s.lines()
	body := 0
	for _, l := range lines[3 : len(lines)-1] {
		if strings.Contains(l, "│") {
			body++
		}
	}
	if body < 30 {
		t.Errorf("separator on %d body rows, want every row", body)
	}
	if !regexp.MustCompile(`Host実行 \d+ \| Host永続 \d+ \| RWマウント \d+`).MatchString(lines[1]) {
		t.Errorf("full summary should fit: %q", lines[1])
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
	for _, want := range []string{"env.RELEASE_CHANNEL", "要は:", "上書き規則: マップ結合", "上書き履歴", "shadow", "replace", "~/proj/local.sbxenv.yaml:3"} {
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
	s.expect("出典")
	s.press("Escape")
	s.gone("出典")
	s.expect("1:Risk")
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
	s.expect("g:層別")
	s.expect("層 L1 ~/proj/base.sbxenv.yaml")
	s.press("g")
	s.expect("g:境界別")
	s.expect("境界: host-exec (host)")
	s.press("g")
	s.expect("g:kit別")
	s.expect("kit: egress")
	s.press("g")
	s.expect("g:層別")
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
	s.expect("ports[")
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
