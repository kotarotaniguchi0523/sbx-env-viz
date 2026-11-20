package render_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// ui drives a TUI model the way Bubble Tea does: messages in, frames out.
type ui struct {
	t *testing.T
	m *tui.Model
}

func newUI(t *testing.T, width, height int, noColor bool) *ui {
	t.Helper()
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.New(p, noColor)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	u := &ui{t: t, m: m}
	u.frame()
	return u
}

func (u *ui) press(keys ...string) *ui {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		default:
			r := []rune(k)
			msg = tea.KeyPressMsg{Code: r[0], Text: k}
		}
		u.m.Update(msg)
		u.frame()
	}
	return u
}

// frame renders like the program does after every update (the model
// records its clickable zones while drawing).
func (u *ui) frame() string { return u.m.Render() }

// lines returns the current frame without styling.
func (u *ui) lines() []string { return strings.Split(ansi.Strip(u.frame()), "\n") }

func (u *ui) text() string { return ansi.Strip(u.frame()) }

// find returns the screen cell where text first appears at or after row
// minRow, counting display width so wide characters are measured right.
func (u *ui) find(text string, minRow int) (x, y int) {
	u.t.Helper()
	for row, line := range u.lines() {
		if row < minRow {
			continue
		}
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), row
		}
	}
	u.t.Fatalf("%q not on screen:\n%s", text, u.text())
	return 0, 0
}

func (u *ui) click(x, y int) *ui {
	u.m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	u.frame()
	return u
}

func (u *ui) clickOn(text string, minRow int) *ui {
	x, y := u.find(text, minRow)
	return u.click(x+1, y)
}

func (u *ui) wheel(x, y int, down bool) *ui {
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	u.m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: b})
	u.frame()
	return u
}

func (u *ui) has(text string) bool { return strings.Contains(u.text(), text) }

func (u *ui) want(texts ...string) {
	u.t.Helper()
	for _, s := range texts {
		if !u.has(s) {
			u.t.Errorf("screen missing %q\n%s", s, u.text())
		}
	}
}

// listTitle reads the list panel's title ("一覧 · Items（N 件）").
func (u *ui) listTitle() string {
	for _, l := range u.lines() {
		if i := strings.Index(l, "一覧 · "); i >= 0 {
			return strings.TrimRight(strings.SplitN(l[i:], "│", 2)[0], " ")
		}
	}
	return ""
}

// detailTitle returns the first content line of the detail panel.
func (u *ui) detailFirstLine() string {
	lines := u.lines()
	for i, l := range lines {
		if strings.Contains(l, "詳細") && i+1 < len(lines) {
			next := lines[i+1]
			if j := strings.LastIndex(next, "│ "); j >= 0 && strings.Contains(l, "││") {
				return strings.TrimSpace(strings.Trim(next[j:], "│ "))
			}
			return strings.TrimSpace(strings.Trim(next, "│ "))
		}
	}
	return ""
}

// colorRe matches SGR colour parameters (truecolor, 256-colour, basic).
var colorRe = regexp.MustCompile("\x1b\\[[0-9;]*(38;[25];|48;[25];|3[0-7]m|4[0-7]m|9[0-7]m)")

// --- layout and look -------------------------------------------------------

func TestTUIHeaderShowsTabsSummaryChipsAndStatusBar(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 140, 40, true)

	// Assert
	lines := u.lines()
	for _, want := range []string{"sbx-env-viz", "1:Overview", "2:Risk", "3:Items", "4:Sources"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("title bar missing %q: %q", want, lines[0])
		}
	}
	for _, want := range []string{"[Host実行 ", "[Host永続 ", "[RWマウント ", "[外への許可 ", "[警告 "} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("summary chips missing %q: %q", want, lines[1])
		}
	}
	if strings.Contains(lines[1], "未展開") {
		t.Errorf("gaps of the static analysis are not a header count: %q", lines[1])
	}
	if !strings.Contains(lines[2], "宣言の静的解析") {
		t.Errorf("the static-analysis notice is missing: %q", lines[2])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, " q  終了 ") || !strings.Contains(last, "ヘルプ") || !strings.Contains(last, "マウス") {
		t.Errorf("status bar = %q", last)
	}
	if strings.Contains(u.text(), "安全") {
		t.Error("the TUI must never claim safety")
	}
}

