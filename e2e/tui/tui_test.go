package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tuitest "github.com/microsoft/tui-test/bindings/go"
)

// tabs is the numbered tab row: every view with its count, the Overview without one.
var tabs = regexp.MustCompile(`1 概要\s+2 リスク \d+\s+3 設定 \d+\s+4 ファイル \d+\s*$`)

func TestEightyColumnsShowsOnePane(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 80})
	s.press("2")

	// Assert
	lines := s.lines()
	if rows, _ := s.separators(); rows != 0 {
		t.Errorf("pane separator on %d rows at 80 columns, want a single pane\n%s", rows, s.text())
	}
	if !strings.HasPrefix(lines[0], " sbxray  web-app ") || !tabs.MatchString(lines[0]) {
		t.Errorf("title row should name the sandbox and keep every tab with its count: %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "静的解析   絞り込み  全て L1 L2 K1 K2" {
		t.Errorf("context row should hold the filter switches and where the data comes from on Risk: %q", lines[1])
	}
	s.expectMatch(`危険度\s+内容`)
	if strings.Contains(s.text(), "…") {
		t.Errorf("clipped text ends in \"..\", not an ellipsis:\n%s", s.text())
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "? ヘルプ") || !strings.HasSuffix(strings.TrimSpace(last), "q 終了") {
		t.Errorf("status bar should end with help and quit: %q", last)
	}
	s.snapshot("risk-80x40", false)
}

func TestOneHundredThirtyColumnsShowsTwoPanes(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130})
	s.press("2")

	// Assert: one muted rule between the panes on every body row (all but
	// the title, context, blank and status rows), right of 45% of the width.
	rows, columns := s.separators()
	if body := len(s.lines()) - 4; rows != body || len(columns) != 1 || !columns[130*45/100+1] {
		t.Errorf("separator on %d of %d rows in columns %v, want column %d on every row\n%s", rows, body, columns, 130*45/100+1, s.text())
	}
	if n := len(s.locations("╭")) + len(s.locations("╰")); n != 0 {
		t.Errorf("%d rounded corners, want none", n)
	}
	if lines := s.lines(); !tabs.MatchString(lines[0]) {
		t.Errorf("title row: %q", lines[0])
	}
	// Risk is sectioned by bold header rows that the cursor skips: the
	// detail opens on the first finding, not on a header.
	bold := s.styled(tuitest.TextStyle{Bold: tuitest.Ptr(true)})
	for _, section := range []string{"このツールの指摘（5 件）", "設定ごとの危険度（19 件）"} {
		if !slices.ContainsFunc(bold, func(l tuitest.TextMatch) bool { return strings.TrimSpace(l.Text) == section }) {
			t.Errorf("section %q is not a bold header row", section)
		}
	}
	s.expectMatch(`\|  認証情報の差し込み先が未承認`)
	// Item rows show the ID, then the value, without " = ".
	s.expectMatch(`✗  高\s+secrets\.github  command: gh auth token`)
	s.snapshot("risk-130x40", false)
}

func TestDetailPaneExplainsTheItemInJapanese(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 70})

	// Act: Items view, search the variable local.sbxenv.yaml overrides.
	s.press("3", "/", "R", "E", "L", "E", "A", "S", "E", "Enter")

	// Assert
	for _, want := range []string{"env.RELEASE_CHANNEL", "公式の見解（Docker Sandboxes / sandbox-kit-spec）", "公式ドキュメントに特段の警告記載なし", "このツールの判定（独自基準）", "危険度: 情報（INFO）", "要は:", "複数ファイルでの上書き規則", "規則: マップ結合", "上書きの履歴", "上書きされた（無効）: stable", "上書き: beta", "~/proj/local.sbxenv.yaml の 3 行目"} {
		s.expect(want)
	}
	if strings.Contains(s.text(), "▍") {
		t.Errorf("detail headings carry no bar:\n%s", s.text())
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
	s.expect("< 一覧に戻る（esc）")
	s.expect("何が起きるか")
	s.expect("j/k スクロール   esc 戻る")
	s.press("Escape")
	s.gone("< 一覧に戻る")
	s.expectMatch(`危険度\s+内容`)
	s.expect("j/k 移動")
}

func TestQuitPrintsOneSummaryLine(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 100})

	// Act
	full := s.quit()

	// Assert
	var summaries []string
	for _, l := range strings.Split(full, "\n") {
		if strings.HasPrefix(l, "sbxray: Host実行") {
			summaries = append(summaries, strings.TrimSpace(l))
		}
	}
	if len(summaries) != 1 || !regexp.MustCompile(`警告 \d+ \| 未展開 \d+$`).MatchString(summaries[0]) {
		t.Errorf("want one summary line, got %q\n%s", summaries, full)
	}
}

