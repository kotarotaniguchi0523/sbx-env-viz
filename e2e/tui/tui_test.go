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
	s.press("2")

	// Assert
	lines := s.lines()
	if n := len(s.locations("╭")); n != 1 {
		t.Errorf("%d rounded panels at 80 columns, want 1", n)
	}
	if !regexp.MustCompile(`Host実行\d+ 永続\d+ RW\d+ 許可\d+ E\d+ W\d+`).MatchString(lines[1]) || strings.Contains(lines[1], "未展開") {
		t.Errorf("compact summary should keep every count but no analysis gaps: %q", lines[1])
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
	s.press("2")

	// Assert
	lines := s.lines()
	if n := len(s.locations("╭")); n != 2 {
		t.Errorf("%d rounded panels at 130 columns, want 2", n)
	}
	if n := len(s.locations("╰")); n != 2 {
		t.Errorf("%d bottom corners, want 2", n)
	}
	for _, chip := range []string{"[Host実行 ", "[Host永続 ", "[RWマウント ", "[外への許可 ", "[警告 "} {
		if !strings.Contains(lines[1], chip) {
			t.Errorf("summary chip %q missing: %q", chip, lines[1])
		}
	}
	s.snapshot("risk-130x40", false)
}

func TestDetailPaneExplainsTheItemInJapanese(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 70})

	// Act: Items view, search the variable local.sbxenv.yaml overrides.
	s.press("3", "/", "R", "E", "L", "E", "A", "S", "E", "Enter")

	// Assert
	for _, want := range []string{"env.RELEASE_CHANNEL", "▍公式の見解（Docker Sandboxes / sandbox-kit-spec）", "公式ドキュメントに特段の警告記載なし", "▍このツールの判定（独自基準）", "危険度: 情報（INFO）", "要は:", "▍複数ファイルでの上書き規則", "規則: マップ結合", "▍上書きの履歴", "上書きされた（無効）:", "上書き:", "~/proj/local.sbxenv.yaml の 3 行目"} {
		s.expect(want)
	}
	s.snapshot("items-detail-130x70", false)
}

func TestNarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80})
	s.press("2")

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
	for _, step := range []struct{ key, want string }{
		{"", "1:Overview"}, {"2", "2:Risk"}, {"3", "3:Items"}, {"4", "4:Sources"}, {"1", "1:Overview"},
		{"]", "2:Risk"}, {"[", "1:Overview"}, {"[", "4:Sources"},
	} {
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
	s.press("3")

	// Act / Assert
	s.expect("▸ 層 L1 ~/proj/base.sbxenv.yaml")
	s.press("g")
	s.expect("▸ 境界: PC 上でのコマンド実行")
	s.press("g")
	s.expect("▸ kit: egress")
	s.press("g")
	s.expect("▸ 層 L1 ~/proj/base.sbxenv.yaml")
}

func TestLayerFilterReportsHiddenWarnings(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

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
	s.press("3")

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
	s.expect("あなたの PC で実行")
	s.press("x")
	s.gone("キー操作")
}