func TestTUIPanelsUseRoundedBordersAndSplitAt120Columns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width  int
		panels int
	}{{80, 1}, {119, 1}, {120, 2}, {160, 2}}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			// Act
			u := newUI(t, tc.width, 30, true).press("2")

			// Assert
			corners := 0
			for _, l := range u.lines() {
				corners += strings.Count(l, "╭")
			}
			if corners != tc.panels {
				t.Errorf("width %d: %d rounded panels, want %d", tc.width, corners, tc.panels)
			}
			for i, l := range u.lines() {
				if w := ansi.StringWidth(l); w > tc.width {
					t.Errorf("row %d is %d cells wide, terminal is %d", i, w, tc.width)
				}
			}
		})
	}
}

// borderColours returns the SGR sequence in effect at each panel's top-left
// corner on the first row that has one.
func borderColours(frame string) []string {
	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "╭") {
			continue
		}
		var out []string
		rest := line
		for {
			i := strings.Index(rest, "╭")
			if i < 0 {
				return out
			}
			before := rest[:i]
			if j := strings.LastIndex(before, "\x1b["); j >= 0 {
				out = append(out, before[j:])
			} else {
				out = append(out, "")
			}
			rest = rest[i+len("╭"):]
		}
	}
	return nil
}

func TestTUIFocusedPanelIsMarkedAndAccented(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, false).press("2")
	const accent, blur = "122;162;247", "59;66;97" // #7aa2f7, #3b4261

	// Act / Assert: the list starts focused.
	if !u.has("● 一覧") || u.has("● 詳細") {
		t.Errorf("the list starts focused:\n%s", u.text())
	}
	if c := borderColours(u.frame()); len(c) != 2 || !strings.Contains(c[0], accent) || !strings.Contains(c[1], blur) {
		t.Errorf("border colours = %q, want accent then dim", c)
	}
	u.press("l")
	if !u.has("● 詳細") || u.has("● 一覧") {
		t.Errorf("l moves focus to the detail panel:\n%s", u.text())
	}
	if c := borderColours(u.frame()); len(c) != 2 || !strings.Contains(c[0], blur) || !strings.Contains(c[1], accent) {
		t.Errorf("after l, border colours = %q, want dim then accent", c)
	}
}

func TestTUIDetailSectionsAndPathWrapping(t *testing.T) {
	t.Parallel()
	// Arrange: a source path far wider than the detail panel, made of
	// hyphenated directory names.
	dir := filepath.Join(t.TempDir(), "team-shared-config", "sandbox-environment-definitions", "per-project-overrides", "web-application-frontend")
	doc := "schemaVersion: \"1\"\nagent: claude\nenv:\n  NODE_ENV: test\n"
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(dir, "sbxenv.yaml"), Content: []byte(doc), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: t.TempDir(), Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	u := &ui{t: t, m: m}

	// Act
	u.press("3", "/", "N", "O", "D", "E", "enter")

	// Assert
	u.want("▍公式の見解（Docker Sandboxes / sandbox-kit-spec）", "▍このツールの判定（独自基準）", "▍解説", "要は:", "▍複数ファイルでの上書き規則", "規則: マップ結合", "▍上書きの履歴", "▍どこで設定されたか")
	wrapped := 0
	for _, l := range u.lines() {
		i := strings.LastIndex(l, "││")
		if i < 0 {
			continue
		}
		right := strings.TrimRight(strings.TrimRight(l[i+len("││"):], "│"), " ")
		if strings.HasSuffix(right, "-") {
			t.Errorf("path broken inside a hyphenated name: %q", right)
		}
		if strings.HasSuffix(right, "/") {
			wrapped++
		}
	}
	if wrapped == 0 {
		t.Errorf("the long path should wrap after a /:\n%s", u.text())
	}
}