func TestNumberKeysSwitchViews(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	for _, step := range []struct{ key, want string }{
		{"", "1 概要"}, {"2", "2 リスク 24"}, {"3", "3 設定 34"}, {"4", "4 ファイル 9"}, {"1", "1 概要"},
		{"]", "2 リスク 24"}, {"[", "1 概要"}, {"[", "4 ファイル 9"},
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
	s.expect(" 層 L1 ~/proj/base.sbxenv.yaml")
	s.press("g")
	s.expect(" 境界: PC 上でのコマンド実行")
	s.press("g")
	s.expect(" kit: egress")
	// Kit grouping names the kit in the header, so its rows drop the prefix.
	s.expectMatch(`✗  ssh-agent@1/runtime\s+phase=runtime unrestr`)
	for _, l := range s.locations("kit:egress/") {
		if l.Start.Column < 130*45/100 {
			t.Errorf("kit rows repeat the kit their header names: row %d", l.Start.Row)
		}
	}
	s.press("g")
	s.expect(" 層 L1 ~/proj/base.sbxenv.yaml")
	if bold := s.styled(tuitest.TextStyle{Bold: tuitest.Ptr(true)}); !slices.ContainsFunc(bold, func(l tuitest.TextMatch) bool {
		return strings.TrimSpace(l.Text) == "層 L1 ~/proj/base.sbxenv.yaml"
	}) {
		t.Errorf("group header is not bold: %v", bold)
	}
	if strings.Contains(s.text(), "▸") {
		t.Errorf("group headers carry no arrow:\n%s", s.text())
	}
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
	s.expectMatch(`agent\s+claude`)
	if strings.Contains(s.text(), "ports[") || strings.Contains(s.text(), "env.NODE_ENV") {
		t.Errorf("unmatched items should be hidden while searching:\n%s", s.text())
	}
	s.press("Escape")
	s.gone("検索:")
	s.expectMatch(`env\.NODE_ENV\s+development`)
}

func TestHelpScrollsAndAnyOtherKeyCloses(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})

	// Act / Assert
	s.press("?")
	for _, heading := range []string{"キー操作", "マウス", "一覧の印", "詳細に出る目印"} {
		s.expect(heading)
	}
	s.expect("・あなたの PC で実行")
	if strings.Contains(s.text(), "[HOST]") {
		t.Errorf("help lists badge codes instead of their meaning:\n%s", s.text())
	}
	s.expectMatch(`ほか \d+ 行（j でスクロール）`)
	s.press("j")
	s.gone("このツールについて")
	s.expect("キー操作")
	s.press("x")
	s.gone("キー操作")
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

	// Act / Assert: each view is recognised by its column-header row.
	for _, tab := range []struct{ label, header string }{
		{"3 設定", `設定\s+値`}, {"4 ファイル", `ID\s+状態\s+ファイル`}, {"2 リスク", `危険度\s+内容`}, {"1 概要", `項目\s+宣言（sbxenv\.yaml）`},
	} {
		s.click(tab.label)
		s.expectMatch(tab.header)
		if got := activeView(t, s); !strings.HasPrefix(got, tab.label) {
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
	s.expect(" 境界: PC 上でのコマンド実行")
	s.click("kit別")
	s.expect(" kit: egress")
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
	s.clickMatch(`agent\s+claude`)

	// Assert: the detail shows the row, and the list keeps the focus.
	s.expect("解説（agent）")
	s.expect("j/k 移動")
}

func TestMouseOnANarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80})
	s.press("3")

	// Act / Assert
	s.clickMatch(`agent\s+claude`)
	s.expect("< 一覧に戻る（esc）")
	s.expect("解説（agent）")
	s.click("< 一覧に戻る")
	s.gone("< 一覧に戻る")
	s.expectMatch(`設定\s+値`)
	s.expectMatch(`agent\s+claude`)
}

func TestMouseClickOnThePanelsMovesFocus(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130})
	s.press("2")

	// Act / Assert: the detail takes the focus; the list cursor row loses
	// its reverse video and is marked with "›" instead.
	s.click("何が起きるか")
	s.expect("j/k スクロール")
	s.expect("›")
	if listCursor(t, s) {
		t.Errorf("the list cursor stays in reverse video while the detail has focus")
	}

	// Act / Assert: a click on the list (its column header, not a row) takes it back.
	s.click("危険度")
	s.expect("j/k 移動")
	s.gone("›")
	if !listCursor(t, s) {
		t.Errorf("the focused list should show its cursor row in reverse video")
	}
}

// listCursor reports whether a row of the list pane is drawn in reverse video.
func listCursor(t *testing.T, s *screen) bool {
	t.Helper()
	for _, l := range s.styled(tuitest.TextStyle{Inverse: tuitest.Ptr(true)}) {
		if l.Start.Row > 0 && l.Start.Column < 130*45/100 {
			return true
		}
	}
	return false
}