// click presses and releases the left button on the first cell of the
// first occurrence of text. The press is written as the SGR mouse sequence
// a terminal sends (1-based coordinates): tui-test v0.1.1's Mouse.Click
// fails on linux/amd64 with "null native string with nonzero length".
func (s *screen) click(text string) {
	s.t.Helper()
	loc := s.locations(text)
	if len(loc) == 0 {
		s.t.Fatalf("click %q: not on screen\n%s", text, s.text())
	}
	x, y := loc[0].Start.Column+1, loc[0].Start.Row+1
	before := s.text()
	if err := s.term.Write(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y)); err != nil {
		s.t.Fatalf("click %q: %v", text, err)
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
	for _, tab := range []struct{ label, title string }{{"3:Items", "一覧 · Items"}, {"4:Sources", "一覧 · Sources"}, {"2:Risk", "一覧 · Risk"}, {"1:Overview", "▍基本構成"}} {
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
	s.press("3")

	// Act / Assert
	s.click("境界別")
	s.expect("▸ 境界: PC 上でのコマンド実行")
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
	s.press("3")

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
	s.press("3")

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
	s.press("2")

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
	s.press("3")
	// "INFO  " only prefixes list rows; the detail title repeats the ID.
	row := "INFO  kit:egress/network-policy@1/runtime.deny"
	s.click(row)
	s.expect("値: runtime deny telemetry.example.com")

	// Act: the wheel over the list moves the selection to the next row.
	s.wheel(row, true, 1)

	// Assert
	s.expect("値: service=example")
	s.expect("● 一覧")

	// Act: the wheel over the detail scrolls it past its first lines.
	s.wheel("値: service=example", true, 3)

	// Assert
	s.gone("値: service=example")
	s.expect("● 一覧")
}

// --- Overview ---------------------------------------------------------------

func TestOverviewIsTheDefaultScreenWithFiveCards(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130, rows: 70})

	// Assert
	for _, card := range []string{"▍基本構成", "▍適用される Kits", "▍ワークスペース・マウント", "▍MCP & Network", "▍資格情報・SSH・ポート・ホスト実行"} {
		s.expect(card)
	}
	if n := len(s.locations("╭")); n != 5 {
		t.Errorf("%d rounded cards, want 5\n%s", n, s.text())
	}
	for _, entry := range []string{"名前: web-app", "playwright: リモートの Kit（中身は 4: Sources）", "MCP docs: リモート（URL に接続）", "通信の禁止（実行中）: telemetry.example.com", "PC 上で実行: "} {
		s.expect(entry)
	}
	for _, noise := range []string{"未展開", "[COVERAGE]", "組み込み", "sbx policy"} {
		if strings.Contains(s.text(), noise) {
			t.Errorf("the overview shows the analysis premise %q", noise)
		}
	}
	if got := activeView(t, s); got != "1:Overview" {
		t.Errorf("active view = %q", got)
	}
	s.snapshot("overview-130x70", false)
}

func TestOverviewEnterOpensTheSelectedItem(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act
	s.press("j", "Enter")

	// Assert: the second entry of 基本構成 is the agent.
	s.expect("一覧 · Items")
	s.expect("▍解説（agent）")
	if got := activeView(t, s); got != "3:Items" {
		t.Errorf("active view = %q", got)
	}
}

func TestAnalysisGapsStayOutOfRiskAndSitQuietlyInSources(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 70})

	// Act / Assert: Risk lists findings only.
	s.press("2")
	s.expect("一覧 · Risk")
	for _, noise := range []string{"未展開", "[COVERAGE]"} {
		if strings.Contains(s.text(), noise) {
			t.Errorf("Risk shows %q:\n%s", noise, s.text())
		}
	}

	// Act / Assert: Sources lists the remote kit the file names, not the
	// premises every run shares.
	s.press("4")
	s.expect("[未展開] docker.io/sbx/playwright:1.61.1")
	if strings.Contains(s.text(), "sbx policy") || strings.Contains(s.text(), "組み込みエージェント") {
		t.Errorf("Sources lists premises of every run:\n%s", s.text())
	}
	s.click("[未展開] docker.io/sbx/playwright")
	s.expect("未展開: インターネット上の Kit")
	s.expect("問題が見つかったという意味ではありません")
}

func TestOverviewNarrowScreenScrollsAndOpensTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80, rows: 30})
	s.expect("▍基本構成")

	// Act: walk past the first screen.
	for i := 0; i < 50; i++ {
		s.press("j")
	}

	// Assert
	s.expect("▍資格情報・SSH・ポート・ホスト実行")
	s.gone("▍基本構成")
	s.press("Enter")
	s.expect("← 戻る")
	s.expect("▍このツールの判定（独自基準）")
}

func TestRiskDetailExplainsTheDiagnosticInJapanese(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 100})
	s.press("2")

	// Act
	s.click("SSH 鍵で何にでも署名できる")

	// Assert
	for _, want := range []string{"重要度: 警告（危険・要確認）", "▍何が起きるか", "▍どうすればよいか", "▍対象の設定",
		"▍公式の見解（Docker Sandboxes / sandbox-kit-spec）", "ssh-agent@1.md",
		"▍このツールの判定（独自基準）", "危険度: 高（HIGH）", "公式の評価ではありません", "▍sandbox の外との関わり"} {
		s.expect(want)
	}
	for _, english := range []string{"SSH_AGENT_UNRESTRICTED", "can sign anything", "ssh-agent 実行/所在", "寿命="} {
		if strings.Contains(s.text(), english) {
			t.Errorf("detail shows %q:\n%s", english, s.text())
		}
	}
}