func TestTUIColourAndNoColor(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	plain := newUI(t, 130, 30, true).frame()
	coloured := newUI(t, 130, 30, false).frame()

	// Assert
	if colorRe.MatchString(plain) {
		t.Error("NO_COLOR output must not contain colour escapes")
	}
	if !strings.Contains(ansi.Strip(plain), "╭") || !strings.Contains(ansi.Strip(plain), "╰") {
		t.Error("NO_COLOR keeps the panel borders")
	}
	if !colorRe.MatchString(coloured) {
		t.Error("colours expected when NO_COLOR is not set")
	}
	for _, green := range []string{"38;2;0;255;0", "38;2;158;206;106", "38;5;2m", "32m"} {
		if strings.Contains(coloured, green) {
			t.Errorf("green (%s) must not be used", green)
		}
	}
}

func TestTUIEscapesControlCharactersInValues(t *testing.T) {
	t.Parallel()
	// Arrange
	home := t.TempDir()
	doc := "schemaVersion: \"1\"\nagent: claude\nenv:\n  EVIL: \"x\\e[31mred\\nnext\"\n"
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(home, "e.yaml"), Content: []byte(doc), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: home, Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.press("3", "/", "E", "V", "I", "L", "enter")

	// Assert
	if strings.Contains(u.frame(), "\x1b[31m") || u.has("red\nnext") {
		t.Errorf("value control characters reached the screen")
	}
	u.want(`x\x1b[31mred\nnext`)
}

// --- keyboard ----------------------------------------------------------------

func TestTUIKeyboardViewsGroupingFilterSearchAndHelp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		keys []string
		want []string
		not  []string
	}{
		{"overview is the default", nil, []string{"▍基本構成", "▍適用される Kits", "▍ワークスペース・マウント", "▍MCP & Network", "▍資格情報・SSH・ポート・ホスト実行"}, []string{"一覧 · ", "解析範囲外", "未展開", "[COVERAGE]"}},
		{"risk lists no analysis gaps", []string{"2"}, []string{"一覧 · Risk"}, []string{"未展開", "[COVERAGE]"}},
		{"sources lists the gaps the files name, quietly", []string{"4"}, []string{"[未展開] ghcr.io/example/x:dev", "[未展開] docker.io/sbx/playwright:1.61.1", "[展開済み] ~/proj/sbxenv.yaml"}, []string{"[COVERAGE]", "組み込みエージェントの中身", "PC 全体の sbx 設定"}},
		{"risk view", []string{"2"}, []string{"一覧 · Risk"}, nil},
		{"items grouped by layer", []string{"3"}, []string{"一覧 · Items", "▸ 層 L1"}, nil},
		{"g groups by boundary", []string{"3", "g"}, []string{"▸ 境界: PC 上でのコマンド実行"}, []string{"▸ 層 L1"}},
		{"g groups by kit", []string{"3", "g", "g"}, []string{"▸ kit: net"}, nil},
		{"g cycles back to layer", []string{"3", "g", "g", "g"}, []string{"▸ 層 L1"}, nil},
		{"tab moves to the next view", []string{"tab"}, []string{"一覧 · Risk"}, nil},
		{"] moves to the next view", []string{"]", "]"}, []string{"一覧 · Items"}, nil},
		{"[ wraps to the last view", []string{"["}, []string{"一覧 · Sources"}, nil},
		{"1 returns to the overview", []string{"3", "1"}, []string{"▍基本構成"}, []string{"一覧 · "}},
		{"sources view", []string{"4"}, []string{"一覧 · Sources", "~/proj/sbxenv.yaml"}, nil},
		{"f filters by the first layer", []string{"2", "f"}, []string{"絞り込み（非表示の警告・エラー", ": L1 ~/proj/sbxenv.yaml"}, nil},
		{"search narrows the list", []string{"3", "/", "a", "g", "e", "n", "t", "enter"}, []string{"agent = claude", "検索: agent（Esc で解除）"}, []string{"ports["}},
		{"esc clears the search", []string{"3", "/", "a", "g", "enter", "esc"}, []string{"ports["}, []string{"検索:"}},
		{"help", []string{"?"}, []string{"キー操作", "▍マウス", "[HOST]", "あなたの PC で実行", "静的解析の前提"}, nil},
		{"any key closes help", []string{"2", "?", "x"}, []string{"一覧 · Risk"}, []string{"キー操作"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			u := newUI(t, 130, 40, true).press(tc.keys...)

			// Assert
			u.want(tc.want...)
			for _, n := range tc.not {
				if u.has(n) {
					t.Errorf("screen should not contain %q\n%s", n, u.text())
				}
			}
		})
	}
}