func TestMouseWheelScrollsThePanelUnderThePointer(t *testing.T) {
	t.Parallel()
	// Arrange: 20 rows, so the detail is taller than its panel and can scroll.
	s := start(t, launch{cols: 130, rows: 20})
	s.press("3")
	// Layer grouping drops the "kit:egress/" prefix from list rows and keeps
	// both ends of a long key; the detail title keeps the whole ID.
	row := "network-..lemetry.example.com"
	s.click(row)
	s.expect("値: runtime deny telemetry.example.com")

	// Act: the wheel over the list moves the selection to the next row.
	s.wheel(row, true, 1)

	// Assert
	s.expect("値: service=example")
	s.expect("j/k 移動")

	// Act: the wheel over the detail scrolls it past its first lines.
	s.wheel("値: service=example", true, 3)

	// Assert: the detail scrolled, and the focus stayed on the list.
	s.gone("値: service=example")
	s.expectMatch(`ほか \d+ 行（j でスクロール）`)
	s.expect("j/k 移動")
}

// --- Overview ---------------------------------------------------------------

func TestOverviewIsTheDefaultScreenAsAManifest(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 130, rows: 70})

	// Assert: what the data is, then the counts, then a table of the declaration.
	s.expect(" 宣言 2 ファイル・Kit 2 を静的に解析（稼働中の状態ではありません）")
	s.expectMatch(`✗ 最重要・高 \d+\s+! 中 \d+\s+印なしでも問題がないとは限りません`)
	s.expectMatch(`項目\s+宣言（sbxenv\.yaml）`)
	bold := s.styled(tuitest.TextStyle{Bold: tuitest.Ptr(true)})
	for _, section := range []string{"基本", "Kit", "フォルダの共有", "ポート公開", "通信", "資格情報", "コマンドの実行"} {
		if !slices.ContainsFunc(bold, func(l tuitest.TextMatch) bool { return strings.TrimSpace(l.Text) == section }) {
			t.Errorf("section %q is not a bold heading line", section)
		}
	}
	for _, entry := range []string{`名前\s+web-app`, `playwright\s+リモート（中身は読んでいない。理由は「4 ファイル」で確認）`, `MCP docs\s+リモート（URL に接続）`,
		`禁止（実行中）\s+1 件  telemetry\.example\.com`, `✗  PC 上で実行\s+\./hooks/load-secrets\.sh`} {
		s.expectMatch(entry)
	}
	for _, noise := range []string{"未展開", "[COVERAGE]", "組み込み", "sbx policy", "安全", "[HOST]", "実体（sbx）"} {
		if strings.Contains(s.text(), noise) {
			t.Errorf("the overview shows %q", noise)
		}
	}
	if got := activeView(t, s); got != "1 概要" {
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

	// Assert: the second entry of 基本 is the agent.
	s.expect("解説（agent）")
	if got := activeView(t, s); got != "3 設定 34" {
		t.Errorf("active view = %q", got)
	}
}

func TestAnalysisGapsStayOutOfRiskAndSitQuietlyInSources(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 70})

	// Act / Assert: Risk lists findings only.
	s.press("2")
	s.expectMatch(`危険度\s+内容`)
	for _, noise := range []string{"未展開", "[COVERAGE]"} {
		if strings.Contains(s.text(), noise) {
			t.Errorf("Risk shows %q:\n%s", noise, s.text())
		}
	}

	// Act / Assert: Sources lists the remote kit the file names, not the
	// premises every run shares.
	s.press("4")
	s.expectMatch(`-\s+未展開\s+docker\.io/sbx/playwright:1\.61\.1`)
	if strings.Contains(s.text(), "sbx policy") || strings.Contains(s.text(), "組み込みエージェント") {
		t.Errorf("Sources lists premises of every run:\n%s", s.text())
	}
	s.click("docker.io/sbx/playwright")
	s.expect("未展開: インターネット上の Kit")
	s.expect("問題が見つかったという意味では")
}

func TestOverviewNarrowScreenScrollsAndOpensTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 80, rows: 30})
	s.expectMatch(`テンプレート\s+ghcr\.io/example/common-claude:dev`)

	s.expectMatch(`ほか \d+ 行（j で下へ）`)

	// Act: walk past the first screen.
	for i := 0; i < 50; i++ {
		s.press("j")
	}

	// Assert
	s.expectMatch(`PC 上で実行\s+\./hooks/load-secrets\.sh`)
	s.gone("テンプレート")
	s.press("Enter")
	s.expect("< 一覧に戻る（esc）")
	s.expect("このツールの判定（独自基準）")
}

func TestRiskDetailExplainsTheDiagnosticInJapanese(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 130, rows: 100})
	s.press("2")

	// Act
	s.click("SSH 鍵で何にでも署名できる")

	// Assert
	for _, want := range []string{"種類: 警告（危険・要確認）", "何が起きるか", "どうすればよいか", "対象の設定",
		"公式の見解（Docker Sandboxes / sandbox-kit-spec）", "ssh-agent@1.md",
		"このツールの判定（独自基準）", "危険度: 高（HIGH）", "・SSH 鍵を使う", "公式の評価ではありません", "sandbox の外との関わり"} {
		s.expect(want)
	}
	for _, english := range []string{"SSH_AGENT_UNRESTRICTED", "can sign anything", "ssh-agent 実行/所在", "寿命="} {
		if strings.Contains(s.text(), english) {
			t.Errorf("detail shows %q:\n%s", english, s.text())
		}
	}
}
