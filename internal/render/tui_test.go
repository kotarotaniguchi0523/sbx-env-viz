package render_test

import (
	"bytes"
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
	for _, want := range []string{"sbx-env-viz", "1:Risk", "2:Items", "3:Sources"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("title bar missing %q: %q", want, lines[0])
		}
	}
	for _, want := range []string{"[Host実行 ", "[Host永続 ", "[RWマウント ", "[外への許可 ", "[警告 ", "[未展開 "} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("summary chips missing %q: %q", want, lines[1])
		}
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
			u := newUI(t, tc.width, 30, true)

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
	u := newUI(t, 130, 30, false)
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
	u.press("2", "/", "N", "O", "D", "E", "enter")

	// Assert
	u.want("▍解説", "要は:", "▍上書き規則", "マップ結合:", "▍上書き履歴", "▍出典")
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
	u.press("2", "/", "E", "V", "I", "L", "enter")

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
		{"items grouped by layer", []string{"2"}, []string{"一覧 · Items", "▸ 層 L1"}, nil},
		{"g groups by boundary", []string{"2", "g"}, []string{"▸ 境界: host-exec"}, []string{"▸ 層 L1"}},
		{"g groups by kit", []string{"2", "g", "g"}, []string{"▸ kit: net"}, nil},
		{"g cycles back to layer", []string{"2", "g", "g", "g"}, []string{"▸ 層 L1"}, nil},
		{"tab moves to the next view", []string{"tab"}, []string{"一覧 · Items"}, nil},
		{"sources view", []string{"3"}, []string{"一覧 · Sources", "~/proj/sbxenv.yaml"}, nil},
		{"f filters by the first layer", []string{"f"}, []string{"絞り込み（非表示の警告・エラー", ": L1 ~/proj/sbxenv.yaml"}, nil},
		{"search narrows the list", []string{"2", "/", "a", "g", "e", "n", "t", "enter"}, []string{"agent = claude", "検索: agent（Esc で解除）"}, []string{"ports["}},
		{"esc clears the search", []string{"2", "/", "a", "g", "enter", "esc"}, []string{"ports["}, []string{"検索:"}},
		{"help", []string{"?"}, []string{"キー操作", "▍マウス", "[HOST] ホストで実行"}, nil},
		{"any key closes help", []string{"?", "x"}, []string{"一覧 · Risk"}, []string{"キー操作"}},
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
	u := newUI(t, 80, 30, true)

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
	u := newUI(t, 130, 30, true).press("2", "l")

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
		line, err = tui.Run(p, strings.NewReader("2gq"), &out)
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
	for _, tab := range []struct{ label, view string }{{"2:Items", "Items"}, {"3:Sources", "Sources"}, {"1:Risk", "Risk"}} {
		u.clickOn(tab.label, 0)
		if got := u.listTitle(); !strings.Contains(got, tab.view) {
			t.Errorf("click %s: list title = %q", tab.label, got)
		}
	}
}

func TestTUIMouseClickOnGroupAndFilterChips(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true).press("2")

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
	u := newUI(t, 130, 40, true).press("2")

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
	u := newUI(t, 80, 30, true).press("2")

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
	u := newUI(t, 130, 30, true)

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
	u := newUI(t, 130, 30, true).press("2")
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