func TestTUIFilterHeaderKeepsTheHiddenCountOnLongPaths(t *testing.T) {
	t.Parallel()
	// Arrange
	dir := filepath.Join(t.TempDir(), strings.Repeat("deeply-nested-directory/", 6))
	doc := "schemaVersion: \"1\"\nagent: claude\nports:\n  - {sandbox: 1, hostIP: 0.0.0.0}\n"
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(dir, "sbxenv.yaml"), Content: []byte(doc), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: t.TempDir(), Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.press("f")

	// Assert
	if !regexp.MustCompile(`絞り込み（非表示の警告・エラー \d+ 件）`).MatchString(u.text()) {
		t.Errorf("filter state lost its count:\n%s", u.text())
	}
}

func TestTUINarrowScreenEnterAndEscape(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 80, 30, true).press("2")

	// Act / Assert
	u.press("enter")
	u.want("● 詳細", "← 戻る")
	u.press("esc")
	if u.has("← 戻る") {
		t.Error("esc returns to the list")
	}
	u.want("一覧 · Risk")
}

func TestTUIDetailScrollStopsAtTheEnd(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("3", "l")

	// Act
	for i := 0; i < 200; i++ {
		u.press("j")
	}

	// Assert
	filled := 0
	for _, l := range u.lines() {
		if i := strings.LastIndex(l, "││"); i >= 0 && strings.TrimSpace(strings.Trim(l[i:], "│")) != "" {
			filled++
		}
	}
	if filled < 5 {
		t.Errorf("detail pane scrolled past its end:\n%s", u.text())
	}
}

func TestTUIRunQuitsAndReturnsTheSummaryLine(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactHome)
	var out bytes.Buffer

	// Act
	done := make(chan struct{})
	var line string
	var err error
	go func() {
		line, err = tui.Run(p, strings.NewReader("3gq"), &out)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tui.Run did not exit after q")
	}

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "sbx-env-viz: Host実行") {
		t.Errorf("summary line = %q", line)
	}
	if !strings.Contains(out.String(), "\x1b[?1002h") {
		t.Error("the program should enable cell-motion mouse reporting")
	}
}

// --- mouse -------------------------------------------------------------------

func TestTUIMouseClickOnTabsSwitchesViews(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true)

	// Act / Assert
	for _, tab := range []struct{ label, view string }{{"3:Items", "Items"}, {"4:Sources", "Sources"}, {"2:Risk", "Risk"}} {
		u.clickOn(tab.label, 0)
		if got := u.listTitle(); !strings.Contains(got, tab.view) {
			t.Errorf("click %s: list title = %q", tab.label, got)
		}
	}
	u.clickOn("1:Overview", 0)
	u.want("▍基本構成")
}

func TestTUIMouseClickOnGroupAndFilterChips(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true).press("3")

	// Act / Assert
	u.clickOn("境界別", 3)
	u.want("▸ 境界: ")
	u.clickOn("kit別", 3)
	u.want("▸ kit: net")
	u.clickOn("L2", 3)
	u.want("絞り込み（非表示の警告・エラー", ": L2 ~/proj/local.yaml")
	u.clickOn("全て", 3)
	if u.has("絞り込み（") {
		t.Error("全て clears the filter")
	}
}

func TestTUIMouseClickOnARowSelectsIt(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true).press("3")

	// Act
	u.clickOn("agent = claude", 5)

	// Assert
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail shows %q, want the clicked item", got)
	}
	if !u.has("● 一覧") {
		t.Error("clicking a row focuses the list")
	}
}

func TestTUIMouseOnANarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 80, 30, true).press("3")

	// Act
	u.clickOn("agent = claude", 5)

	// Assert
	u.want("● 詳細", "← 戻る")
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail shows %q", got)
	}
	u.clickOn("← 戻る", 4)
	u.want("一覧 · Items")
}

func TestTUIMouseClickOnAPanelMovesFocus(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("2")

	// Act / Assert
	x, y := u.find("詳細", 4)
	u.click(x+10, y+3)
	if !u.has("● 詳細") {
		t.Errorf("clicking the detail panel focuses it:\n%s", u.text())
	}
	x, y = u.find("一覧", 4)
	u.click(x, y)
	if !u.has("● 一覧") {
		t.Error("clicking the list panel focuses it")
	}
}

func TestTUIMouseWheelScrollsThePanelUnderThePointer(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("3")
	before := u.detailFirstLine()
	lx, ly := u.find("一覧", 4)

	// Act: the wheel over the list moves the selection.
	u.wheel(lx+2, ly+4, true).wheel(lx+2, ly+4, true)

	// Assert
	if after := u.detailFirstLine(); after == before {
		t.Errorf("wheel over the list should move the selection (still %q)", after)
	}
	if !u.has("● 一覧") {
		t.Error("the wheel does not move focus")
	}

	// Act: the wheel over the detail scrolls it.
	dx, dy := u.find("詳細", 4)
	top := u.detailFirstLine()
	u.wheel(dx+5, dy+6, true)

	// Assert
	if got := u.detailFirstLine(); got == top {
		t.Errorf("wheel over the detail should scroll it (still %q)", got)
	}
	u.wheel(dx+5, dy+6, false)
	if got := u.detailFirstLine(); got != top {
		t.Errorf("wheel up returns to the top: %q, want %q", got, top)
	}
}

// --- overview ----------------------------------------------------------------

func TestTUIOverviewCardsAreRoundedPanelsInTwoColumns(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 130, 60, true)

	// Assert
	corners, sideBySide := 0, false
	for _, l := range u.lines() {
		corners += strings.Count(l, "╭")
		if strings.Count(l, "│") >= 4 {
			sideBySide = true
		}
	}
	if corners != 5 {
		t.Errorf("%d cards, want 5\n%s", corners, u.text())
	}
	if !sideBySide {
		t.Errorf("cards should sit in two columns at 130 columns\n%s", u.text())
	}
	u.want("エージェント: claude", "ワークスペース（読み書き）: ~/proj", "通信の許可（実行中）: api.example.com",
		"PC 上で実行: echo hi", "playwright: リモートの Kit（中身は 4: Sources）")
	for _, noise := range []string{"組み込み", "PC 全体の sbx 設定", "[COVERAGE]", "解析範囲外"} {
		if u.has(noise) {
			t.Errorf("overview shows the analysis premise %q", noise)
		}
	}
}

func TestTUIOverviewEnterOpensTheItemInItems(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true)

	// Act
	u.press("enter")

	// Assert
	if got := u.listTitle(); !strings.Contains(got, "Items") {
		t.Errorf("list title = %q, want Items", got)
	}
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail = %q, want the selected entry's item", got)
	}
}

func TestTUIOverviewClickOnAFindingOpensItInItems(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 60, true)

	// Act
	u.clickOn("PC 上で実行: echo hi", 4)

	// Assert
	if got := u.listTitle(); !strings.Contains(got, "Items") {
		t.Errorf("list title = %q, want Items", got)
	}
	if got := u.detailFirstLine(); got != "lifecycle.initialize[0]" {
		t.Errorf("detail = %q", got)
	}
}

func TestTUIOverviewScrollsToKeepTheSelectionOnANarrowScreen(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 80, 24, true)

	// Act: walk to the last entry, far below the first screen.
	for i := 0; i < 40; i++ {
		u.press("j")
	}

	// Assert
	u.want("▶ [HOST][OPAQUE] PC 上で実行: echo hi", "▍資格情報・SSH・ポート・ホスト実行")
	if u.has("▍基本構成") {
		t.Error("the first card should have scrolled away")
	}
	u.press("enter")
	u.want("● 詳細", "← 戻る", "lifecycle.initialize[0]")
}

func TestTUISourcesExplainAGapAsAPremise(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 50, true).press("4")

	// Act
	u.clickOn("[未展開] docker.io/sbx/playwright", 4)

	// Assert
	if got := u.detailFirstLine(); got != "未展開: インターネット上の Kit" {
		t.Errorf("detail = %q", got)
	}
	u.want("▍なぜ中身を確認していないか", "静的解析の前提で、問題が見つかったという意味ではありません")
	if u.has("危険度") {
		t.Error("a premise of the analysis gets no risk level")
	}
}

// detailText is the detail panel of a two-pane frame.
func (u *ui) detailText() string {
	var b strings.Builder
	for _, l := range u.lines() {
		if i := strings.LastIndex(l, "││"); i >= 0 {
			b.WriteString(strings.TrimRight(strings.TrimSuffix(l[i+len("││"):], "│"), " "))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func TestTUIDetailPaneSpeaksPlainJapaneseOnly(t *testing.T) {
	t.Parallel()
	// Arrange: English machine text the Plan carries for JSON consumers.
	p, _ := newPlan(t, sbxenv.RedactHome)
	var english []string
	for _, d := range p.Diagnostics {
		english = append(english, d.Message)
	}
	for _, b := range p.Boundaries {
		english = append(english, b.Reason)
	}
	for _, c := range p.Coverage {
		english = append(english, c.Reason)
	}
	prose := regexp.MustCompile(`[a-z]+(?: [a-z]+){3,}`)
	if prose.FindString(strings.Join(english, "\n")) == "" {
		t.Fatal("the fixture carries no English prose to look for")
	}
	machine := regexp.MustCompile(`実行/所在=|寿命=|proxyManaged|host-exec|host-persistent|network-egress|not expanded|\[(WARNING|ERROR)\]`)

	for _, view := range []string{"2", "3", "4"} {
		u := newUI(t, 250, 200, true).press(view)
		var n int
		if _, err := fmt.Sscanf(u.listTitle()[strings.Index(u.listTitle(), "（")+len("（"):], "%d", &n); err != nil || n == 0 {
			t.Fatalf("view %s: list title %q", view, u.listTitle())
		}
		for i := 0; i < n; i++ {
			// Act
			d := u.detailText()

			// Assert
			for _, e := range english {
				// A run of four plain English words is prose, not a value.
				if w := prose.FindString(e); w != "" && strings.Contains(d, w) {
					t.Errorf("view %s row %d shows English %q:\n%s", view, i, w, d)
				}
			}
			if m := machine.FindString(d); m != "" {
				t.Errorf("view %s row %d shows %q:\n%s", view, i, m, d)
			}
			if view != "4" && !strings.Contains(d, "危険度") && !strings.Contains(d, "重要度") {
				t.Errorf("view %s row %d does not state a risk level:\n%s", view, i, d)
			}
			u.press("j")
		}
	}
}

func TestTUIItemDetailExplainsItsRisk(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 60, true).press("3", "/", "i", "n", "i", "t", "enter")

	// Assert
	u.want("▍公式の見解（Docker Sandboxes / sandbox-kit-spec）", "lifecycle のコマンドはホスト上であなたのユーザー権限で動きます",
		"--skip-host-commands", "#approve-or-skip-host-commands", "▍このツールの判定（独自基準）", "危険度: 高（HIGH）", "理由: ", "公式の評価ではありません",
		"[HOST] あなたの PC で実行", "▍sandbox の外との関わり", "PC 上でのコマンド実行")
}

func TestTUIDetailSaysWhenTheDocsGiveNoWarning(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 130, 60, true).press("3", "/", "N", "O", "D", "E", "enter")

	// Assert
	u.want("▍公式の見解（Docker Sandboxes / sandbox-kit-spec）", "公式ドキュメントに特段の警告記載なし", "▍このツールの判定（独自基準）", "危険度: 情報（INFO）")
}
