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

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
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

// footer is the key-hint line at the bottom of the screen.
func (u *ui) footer() string {
	l := u.lines()
	return l[len(l)-1]
}

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

// rowY returns the screen row whose list part (the left pane, or the whole
// line on a one-pane screen) matches pattern.
func (u *ui) rowY(pattern string) int {
	u.t.Helper()
	re := regexp.MustCompile(pattern)
	for y, l := range u.lines() {
		if left, _, _ := splitPanes(l); re.MatchString(left) {
			return y
		}
	}
	u.t.Fatalf("no row matches %q:\n%s", pattern, u.text())
	return 0
}

// clickRow clicks the list row matching pattern.
func (u *ui) clickRow(pattern string) *ui { return u.click(2, u.rowY(pattern)) }

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

// wantRe checks that every pattern matches some line of the screen.
func (u *ui) wantRe(patterns ...string) {
	u.t.Helper()
	for _, p := range patterns {
		if !regexp.MustCompile("(?m)" + p).MatchString(u.text()) {
			u.t.Errorf("no line matches %q\n%s", p, u.text())
		}
	}
}

// hasLine reports whether some screen line, trimmed, is exactly s (a
// section heading, for example).
func (u *ui) hasLine(s string) bool {
	for _, l := range u.lines() {
		if strings.TrimSpace(l) == s {
			return true
		}
	}
	return false
}

// paneRule separates the list and the detail on a two-pane screen.
const paneRule = " | "

// backLink is the first line of the detail shown alone on a narrow screen.
const backLink = "< 一覧に戻る（esc）"

// splitPanes cuts a two-pane line at the rule between list and detail.
func splitPanes(l string) (left, right string, ok bool) {
	i := strings.Index(l, paneRule)
	if i < 0 {
		return l, "", false
	}
	return l[:i], l[i+len(paneRule):], true
}

// detailText is the detail pane: the right of the rule on a two-pane
// screen, or what follows the back link when the detail is shown alone.
func (u *ui) detailText() string {
	lines := u.lines()
	var out []string
	for _, l := range lines {
		if _, r, ok := splitPanes(l); ok {
			out = append(out, strings.TrimRight(r, " "))
		}
	}
	if len(out) == 0 {
		for i, l := range lines {
			if strings.Contains(l, backLink) {
				for _, d := range lines[i+1 : len(lines)-1] {
					out = append(out, strings.TrimRight(d, " "))
				}
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// detailFirstLine returns the first non-blank line of the detail pane.
func (u *ui) detailFirstLine() string {
	for _, l := range strings.Split(u.detailText(), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			return s
		}
	}
	return ""
}

// viewHeads tell the views apart by their underlined column-header rows.
var viewHeads = []struct {
	name string
	re   *regexp.Regexp
}{
	{"概要", regexp.MustCompile(`^ {4}項目 +宣言（sbxenv\.yaml）`)},
	{"リスク", regexp.MustCompile(`^ {4}危険度  内容`)},
	{"設定", regexp.MustCompile(`^ {4}設定 +値`)},
	{"ファイル", regexp.MustCompile(`^ {4}ID +状態 +ファイル`)},
}

// view names the view on screen by its column-header row, or "".
func (u *ui) view() string {
	for _, l := range u.lines() {
		for _, h := range viewHeads {
			if h.re.MatchString(l) {
				return h.name
			}
		}
	}
	return ""
}

func (u *ui) wantView(name string) {
	u.t.Helper()
	if got := u.view(); got != name {
		u.t.Errorf("view = %q, want %q\n%s", got, name, u.text())
	}
}

// sgrRe matches one Select Graphic Rendition escape.
var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// attrsAt returns the SGR parameters in effect at the first cell of text
// on a raw (styled) screen line, or nil when text is not on the line.
func attrsAt(raw, text string) map[string]bool {
	at := strings.Index(ansi.Strip(raw), text)
	if at < 0 {
		return nil
	}
	attrs := map[string]bool{}
	plain := 0
	for i := 0; i < len(raw); {
		if m := sgrRe.FindStringSubmatchIndex(raw[i:]); m != nil && m[0] == 0 {
			params := raw[i+m[2] : i+m[3]]
			if params == "" || params == "0" {
				attrs = map[string]bool{}
			}
			for _, p := range strings.Split(params, ";") {
				if p != "" && p != "0" {
					attrs[p] = true
				}
			}
			i += m[1]
			continue
		}
		if plain == at {
			return attrs
		}
		plain++
		i++
	}
	return nil
}

// rawLine returns the styled screen line whose plain text contains text.
func (u *ui) rawLine(text string) string {
	u.t.Helper()
	for _, l := range strings.Split(u.frame(), "\n") {
		if strings.Contains(ansi.Strip(l), text) {
			return l
		}
	}
	u.t.Fatalf("%q not on screen:\n%s", text, u.text())
	return ""
}

// sgrParams lists every SGR parameter a frame uses.
func sgrParams(frame string) []int {
	var out []int
	for _, m := range sgrRe.FindAllStringSubmatch(frame, -1) {
		for _, p := range strings.Split(m[1], ";") {
			if p == "" {
				continue
			}
			n, err := strconv.Atoi(p)
			if err != nil {
				panic(err)
			}
			out = append(out, n)
		}
	}
	return out
}

// --- layout and look -------------------------------------------------------

func TestTUIHeaderShowsTabsContextAndKeyHints(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 140, 40, true)

	// Assert
	lines := u.lines()
	for _, want := range []string{`^ sbxray `, `1 概要`, `2 リスク \d+`, `3 設定 \d+`, `4 ファイル \d+`} {
		if !regexp.MustCompile(want).MatchString(lines[0]) {
			t.Errorf("title bar missing %q: %q", want, lines[0])
		}
	}
	raw := strings.Split(u.frame(), "\n")[0]
	if !attrsAt(raw, "1 概要")["4"] || attrsAt(raw, "2 リスク")["4"] {
		t.Errorf("only the active tab is underlined: %q", raw)
	}
	if !strings.Contains(lines[1], "宣言 2 ファイル・Kit 1 を静的に解析（稼働中の状態ではありません）") {
		t.Errorf("the static-analysis notice is missing: %q", lines[1])
	}
	if strings.Contains(lines[1], "未展開") {
		t.Errorf("gaps of the static analysis are not a header count: %q", lines[1])
	}
	u.wantRe(`^ ✗ 最重要・高 \d+    ! 中 \d+    印なしでも問題がないとは限りません`)
	if f := u.footer(); !strings.Contains(f, "q 終了") || !strings.Contains(f, "? ヘルプ") || !strings.Contains(f, "enter 詳細") {
		t.Errorf("key hints = %q", f)
	}
	if strings.Contains(u.text(), "安全") {
		t.Error("the TUI must never claim safety")
	}
}

func TestTUITabCountsMatchTheirLists(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ key, name string }{{"2", "リスク"}, {"3", "設定"}, {"4", "ファイル"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			u := newUI(t, 250, 200, true).press(tc.key)
			n, _ := strconv.Atoi(regexp.MustCompile(tc.name + ` (\d+)`).FindStringSubmatch(u.lines()[0])[1])

			// Act: walk the list, past its end.
			seen := map[string]bool{}
			for i := 0; i < n+3; i++ {
				seen[u.detailFirstLine()] = true
				u.press("j")
			}

			// Assert
			if n == 0 || len(seen) != n {
				t.Errorf("tab says %d, the list holds %d entries", n, len(seen))
			}
		})
	}
}

func TestTUIPanesSplitAt120Columns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width int
		two   bool
	}{{80, false}, {119, false}, {120, true}, {160, true}}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			// Act
			u := newUI(t, tc.width, 30, true).press("2")

			// Assert
			cols := map[int]bool{}
			for i, l := range u.lines() {
				if w := ansi.StringWidth(l); w > tc.width {
					t.Errorf("row %d is %d cells wide, terminal is %d", i, w, tc.width)
				}
				if strings.ContainsAny(l, "╭╮╰╯│…") {
					t.Errorf("row %d draws a border, box rule or ellipsis glyph: %q", i, l)
				}
				if left, _, ok := splitPanes(l); ok {
					cols[ansi.StringWidth(left)] = true
				}
			}
			if two := len(cols) > 0; two != tc.two {
				t.Errorf("width %d: two panes = %v, want %v\n%s", tc.width, two, tc.two, u.text())
			}
			if len(cols) > 1 {
				t.Errorf("the rule between the panes moves between rows: %v", cols)
			}
		})
	}
}

func TestTUIFocusShowsInTheListCursorAndTheKeyHints(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("2")
	const first = "Kit が書き換え可能な場所にある"
	cursorRow := func() string { return u.lines2raw(u.rowY(`^.[✗! ]  高 +` + first)) }

	// Act / Assert: the list starts focused; its cursor row is reverse video.
	if a := attrsAt(cursorRow(), first); !a["7"] {
		t.Errorf("the focused list's cursor row is reverse video: %v", a)
	}
	if !strings.Contains(u.footer(), "j/k 移動") {
		t.Errorf("key hints for the list: %q", u.footer())
	}

	// Act / Assert: l moves focus to the detail; the list cursor becomes › and bold.
	u.press("l")
	row := cursorRow()
	if a := attrsAt(row, first); a["7"] || !a["1"] {
		t.Errorf("an unfocused list's cursor row is bold, not reverse: %v", a)
	}
	if !strings.HasPrefix(ansi.Strip(row), "›") {
		t.Errorf("an unfocused list's cursor row starts with ›: %q", ansi.Strip(row))
	}
	if f := u.footer(); !strings.Contains(f, "j/k スクロール") || !strings.Contains(f, "esc 戻る") {
		t.Errorf("key hints for the detail: %q", f)
	}

	// Act / Assert: h returns.
	u.press("h")
	if a := attrsAt(cursorRow(), first); !a["7"] {
		t.Errorf("h focuses the list again: %v", a)
	}
}

func TestTUIDetailSectionsAndPathWrapping(t *testing.T) {
	t.Parallel()
	// Arrange: a source path far wider than the detail pane, made of
	// hyphenated directory names.
	dir := filepath.Join(t.TempDir(), "team-shared-config", "sandbox-environment-definitions", "per-project-overrides", "web-application-frontend")
	doc := "schemaVersion: \"1\"\nagent: claude\nenv:\n  NODE_ENV: test\n"
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(dir, "sbxenv.yaml"), Content: []byte(doc), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: t.TempDir(), Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 90})
	u := &ui{t: t, m: m}

	// Act
	u.press("3", "/", "N", "O", "D", "E", "enter")

	// Assert
	u.wantRe(`\|  公式の見解（Docker Sandboxes / sandbox-kit-spec）`, `\|  このツールの判定（独自基準）`, `\|  解説（`,
		`\|  要は: `, `\|  複数ファイルでの上書き規則`, `\|  規則: マップ結合`, `\|  上書きの履歴`, `\|  どこで設定されたか`)
	wrapped := 0
	for _, l := range strings.Split(u.detailText(), "\n") {
		if strings.HasSuffix(l, "-") {
			t.Errorf("path broken inside a hyphenated name: %q", l)
		}
		if strings.HasSuffix(l, "/") {
			wrapped++
		}
	}
	if wrapped == 0 {
		t.Errorf("the long path should wrap after a /:\n%s", u.text())
	}
}

func TestTUIColourAndNoColor(t *testing.T) {
	t.Parallel()
	screens := [][]string{nil, {"2"}, {"2", "l"}, {"3"}, {"4"}, {"?"}}
	for _, keys := range screens {
		t.Run(strings.Join(keys, ""), func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			plain := newUI(t, 130, 40, true).press(keys...).frame()
			coloured := newUI(t, 130, 40, false).press(keys...).frame()

			// Assert: NO_COLOR has no colour at all.
			for _, n := range sgrParams(plain) {
				if (n >= 30 && n <= 49) || (n >= 90 && n <= 107) {
					t.Errorf("NO_COLOR output uses colour parameter %d", n)
				}
			}
			// Colour mode uses the 16 ANSI foregrounds only: no 256-colour,
			// no truecolor, no background except reverse video, no green.
			colours := 0
			for _, n := range sgrParams(coloured) {
				switch {
				case n == 32 || n == 92:
					t.Errorf("green (%d) must not be used", n)
				case (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
					colours++
				case n == 0 || n == 1 || n == 2 || n == 3 || n == 4 || n == 7 || n == 22 || n == 23 || n == 24 || n == 27 || n == 39:
				default:
					t.Errorf("colour mode uses SGR parameter %d (only ANSI-16 foregrounds and text attributes are allowed)", n)
				}
			}
			if colours == 0 {
				t.Error("colours expected when NO_COLOR is not set")
			}
		})
	}
}

func TestTUINoColorKeepsStructureWithAttributes(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 130, 40, true)

	// Assert: the selection is reverse video and column headers are underlined.
	if a := attrsAt(u.rawLine("エージェント"), "エージェント"); !a["7"] {
		t.Errorf("the selected Overview row is reverse video: %v", a)
	}
	if a := attrsAt(u.rawLine("宣言（sbxenv.yaml）"), "項目"); !a["4"] {
		t.Errorf("the Overview's column header is underlined: %v", a)
	}
	u.press("3")
	if a := attrsAt(u.rawLine("層 L1"), "層 L1"); !a["1"] {
		t.Errorf("a group heading is bold: %v", a)
	}
	if a := attrsAt(u.lines2raw(u.rowY(`^ {4}設定 +値`)), "設定"); !a["4"] {
		t.Errorf("the list's column header is underlined: %v", a)
	}
}

// lines2raw returns the styled screen line y.
func (u *ui) lines2raw(y int) string { return strings.Split(u.frame(), "\n")[y] }

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

func TestTUIKeepsWideCharactersInsideTheirColumns(t *testing.T) {
	t.Parallel()
	// Arrange: a Japanese value wider than the value column.
	home := t.TempDir()
	doc := "schemaVersion: \"1\"\nagent: claude\nenv:\n  GREETING: " + strings.Repeat("日本語の値", 20) + "\n"
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(home, "e.yaml"), Content: []byte(doc), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: home, Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{80, 121, 130} {
		m := tui.New(p, true)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		u := &ui{t: t, m: m}

		// Act
		u.press("3", "/", "G", "R", "E", "E", "T", "enter")

		// Assert
		cols := map[int]bool{}
		for i, l := range u.lines() {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: row %d is %d cells wide", width, i, w)
			}
			if left, _, ok := splitPanes(l); ok {
				cols[ansi.StringWidth(left)] = true
			}
		}
		if len(cols) > 1 {
			t.Errorf("width %d: a wide value pushed the pane rule: %v\n%s", width, cols, u.text())
		}
		u.wantRe(`GREETING +日本語の値`)
	}
}

// --- keyboard ----------------------------------------------------------------

func TestTUIKeyboardViewsGroupingFilterSearchAndHelp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		keys  []string
		view  string
		want  []string
		lines []string
		not   []string
	}{
		{"overview is the default", nil, "概要", nil, []string{"基本", "Kit", "フォルダの共有", "ポート公開", "通信", "コマンドの実行"}, []string{"解析範囲外", "未展開", "[COVERAGE]", "[HOST]", "実体（sbx）"}},
		{"risk lists no analysis gaps", []string{"2"}, "リスク", []string{`^ ✗  高 +lif\S*\[0\]  echo hi`}, nil, []string{"未展開", "[COVERAGE]", "[HOST]"}},
		{"sources lists the gaps the files name, quietly", []string{"4"}, "ファイル", []string{`^ +- +未展開 +ghcr\.io/example/x:dev`, `^ +- +未展開 +docker\.io/sbx/playwright:1\.61\.1`, `^ +L1 +展開済み +~/proj/sbxenv\.yaml`}, nil, []string{"[COVERAGE]", "組み込みエージェントの中身", "PC 全体の sbx 設定"}},
		{"items grouped by layer", []string{"3"}, "設定", []string{`^ 層 L1 ~/proj/sbxenv\.yaml`, `^ {4}agent +claude`}, nil, []string{"▸", " = "}},
		{"g groups by boundary", []string{"3", "g"}, "設定", []string{`^ 境界: PC 上でのコマンド実行`}, nil, []string{"層 L1"}},
		{"g groups by kit", []string{"3", "g", "g"}, "設定", []string{`^ kit: net`}, nil, nil},
		{"g cycles back to layer", []string{"3", "g", "g", "g"}, "設定", []string{`^ 層 L1`}, nil, nil},
		{"tab moves to the next view", []string{"tab"}, "リスク", nil, nil, nil},
		{"] moves to the next view", []string{"]", "]"}, "設定", nil, nil, nil},
		{"[ wraps to the last view", []string{"["}, "ファイル", nil, nil, nil},
		{"1 returns to the overview", []string{"3", "1"}, "概要", nil, []string{"基本"}, nil},
		{"sources view", []string{"4"}, "ファイル", []string{`~/proj/sbxenv\.yaml`}, nil, nil},
		{"f filters by the first layer", []string{"2", "f"}, "リスク", []string{`^ 絞り込み（非表示の警告・エラー \d+ 件）: L1 ~/proj/sbxenv\.yaml`}, nil, nil},
		{"search narrows the list", []string{"3", "/", "a", "g", "e", "n", "t", "enter"}, "設定", []string{`^ {4}agent +claude`, `^ 検索: agent（Esc で解除）`}, nil, []string{"ports["}},
		{"esc clears the search", []string{"3", "/", "a", "g", "enter", "esc"}, "設定", []string{`ports\[`}, nil, []string{"検索:"}},
		{"search shows its cursor while typing", []string{"3", "/", "a"}, "設定", []string{`^ 検索: a_ *$`}, nil, nil},
		{"help", []string{"?"}, "", []string{"あなたの PC で実行", "静的解析の前提", "✗  このツールの基準で危険度が最重要・高", "!  危険度が中", "太字・下線の値は、宣言と違う"}, []string{"キー操作", "マウス", "一覧の印", "詳細に出る目印"}, []string{"[HOST]"}},
		{"any key closes help", []string{"2", "?", "x"}, "リスク", nil, nil, []string{"キー操作"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			u := newUI(t, 130, 40, true).press(tc.keys...)

			// Assert
			u.wantView(tc.view)
			u.wantRe(tc.want...)
			for _, l := range tc.lines {
				if !u.hasLine(l) {
					t.Errorf("no line reads %q\n%s", l, u.text())
				}
			}
			for _, n := range tc.not {
				if u.has(n) {
					t.Errorf("screen should not contain %q\n%s", n, u.text())
				}
			}
		})
	}
}

func TestTUIKitGroupingDropsTheKitPrefixFromRows(t *testing.T) {
	t.Parallel()
	for _, keys := range [][]string{{"3"}, {"3", "g", "g"}} {
		// Act
		u := newUI(t, 80, 30, true).press(keys...)

		// Assert: the group heading names the kit, so the rows do not; a long
		// key is cut in the middle so its last segment stays readable.
		u.wantRe(`^ {4}network-poli\S*\.\.\S*/api\.example\.com +runtime allow api\.example\.com`)
		if u.has("kit:net/") {
			t.Errorf("keys %v: a row repeats the kit its heading names:\n%s", keys, u.text())
		}
	}
	// Grouped by boundary, rows of different kits mix, so the prefix stays.
	newUI(t, 80, 30, true).press("3", "g").wantRe(`^ {4}kit:net/\S*\.\.\S*/api\.example\.com +runtime allow`)
}

func TestTUIHelpScrollsWithJKAndClosesOnOtherKeys(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 20, true).press("2", "?")

	// Assert: a clipped help says how much follows.
	u.wantRe(`^ +ほか \d+ 行（j でスクロール） *$`)
	if f := u.footer(); !strings.Contains(f, "j/k スクロール") || !strings.Contains(f, "ほかのキー 閉じる") {
		t.Errorf("key hints in help = %q", f)
	}

	// Act / Assert: j scrolls, and stops at the end.
	for i := 0; i < 200; i++ {
		u.press("j")
	}
	if u.hasLine("このツールについて") || !u.has("詳細に出る目印") || u.has("ほか ") {
		t.Errorf("j scrolls the help to its end:\n%s", u.text())
	}
	// k scrolls back at once, since the scroll stopped at the end.
	u.press("k")
	if !u.has("詳細に出る目印") {
		t.Errorf("k scrolls back by one line:\n%s", u.text())
	}
	for i := 0; i < 200; i++ {
		u.press("k")
	}
	if !u.hasLine("このツールについて") {
		t.Errorf("k scrolls the help back to its top:\n%s", u.text())
	}

	// Act / Assert: any other key closes it.
	u.press("x")
	u.wantView("リスク")
	if u.has("キー操作") {
		t.Error("a key other than j/k closes the help")
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
	u.press("2", "f")

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
	u.want(backLink)
	if got := u.detailFirstLine(); got != "Kit が書き換え可能な場所にある" {
		t.Errorf("detail = %q", got)
	}
	u.wantRe(`^ Kit が書き換え可能な場所にある *\n 危険度: 高（HIGH） *\n 種類: 警告（危険・要確認）`,
		`^ 対象の設定 *\n   kits\[\./kits/net\] *\n   値: \./kits/net`)
	if !strings.Contains(u.footer(), "esc 戻る") {
		t.Errorf("key hints = %q", u.footer())
	}
	u.press("esc")
	if u.has(backLink) {
		t.Error("esc returns to the list")
	}
	u.wantView("リスク")
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
	for _, l := range strings.Split(u.detailText(), "\n") {
		if strings.TrimSpace(l) != "" {
			filled++
		}
	}
	if filled < 5 {
		t.Errorf("detail pane scrolled past its end:\n%s", u.text())
	}
	if strings.Contains(u.detailText(), "ほか ") {
		t.Errorf("at its end the detail says nothing follows:\n%s", u.text())
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
	if !strings.HasPrefix(line, "sbxray: Host実行") {
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
	for _, tab := range []struct{ label, view string }{{"3 設定", "設定"}, {"4 ファイル", "ファイル"}, {"2 リスク", "リスク"}, {"1 概要", "概要"}} {
		u.clickOn(tab.label, 0)
		u.wantView(tab.view)
	}
}

func TestTUIMouseClickOnGroupAndFilterSwitches(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true).press("3")

	// Act / Assert
	u.clickOn("境界別", 1)
	u.wantRe(`^ 境界: `)
	u.clickOn("kit別", 1)
	u.wantRe(`^ kit: net`)
	u.clickOn("L2", 1)
	u.wantRe(`^ 絞り込み（非表示の警告・エラー \d+ 件）: L2 ~/proj/local\.yaml`)
	u.clickOn("全て", 1)
	if u.has("絞り込み（") {
		t.Error("全て clears the filter")
	}
}

func TestTUIMouseClickOnARowSelectsIt(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true).press("3")

	// Act
	u.clickRow(`^ {4}agent +claude`)

	// Assert
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail shows %q, want the clicked item", got)
	}
	if a := attrsAt(u.lines2raw(u.rowY(`^ {4}agent +claude`)), "agent"); !a["7"] {
		t.Errorf("clicking a row focuses the list (reverse-video cursor): %v", a)
	}
}

func TestTUIMouseOnANarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 80, 30, true).press("3")

	// Act
	u.clickRow(`^ {4}agent +claude`)

	// Assert
	u.want(backLink)
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail shows %q", got)
	}
	u.clickOn(backLink, 1)
	if u.has(backLink) {
		t.Error("the back link closes the detail")
	}
	u.wantView("設定")
}

func TestTUIMouseClickOnAPaneMovesFocus(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("2")
	_, top := u.find("危険度  内容", 1)
	rule, _ := u.find(paneRule, top)

	// Act / Assert
	u.click(rule+10, top+3)
	if !strings.Contains(u.footer(), "j/k スクロール") {
		t.Errorf("clicking the detail pane focuses it: %q", u.footer())
	}
	u.click(5, len(u.lines())-3)
	if !strings.Contains(u.footer(), "j/k 移動") {
		t.Errorf("clicking the list pane focuses it: %q", u.footer())
	}
}

func TestTUIMouseWheelScrollsThePaneUnderThePointer(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 30, true).press("3")
	before := u.detailFirstLine()
	_, top := u.find("設定  ", 1)
	rule, _ := u.find(paneRule, top)

	// Act: the wheel over the list moves the selection.
	u.wheel(4, top+4, true).wheel(4, top+4, true)

	// Assert
	if after := u.detailFirstLine(); after == before {
		t.Errorf("wheel over the list should move the selection (still %q)", after)
	}
	if !strings.Contains(u.footer(), "j/k 移動") {
		t.Error("the wheel does not move focus")
	}

	// Act: the wheel over the detail scrolls it.
	topLine := u.detailFirstLine()
	u.wheel(rule+5, top+6, true)

	// Assert
	if got := u.detailFirstLine(); got == topLine {
		t.Errorf("wheel over the detail should scroll it (still %q)", got)
	}
	u.wheel(rule+5, top+6, false)
	if got := u.detailFirstLine(); got != topLine {
		t.Errorf("wheel up returns to the top: %q, want %q", got, topLine)
	}
}

// --- overview ----------------------------------------------------------------

func TestTUIOverviewIsAManifestTable(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 130, 60, true)

	// Assert: one row per declared setting, marked in the margin.
	u.wantRe(`^ {4}エージェント +claude`, `^ !  ワークスペース +~/proj（読み書き）`, `^ ✗  ポート +0\.0\.0\.0:ephemeral -> sandbox:3000/tcp4`,
		`^ {4}許可（実行中） +1 件  api\.example\.com`, `^ ✗  PC 上で実行 +echo hi`, `^ {4}playwright +リモート（中身は「4 ファイル」で確認）`)
	// The declared values line up in one column.
	cols := map[int]bool{}
	for _, v := range []string{"claude", "~/proj（読み書き）", "echo hi", "リモート（中身は"} {
		x, _ := u.find(v, 4)
		cols[x] = true
	}
	if len(cols) != 1 {
		t.Errorf("declared values are not in one column: %v\n%s", cols, u.text())
	}
	for _, noise := range []string{"組み込み", "PC 全体の sbx 設定", "[COVERAGE]", "解析範囲外", "[HOST]", "[OPAQUE]"} {
		if u.has(noise) {
			t.Errorf("overview shows %q", noise)
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
	u.wantView("設定")
	if got := u.detailFirstLine(); got != "agent" {
		t.Errorf("detail = %q, want the selected entry's item", got)
	}
}

func TestTUIOverviewClickOnAnEntryOpensItInItems(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 60, true)

	// Act
	u.clickRow(`PC 上で実行 +echo hi`)

	// Assert
	u.wantView("設定")
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
	if a := attrsAt(u.rawLine("PC 上で実行"), "PC 上で実行"); !a["7"] {
		t.Errorf("the last entry is selected and on screen: %v\n%s", a, u.text())
	}
	if !u.hasLine("コマンドの実行") {
		t.Errorf("its section heading is on screen:\n%s", u.text())
	}
	if u.hasLine("基本") {
		t.Error("the first section should have scrolled away")
	}
	u.press("enter")
	u.want(backLink)
	if got := u.detailFirstLine(); got != "lifecycle.initialize[0]" {
		t.Errorf("detail = %q", got)
	}
}

func TestTUISourcesExplainAGapAsAPremise(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 50, true).press("4")

	// Act
	u.clickRow(`^ +- +未展開 +docker\.io/sbx/playwright`)

	// Assert
	if got := u.detailFirstLine(); got != "未展開: インターネット上の Kit" {
		t.Errorf("detail = %q", got)
	}
	u.want("なぜ中身を確認していないか", "これは静的解析の前提で、問題が見つかったという意味では")
	if u.has("危険度") {
		t.Error("a premise of the analysis gets no risk level")
	}
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

	for _, view := range []struct{ key, name string }{{"2", "リスク"}, {"3", "設定"}, {"4", "ファイル"}} {
		u := newUI(t, 250, 200, true).press(view.key)
		var n int
		if _, err := fmt.Sscanf(u.lines()[0][strings.Index(u.lines()[0], view.name)+len(view.name):], "%d", &n); err != nil || n == 0 {
			t.Fatalf("view %s: tab count in %q", view.name, u.lines()[0])
		}
		for i := 0; i < n; i++ {
			// Act
			d := u.detailText()

			// Assert
			for _, e := range english {
				// A run of four plain English words is prose, not a value.
				if w := prose.FindString(e); w != "" && strings.Contains(d, w) {
					t.Errorf("view %s row %d shows English %q:\n%s", view.name, i, w, d)
				}
			}
			if m := machine.FindString(d); m != "" {
				t.Errorf("view %s row %d shows %q:\n%s", view.name, i, m, d)
			}
			if view.key != "4" && !strings.Contains(d, "危険度: ") {
				t.Errorf("view %s row %d does not state a risk level:\n%s", view.name, i, d)
			}
			u.press("j")
		}
	}
}

func TestTUIItemDetailExplainsItsRisk(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 110, true).press("3", "/", "i", "n", "i", "t", "enter")

	// Assert
	u.want("公式の見解（Docker Sandboxes / sandbox-kit-spec）", "lifecycle のコマンドはホスト上であなたのユーザー権限で動きます",
		"--skip-host-commands", "#approve-or-skip-host-commands", "このツールの判定（独自基準）", "危険度: 高（HIGH）", "理由: ", "公式の評価ではありません",
		"・あなたの PC で実行", "・中身を確認していないコマンド", "sandbox の外との関わり", "PC 上でのコマンド実行")
	if u.has("[HOST]") {
		t.Error("the detail names badges in words, not codes")
	}
}

func TestTUIDetailSaysWhenTheDocsGiveNoWarning(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 130, 60, true).press("3", "/", "N", "O", "D", "E", "enter")

	// Assert
	u.want("公式の見解（Docker Sandboxes / sandbox-kit-spec）", "公式ドキュメントに特段の警告記載なし", "このツールの判定（独自基準）", "危険度: 情報（INFO）")
}

func TestTUIDetailShowsExternalStandardsSeparately(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newUI(t, 160, 110, true).press("3", "/", "i", "n", "i", "t", "enter")

	// Assert
	u.want("外部セキュリティ基準の観点（OWASP 等）", "ASI05 Unexpected Code Execution",
		"OWASP AI Agent Security Cheat Sheet Do's and Don'ts", "出典: https://genai.owasp.org/resource/")
	d := u.detailText()
	official, external, judgement := strings.Index(d, "公式の見解"), strings.Index(d, "外部セキュリティ基準の観点"), strings.Index(d, "このツールの判定")
	if official < 0 || official > external || external > judgement {
		t.Errorf("order should be official view, external standards, this tool's judgement:\n%s", d)
	}
}

// --- discovered sandboxes ------------------------------------------------------

// newTargetsUI shows three discovered targets: a declaration paired with a
// running sandbox whose agent differs and which publishes a port nothing
// declares, a sandbox without files, and a declaration not yet created.
func newTargetsUI(t *testing.T, width, height int) *ui {
	t.Helper()
	p, _ := newPlan(t, sbxenv.RedactHome)
	running := &live.Sandbox{Name: "web", Agent: "codex", Status: "running", RulesKnown: true, SecretsKnown: true,
		Ports: []live.Port{{HostIP: "127.0.0.1", HostPort: 9000, SandboxPort: 9000, Protocol: "tcp"}}}
	cmp := live.Compare(p, running, &live.Snapshot{Available: true}, nil)
	orphan := &live.Sandbox{Name: "orphan", Agent: "claude", Status: "stopped",
		Workspaces: []live.Mount{{Path: "/srv/orphan"}}, Ports: []live.Port{{HostIP: "127.0.0.1", HostPort: 8080, SandboxPort: 80, Protocol: "tcp"}}}
	q, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{
		{Name: "web", State: "稼働中", Files: []string{"~/proj/sbxenv.yaml"}, Plan: p, Sandbox: running, Comparison: &cmp},
		{Name: "orphan", State: "停止中・宣言ファイル未検出", Sandbox: orphan},
		{Name: "api", State: "宣言のみ（未作成）", Files: []string{"~/api/sbxenv.yaml"}, Plan: q},
	}, "", true)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	u := &ui{t: t, m: m}
	u.frame()
	return u
}

func TestTUITargetBarNamesEverySandboxWithItsState(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newTargetsUI(t, 160, 50)

	// Assert
	lines := u.lines()
	if !strings.HasPrefix(lines[0], " sbxray  web  稼働中 ") {
		t.Errorf("title bar names the target and its state: %q", lines[0])
	}
	for _, want := range []string{" 対象  ", "web（稼働中・ズレ ", "orphan（停止中・宣言ファイル未検出）", "api（宣言のみ（未作成））"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("target bar missing %q: %q", want, lines[1])
		}
	}
	if !strings.Contains(lines[2], "宣言 2 ファイルと sbx の sandbox（読み取り専用コマンド）を照合") {
		t.Errorf("context line = %q", lines[2])
	}
	u.wantRe(`^ ✗ 最重要・高 \d+    ! 中 \d+    ズレ \d+ `, `^ {4}項目 +宣言（sbxenv\.yaml） +実体（sbx）`)
}

func TestTUISinglePlanHasNoTargetBar(t *testing.T) {
	t.Parallel()
	// Act
	u := newUI(t, 130, 40, true)

	// Assert
	if strings.HasPrefix(u.lines()[1], " 対象 ") || u.has("ズレ") || u.has("実体（sbx）") {
		t.Errorf("a plan from the command line has no target bar or live column:\n%s", u.text())
	}
}

func TestTUIOverviewComparesEachEntryWithTheSandbox(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newTargetsUI(t, 160, 60)

	// Assert: drift, and areas sbx cannot report, are told apart per row.
	u.wantRe(`^ !  エージェント +claude +codex *$`, `^ ✗  ポート +0\.0\.0\.0:ephemeral -> sandbox:3000/tcp4 +（公開されていない） *$`,
		`^ {4}テンプレート +ghcr\.io/example/x:dev +照合なし *$`, `^ ✗  PC 上で実行 +echo hi +照合なし *$`)
	u.want("lifecycle の実行履歴などは sbx の一覧から取得できないため比較していません")
	// Regression: Kit network rules sbx policy ls cannot show were not
	// compared, so the row must not claim a match.
	u.wantRe(`^ {4}許可（実行中） +1 件  api\.example\.com +照合なし *$`)
	if u.has("一致") {
		t.Errorf("nothing in this fixture was confirmed equal:\n%s", u.text())
	}
	// A drifted value is bold and underlined; uncompared areas are not.
	if a := attrsAt(u.rawLine("（公開されていない）"), "（公開されていない）"); !a["1"] || !a["4"] {
		t.Errorf("a drifted value is bold and underlined: %v", a)
	}
	if a := attrsAt(u.rawLine("ghcr.io/example/x:dev"), "照合なし"); a["4"] || a["1"] {
		t.Errorf("an uncompared area is not marked as drift: %v", a)
	}
}

func TestTUIKeysSwitchTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		keys       []string
		name, want string
	}{
		{nil, "web", "実体（sbx）"},
		{[]string{">"}, "orphan", "sbx の sandbox（宣言ファイル未検出）"},
		{[]string{">", ">"}, "api", "まだ作成されていません"},
		{[]string{"<"}, "api", "まだ作成されていません"},
		{[]string{">", ">", ">"}, "web", "実体（sbx）"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.keys, ""), func(t *testing.T) {
			t.Parallel()
			// Act
			u := newTargetsUI(t, 160, 50).press(tc.keys...)

			// Assert
			if !strings.HasPrefix(u.lines()[0], " sbxray  "+tc.name+"  ") {
				t.Errorf("title bar = %q, want target %s", u.lines()[0], tc.name)
			}
			u.want(tc.want)
		})
	}
}

func TestTUIClickOnATargetSwitchesToIt(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newTargetsUI(t, 160, 50)

	// Act
	u.clickOn("orphan", 1)

	// Assert
	u.want("sbx の sandbox（宣言ファイル未検出）", "ワークスペース: /srv/orphan", "ポート公開: 127.0.0.1:8080 -> sandbox:80/tcp", "sbx の sandbox のみ（宣言ファイル未検出）")
}

func TestTUIPickerListsTargetsAndSwitchesByKeyOrClick(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newTargetsUI(t, 160, 50)

	// Act / Assert: s opens the list, j and Enter pick the third target.
	u.press("s")
	u.want("対象の sandbox を選ぶ")
	u.wantRe(`^ {4}名前 +状態 +ズレ +ファイル`, `^ {4}web +稼働中 +\d+ +~/proj/sbxenv\.yaml`,
		`^ {4}orphan +停止中.* +- +（設定ファイルなし）`, `^ {4}api +宣言のみ（未作成） +- +~/api/sbxenv\.yaml`)
	if a := attrsAt(u.lines2raw(u.rowY(`^ {4}web +稼働中`)), "web"); !a["7"] {
		t.Errorf("the current target is selected: %v", a)
	}
	u.press("j", "j", "enter")
	u.want("まだ作成されていません")
	if u.has("対象の sandbox を選ぶ") {
		t.Error("Enter closes the list")
	}

	// Act / Assert: a click on a row picks it too.
	u.press("s")
	u.clickRow(`^ {4}orphan +停止中`)
	u.want("sbx の sandbox（宣言ファイル未検出）")
}

func TestTUIDriftOpensSideBySideInRisk(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newTargetsUI(t, 160, 60)

	// Act: a drift no declared line stands for has its own section.
	u.clickRow(`^ [✗! ]  ポート公開 +（宣言なし） +127\.0\.0\.1:9000`)

	// Assert
	u.wantView("リスク")
	if got := u.detailFirstLine(); got != "ズレ: 宣言にないポートが公開されている" {
		t.Errorf("detail = %q", got)
	}
	u.want("宣言（sbxenv.yaml）", "現在の実体（sbx）", "（宣言なし）", "どうすればよいか", "sbx ports --unpublish", "比較の根拠", "sbx ls --json")
}

func TestTUIOverviewEnterOnADriftedEntryOpensTheDrift(t *testing.T) {
	t.Parallel()
	// Arrange: the first Overview entry is エージェント, whose sandbox runs codex.
	u := newTargetsUI(t, 160, 60)

	// Act
	u.press("enter")

	// Assert: regression; the drift opens in リスク, not the setting in 設定.
	u.wantView("リスク")
	if got := u.detailFirstLine(); got != "ズレ: 宣言と違うエージェントで動いている" {
		t.Errorf("detail = %q", got)
	}
	d := u.detailText()
	for _, want := range []string{"宣言（sbxenv.yaml）", "claude", "現在の実体（sbx）", "codex"} {
		if !strings.Contains(d, want) {
			t.Errorf("drift detail missing %q:\n%s", want, d)
		}
	}

	// Act / Assert: a click on another drifted entry does the same.
	u.press("1").clickRow(`^ [✗! ]  ポート +0\.0\.0\.0:ephemeral`)
	u.wantView("リスク")
	if got := u.detailFirstLine(); got != "ズレ: 宣言したポートが公開されていない" {
		t.Errorf("detail = %q", got)
	}
}

func TestTUIRiskListsDriftsFirst(t *testing.T) {
	t.Parallel()
	// Act
	u := newTargetsUI(t, 160, 60).press("2")

	// Assert
	if got := u.detailFirstLine(); got != "ズレ: 宣言と違うエージェントで動いている" {
		t.Errorf("detail = %q", got)
	}
	u.want("宣言（sbxenv.yaml）", "claude", "現在の実体（sbx）", "codex", "sbx env rm")
	u.wantRe(`^ {4}危険度  内容 +\| *.*\n sbx とのズレ（4 件） `, `^ !  中 +エージェント: 宣言と違うエージェントで動いている `)
}

func TestTUIRiskIsSectionedAndSortedByRisk(t *testing.T) {
	t.Parallel()
	// Act
	u := newUI(t, 80, 40, true).press("2")

	// Assert: each section counts its rows, which run from the highest risk down.
	rank := map[string]int{"最重要": 4, "高": 3, "中": 2, "低": 1, "情報": 0}
	headRe := regexp.MustCompile(`^ (\S+)（(\d+) 件） *$`)
	rowRe := regexp.MustCompile(`^ [✗! ]  (最重要|高|中|低|情報) `)
	var names []string
	counts := map[string]int{}
	want := map[string]int{}
	last := 99
	for _, l := range u.lines() {
		if m := headRe.FindStringSubmatch(l); m != nil {
			names = append(names, m[1])
			want[m[1]], _ = strconv.Atoi(m[2])
			last = 99
			continue
		}
		if m := rowRe.FindStringSubmatch(l); m != nil && len(names) > 0 {
			counts[names[len(names)-1]]++
			if r := rank[m[1]]; r > last {
				t.Errorf("section %s is not sorted by risk: %q", names[len(names)-1], l)
			} else {
				last = r
			}
		}
	}
	if strings.Join(names, ",") != "このツールの指摘,設定ごとの危険度" {
		t.Errorf("sections = %v (no live data, so no drift section)", names)
	}
	for _, n := range names {
		if counts[n] != want[n] {
			t.Errorf("section %s says %d, lists %d", n, want[n], counts[n])
		}
	}

	// Act / Assert: the cursor never rests on a section heading.
	u.press("k", "enter")
	if got := u.detailFirstLine(); got != "Kit が書き換え可能な場所にある" {
		t.Errorf("k at the top stays on the first row: %q", got)
	}
	u.press("esc", "j", "j", "j", "enter")
	if got := u.detailFirstLine(); got != "ports[3000/tcp4]" {
		t.Errorf("j steps over the next section heading: %q", got)
	}
}

func TestTUIClippedPanesSayHowMuchFollows(t *testing.T) {
	t.Parallel()
	// Overview and lists end with a count of the rows below.
	newUI(t, 80, 24, true).wantRe(`^ +ほか \d+ 行（j で下へ） *$`)
	newUI(t, 80, 12, true).press("2").wantRe(`^ +ほか \d+ 行（j で下へ） *$`)
	// The detail says how far it scrolls.
	newUI(t, 130, 30, true).press("3").wantRe(`\|  ほか \d+ 行（j でスクロール） *$`)
	// Nothing is promised when everything fits.
	if u := newUI(t, 250, 200, true).press("2"); u.has("ほか ") {
		t.Errorf("a list that fits has no clip note:\n%s", u.text())
	}
}

func TestTUITargetPositionReplacesATargetLineThatDoesNotFit(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	u := newTargetsUI(t, 80, 30)

	// Assert
	if !strings.HasPrefix(u.lines()[0], " sbxray  web  稼働中 1/3 ") {
		t.Errorf("title bar names the position: %q", u.lines()[0])
	}
	for _, l := range u.lines() {
		if strings.HasPrefix(l, " 対象  ") {
			t.Errorf("a target line that does not fit is not drawn: %q", l)
		}
	}
	// A long state gives way to the tabs, and the position stays.
	u.press(">")
	if l := u.lines()[0]; !strings.HasPrefix(l, " sbxray  orphan  停止中") || !strings.Contains(l, ".. 2/3 ") || !strings.Contains(l, "4 ファイル") {
		t.Errorf("title bar after > = %q", l)
	}
	if newTargetsUI(t, 160, 30).has(" 1/3") {
		t.Error("the position is shown only when the target line is not")
	}
}

func TestTUIShowsLiveErrorWithoutHidingPlanAndKeepsTargetErrorVisible(t *testing.T) {
	t.Parallel()
	// Arrange: target 0 has a valid plan plus a live sbx error; target 1 failed to parse.
	p, _ := newPlan(t, sbxenv.RedactHome)
	const liveErr = "sbx コマンドが見つかりません"
	const parseErr = "設定ファイルを解析できませんでした: line 3: invalid YAML"
	m := tui.NewTargets([]live.Target{
		{Name: "web", State: "宣言", Plan: p, Err: liveErr},
		{Name: "broken", State: "エラー", Files: []string{"~/broken/sbxenv.yaml"}, Err: parseErr},
	}, liveErr, true)
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	u := &ui{t: t, m: m}

	// Assert: live error appears in state line while Overview still renders the plan.
	u.want(liveErr)
	u.wantRe(`PC 上で実行 +echo hi`)
	if !u.hasLine("基本") {
		t.Errorf("the plan's Overview is shown:\n%s", u.text())
	}
	if u.has("設定ファイルを解析できません") {
		t.Error("live sbx error must not replace a valid plan's Overview")
	}

	// Act: switch to broken target and move to Risk view.
	u.press(">")
	u.want("設定ファイルを解析できません", parseErr)
	u.press("2")
	u.want(parseErr)
}

// targetUI shows the targets at width x height.
func targetUI(t *testing.T, width, height int, targets ...live.Target) *ui {
	t.Helper()
	m := tui.NewTargets(targets, "", true)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	u := &ui{t: t, m: m}
	u.frame()
	return u
}

func TestTUIOverviewClaimsAMatchOnlyForWhatWasCompared(t *testing.T) {
	t.Parallel()
	// Arrange: the area of folders and of network rules was checked, but
	// only the workspace itself was held against sbx's output.
	p, _ := newPlan(t, sbxenv.RedactHome)
	sb := &live.Sandbox{Name: "web", Status: "running"}
	checked := []string{"ワークスペース・マウント", "通信の許可・拒否"}
	for _, tc := range []struct {
		compared []string
		want     string
	}{
		{nil, "照合なし"},
		// sbx ls reports the host path only, so that is all that matches.
		{[]string{"workspace"}, "パスは一致"},
	} {
		cmp := &live.Comparison{Checked: checked, Compared: tc.compared}

		// Act
		u := targetUI(t, 160, 60, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: sb, Comparison: cmp})

		// Assert: regression; a checked area used to confirm every row in it.
		u.wantRe(`^ [!✗ ]  ワークスペース +\S+（読み書き） +` + tc.want + ` *$`)
		u.wantRe(`^ {4}許可（実行中） +1 件  api\.example\.com +照合なし *$`)
	}
}

func TestTUIOverviewGroupOpensItsRiskiestItem(t *testing.T) {
	t.Parallel()
	// Arrange: two runtime allow rules, the harmless one first.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "kit:net/network-policy@1/runtime.allow/one.test", Path: "cap:network-policy@1", Value: "one.test", Risk: sbxenv.SeverityLow},
		{ID: "kit:net/network-policy@1/runtime.allow/*", Path: "cap:network-policy@1", Value: "*", Risk: sbxenv.SeverityHigh},
	}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.wantRe(`^ ✗  許可（実行中） +2 件  one\.test、\*`)
	u.press("enter")

	// Assert: regression; Enter opened the first item, rated low.
	if got := u.detailFirstLine(); got != "kit:net/network-policy@1/runtime.allow/*" {
		t.Errorf("detail = %q\n%s", got, u.text())
	}
}

func TestTUIOverviewScrollsToLinesBelowTheLastEntry(t *testing.T) {
	t.Parallel()
	// Arrange: a sandbox no file declares has information lines only.
	sb := &live.Sandbox{Name: "orphan", Status: "stopped"}
	for i := range 25 {
		sb.Workspaces = append(sb.Workspaces, live.Mount{Path: fmt.Sprintf("/work/p%02d", i)})
	}
	u := targetUI(t, 80, 15, live.Target{Name: "orphan", State: "停止中・宣言ファイル未検出", Sandbox: sb})
	u.want("ほか ")

	// Act
	for range 40 {
		u.press("j")
	}

	// Assert: regression; j could not move past the hint.
	u.want("ワークスペース: /work/p24")
	if u.has("ほか ") {
		t.Errorf("at the end nothing follows:\n%s", u.text())
	}

	// Act / Assert: k scrolls back at once.
	u.press("k")
	u.want("ほか ")
}

func TestTUIOverviewClickOnTheOverflowHintOpensNothing(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 80, 14, true)

	// Act
	u.clickOn("ほか ", 0)

	// Assert: regression; the hint replaced a row whose zone stayed live.
	u.wantView("概要")
	if u.has(backLink) {
		t.Errorf("a click on the hint opened a detail:\n%s", u.text())
	}
}

func TestTUIOverviewGivesEachDriftedRuleItsOwnRow(t *testing.T) {
	t.Parallel()
	// Arrange: three runtime allow rules; sbx lacks two of them.
	rule := func(host string) sbxenv.Item {
		return sbxenv.Item{ID: "kit:net/network-policy@1/runtime.allow/" + host, Path: "cap:network-policy@1", Value: host, Risk: sbxenv.SeverityLow}
	}
	p := &sbxenv.Plan{Items: []sbxenv.Item{rule("one.test"), rule("two.test"), rule("three.test")}}
	missing := func(host string) live.Drift {
		return live.Drift{Kind: "通信の許可・拒否", Title: "宣言した通信の許可が反映されていない（" + host + "）", Declared: host,
			Actual: "（該当するルールなし）", Severity: sbxenv.SeverityMedium, ItemID: "kit:net/network-policy@1/runtime.allow/" + host}
	}
	cmp := &live.Comparison{Checked: []string{"通信の許可・拒否"}, Drifts: []live.Drift{missing("two.test"), missing("three.test")},
		Compared: []string{"kit:net/network-policy@1/runtime.allow/one.test", "kit:net/network-policy@1/runtime.allow/two.test", "kit:net/network-policy@1/runtime.allow/three.test"}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; one grouped row hid which host was missing,
	// and its second drift could not be opened from the Overview.
	u.wantRe(`^ {4}許可（実行中） +1 件  one\.test +一致 *$`,
		`^ !  許可（実行中） +two\.test +（該当するルールなし） *$`, `^ !  許可（実行中） +three\.test +（該当するルールなし） *$`)
	if u.has("宣言にない実体・その他のズレ") {
		t.Errorf("every drift belongs to a declared row:\n%s", u.text())
	}
	u.clickRow(`^ !  許可（実行中） +three\.test`)
	if got := u.detailFirstLine(); got != "ズレ: 宣言した通信の許可が反映されていない（three.test）" {
		t.Errorf("detail = %q", got)
	}
}

func TestTUIOverviewFindsTheDriftOfAnEscapedID(t *testing.T) {
	t.Parallel()
	// Arrange: a secret named with a tab; the plan's IDs are escaped for
	// the terminal, the comparison's are not.
	id := "secrets.a\tb"
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: id, Path: id, Value: "ref: op://x", Risk: sbxenv.SeverityMedium}}}
	cmp := &live.Comparison{Checked: []string{"シークレット（名前のみ）"}, Compared: []string{id},
		Drifts: []live.Drift{{Kind: "シークレット", Title: "宣言したシークレットが登録されていない", Declared: "a\tb", Actual: "（登録なし）", Severity: sbxenv.SeverityMedium, ItemID: id}}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; the row read 一致 and the drift sat apart.
	u.wantRe(`^ !  秘密 a\\tb +ref: op://x +（登録なし） *$`)
	if u.has("一致") || u.has("宣言にない実体・その他のズレ") {
		t.Errorf("the drift belongs to its row:\n%s", u.text())
	}
}

func TestTUITitleBarKeepsTheTabsBesideALongName(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	long := strings.Repeat("very-long-sandbox-name-", 4)
	u := targetUI(t, 80, 24, live.Target{Name: long, State: "停止中・宣言ファイル未検出", Sandbox: &live.Sandbox{Name: long}})

	// Assert: regression; the name pushed the tabs off the right edge.
	if l := u.lines()[0]; !strings.Contains(l, "4 ファイル") || !strings.Contains(l, "1 概要") {
		t.Errorf("title bar = %q", l)
	}
	u.clickOn("4 ファイル", 0)
	u.wantView("ファイル")
}

func TestTUIOverviewGroupMatchesOnlyWhenEveryRuleWasCompared(t *testing.T) {
	t.Parallel()
	// Arrange: two runtime rules; sbx confirmed one (the other belongs to
	// a skipped optional group).
	rule := func(host string) sbxenv.Item {
		return sbxenv.Item{ID: "kit:net/network-policy@1/runtime.allow/" + host, Path: "cap:network-policy@1", Value: host, Risk: sbxenv.SeverityLow}
	}
	p := &sbxenv.Plan{Items: []sbxenv.Item{rule("active.test"), rule("skipped.test")}}
	cmp := &live.Comparison{Checked: []string{"通信の許可・拒否"}, Compared: []string{"kit:net/network-policy@1/runtime.allow/active.test"}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; one confirmed rule confirmed the whole row.
	u.wantRe(`^ {4}許可（実行中） +2 件  active\.test、skipped\.test +照合なし *$`)
}

func TestTUIOverviewKeepsTheCursorRowOnATinyScreen(t *testing.T) {
	t.Parallel()
	// Arrange: the body has room for one line.
	u := newUI(t, 80, 8, true)

	// Act / Assert: regression; the overflow hint covered every entry.
	for range 3 {
		u.press("j")
		if u.has("ほか ") {
			t.Fatalf("the hint replaced the cursor row:\n%s", u.text())
		}
		if !strings.Contains(u.frame(), "\x1b[7m") {
			t.Fatalf("the cursor row is on screen:\n%s", u.text())
		}
	}
}

func TestTUIPickerScrollsToTheCursor(t *testing.T) {
	t.Parallel()
	// Arrange: ten targets on a short screen.
	var ts []live.Target
	for i := range 10 {
		ts = append(ts, live.Target{Name: fmt.Sprintf("box-%02d", i), State: "停止中・宣言ファイル未検出", Sandbox: &live.Sandbox{Name: "x"}})
	}
	u := targetUI(t, 80, 12, ts...)

	// Act
	u.press("s")
	for range 9 {
		u.press("j")
	}

	// Assert: regression; the picker always drew from the first target.
	u.want("box-09")
	u.press("enter")
	if !strings.HasPrefix(u.lines()[0], " sbxray  box-09") {
		t.Errorf("title bar = %q", u.lines()[0])
	}
}

func TestTUISearchFromTheOverviewSearchesTheSettings(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 130, 40, true)
	if !strings.Contains(u.footer(), "/ 設定を検索") {
		t.Errorf("footer = %q", u.footer())
	}

	// Act
	u.press("/", "N", "O", "D", "E")

	// Assert: regression; the query was shown but filtered nothing.
	u.wantView("設定")
	u.want("検索: NODE")
	if u.has("lifecycle.initialize") {
		t.Errorf("the settings are filtered:\n%s", u.text())
	}
}

func TestTUIOverviewSecretMatchesByNameOnly(t *testing.T) {
	t.Parallel()
	// Arrange: sbx secret ls confirmed the name; where the value comes
	// from is not something it reports.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "secrets.github", Path: "secrets.github", Value: "ref: op://Private/GitHub/token", Risk: sbxenv.SeverityMedium}}}
	cmp := &live.Comparison{Checked: []string{"シークレット（名前のみ）"}, Compared: []string{"secrets.github"}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; the row read 一致 beside the vault reference.
	u.wantRe(`^ !  秘密 github +ref: op://Private/GitHub/token +名前は一致 *$`)
}

// liveDriftUI shows a plan without findings beside a sandbox that
// publishes a port to every network, which no file declares.
func liveDriftUI(t *testing.T) *ui {
	t.Helper()
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}}
	cmp := &live.Comparison{Checked: []string{"ポート公開"}, Drifts: []live.Drift{{Kind: "ポート公開", Title: "宣言にないポートが公開されている",
		Declared: "（宣言なし）", Actual: "0.0.0.0:8080 -> sandbox:80/tcp", Severity: sbxenv.SeverityHigh}}}
	return targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})
}

func TestTUIOverviewSummaryCountsTheDrifts(t *testing.T) {
	t.Parallel()
	// Act
	u := liveDriftUI(t)

	// Assert: regression; 最重要・高 0 sat above a high drift row.
	u.want("✗ 最重要・高 1")
	u.wantRe(`^ ✗  ポート公開 +（宣言なし） +0\.0\.0\.0:8080`)
}

func TestTUIOverviewSelectedDriftKeepsItsUnderline(t *testing.T) {
	t.Parallel()
	// Arrange: the drift row is the second entry.
	u := liveDriftUI(t)

	// Act
	u.press("j")

	// Assert: regression; reverse video replaced the bold underline.
	a := attrsAt(u.rawLine("0.0.0.0:8080"), "0.0.0.0:8080")
	if !a["7"] || !a["1"] || !a["4"] {
		t.Errorf("selected drift value attrs = %v", a)
	}
}

func TestTUIRiskSearchForTheSectionWordFindsTheDrifts(t *testing.T) {
	t.Parallel()
	// Arrange
	u := liveDriftUI(t)

	// Act
	u.press("2", "/", "ズ", "レ", "enter")

	// Assert: regression; the section's own word found nothing.
	u.want("sbx とのズレ（1 件）", "ポート公開: 宣言にないポート")
}

func TestTUIOverviewListsASecondDriftOfTheSameItem(t *testing.T) {
	t.Parallel()
	// Arrange: one MCP server, registered as another kind and waiting for
	// authorization.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "mcp.servers[docs]", Path: "mcp.servers[]", Value: "command: docs-mcp"}}}
	mcp := func(title, actual string, sev sbxenv.Severity) live.Drift {
		return live.Drift{Kind: "MCP サーバー", Title: title, Declared: "command: docs-mcp", Actual: actual, Severity: sev, ItemID: "mcp.servers[docs]"}
	}
	cmp := &live.Comparison{Checked: []string{"MCP サーバー"}, Compared: []string{"mcp.servers[docs]"}, Drifts: []live.Drift{
		mcp("同じ名前の MCP サーバーが、宣言と違う種類で登録されている", "種類: remote（sse）", sbxenv.SeverityMedium),
		mcp("MCP サーバーは登録済みだが使える状態ではない", "状態: needs authorization", sbxenv.SeverityLow),
	}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; the second drift was marked used but shown nowhere.
	u.wantRe(`^ !  MCP docs +.* +種類: remote（sse） *$`)
	u.want("宣言にない実体・その他のズレ", "状態: needs authorization")
}

func TestTUIOverviewSaysASkippedCommandDoesNotRun(t *testing.T) {
	t.Parallel()
	// Arrange: --skip-host-commands marks the hook [SKIP].
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "lifecycle.initialize[0]", Path: "lifecycle.initialize[0]", Value: "echo hi", Risk: sbxenv.SeverityLow,
		Badges: []string{sbxenv.BadgeHost, sbxenv.BadgeSkip}}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; the row read as a command that runs on the PC.
	u.wantRe(`^ {4}PC 上で実行 +echo hi（実行されない） *$`)
}

func TestTUIContextLineOfAParseErrorTarget(t *testing.T) {
	t.Parallel()
	// Act
	u := targetUI(t, 120, 30, live.Target{Name: "broken", State: "解析エラー", Files: []string{"~/x/sbxenv.yaml"}, Err: "設定ファイルを解析できませんでした: line 3"})

	// Assert: regression; it read as a running sandbox without files.
	if l := u.lines()[1]; !strings.Contains(l, "設定ファイルを解析できませんでした") || strings.Contains(l, "稼働中") {
		t.Errorf("context line = %q", l)
	}
}

func TestTUIContextLineDoesNotCallAStoppedSandboxRunning(t *testing.T) {
	t.Parallel()
	// Arrange
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}}

	// Act
	u := targetUI(t, 120, 30, live.Target{Name: "web", State: "停止中", Plan: p, Sandbox: &live.Sandbox{Name: "web", Status: "stopped"}, Comparison: &live.Comparison{}})

	// Assert: regression; the context line said 稼働中.
	if l := u.lines()[1]; strings.Contains(l, "稼働中") || !strings.Contains(l, "sbx の sandbox") {
		t.Errorf("context line = %q", l)
	}
}

func TestTUIFrameFitsAShortTerminal(t *testing.T) {
	t.Parallel()
	// Arrange: two targets (a target line) and a live error (a state line).
	p, _ := newPlan(t, sbxenv.RedactHome)
	targets := []live.Target{{Name: "a", State: "宣言", Plan: p, Err: "sbx コマンドが見つかりません"}, {Name: "b", State: "宣言", Plan: p}}
	for _, h := range []int{8, 9, 10} {
		for _, view := range []string{"1", "2", "3", "4", "s", "?"} {
			m := tui.NewTargets(targets, "sbx コマンドが見つかりません", true)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: h})
			u := &ui{t: t, m: m}
			u.press(view)

			// Assert: regression; the frame was taller than the terminal.
			if n := len(u.lines()); n != h {
				t.Errorf("height %d, view %s: %d lines\n%s", h, view, n, u.text())
			}
			if !strings.Contains(u.lines()[h-1], "q 終了") {
				t.Errorf("height %d, view %s: the footer is on the last line:\n%s", h, view, u.text())
			}
		}
	}
}

func TestTUIOverviewKeepsASkippedRuleOffTheActiveRow(t *testing.T) {
	t.Parallel()
	// Arrange: one active runtime allow, and one from an optional group
	// whose condition fails.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "kit:a/network-policy@1/runtime.allow/on.test", Path: "cap:network-policy@1", Value: "on.test"},
		{ID: "kit:b/network-policy@1/runtime.allow/off.test", Path: "cap:network-policy@1", Value: "off.test", Badges: []string{sbxenv.BadgeSkip}},
	}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; both hosts read as allowed at runtime.
	u.wantRe(`^ {4}許可（実行中） +1 件  on\.test *$`, `^ {4}許可（実行中） +1 件  off\.test（実行されない） *$`)
}

func TestTUIOverviewDoesNotCountRisksWithoutADeclaration(t *testing.T) {
	t.Parallel()
	// Act: a sandbox no file declares.
	u := targetUI(t, 120, 30, live.Target{Name: "orphan", State: "停止中・宣言ファイル未検出", Sandbox: &live.Sandbox{Name: "orphan",
		Ports: []live.Port{{HostIP: "0.0.0.0", HostPort: 8080, SandboxPort: 80, Protocol: "tcp"}}}})

	// Assert: regression; 最重要・高 0 read as nothing found.
	u.want("宣言がないため、危険度は評価していません")
	if u.has("最重要・高 0") {
		t.Errorf("no count without an assessment:\n%s", u.text())
	}
}

func TestTUIEveryViewSaysWhereItsDataComesFrom(t *testing.T) {
	t.Parallel()
	for _, view := range []string{"2", "3", "4"} {
		// Act
		u := newUI(t, 130, 40, true).press(view)

		// Assert: regression; the filter line replaced the notice.
		if l := u.lines()[1]; !strings.Contains(l, "静的解析") {
			t.Errorf("view %s: context line = %q", view, l)
		}
	}
	u := newTargetsUI(t, 160, 50).press("2")
	if l := u.lines()[2]; !strings.Contains(l, "sbx と照合") {
		t.Errorf("a live target's context line = %q", l)
	}
}

func TestTUIOverviewEnterIgnoresACursorRowScrolledAway(t *testing.T) {
	t.Parallel()
	// Arrange: one entry, then many comparison notes, on a short screen.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}}
	cmp := &live.Comparison{}
	for i := range 20 {
		cmp.Skipped = append(cmp.Skipped, fmt.Sprintf("比較していない項目 %02d", i))
	}
	u := targetUI(t, 100, 14, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Act: scroll the notes until the entry has left the screen.
	for range 30 {
		u.press("j")
	}
	u.want("比較していない項目 19")
	if u.has("エージェント") {
		t.Fatalf("the entry should have scrolled away:\n%s", u.text())
	}
	u.press("enter")

	// Assert: regression; Enter opened the unseen entry.
	u.wantView("概要")
	if u.has(backLink) {
		t.Errorf("Enter opened a row that is not on screen:\n%s", u.text())
	}

	// Act / Assert: k scrolls back toward the entry.
	for range 30 {
		u.press("k")
	}
	u.want("エージェント")
}

func TestTUIOverviewEnterOpensNothingWhenNoRowFits(t *testing.T) {
	t.Parallel()
	// Arrange: two targets and a live error leave the pane no body row.
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "a", State: "宣言", Plan: p, Err: "sbx コマンドが見つかりません"}, {Name: "b", State: "宣言", Plan: p}}, "", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	u := &ui{t: t, m: m}

	// Act
	u.press("j", "j", "enter")

	// Assert: regression; Enter opened a setting no frame had shown.
	if u.has(backLink) {
		t.Errorf("Enter opened an unseen row:\n%s", u.text())
	}
}

func TestTUISourceLabelSurvivesALongFilterList(t *testing.T) {
	t.Parallel()
	// Arrange: twelve Kit sources make the filter switches overflow 80 columns.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}}
	for i := range 12 {
		p.Meta.Sources = append(p.Meta.Sources, sbxenv.Source{ID: fmt.Sprintf("K%d", i+1), Kind: sbxenv.SourceKit, Path: fmt.Sprintf("kits/k%d", i+1)})
	}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.press("3")

	// Assert: regression; the label came after the switches and was cut.
	if l := u.lines()[1]; !strings.Contains(l, "静的解析") {
		t.Errorf("context line = %q", l)
	}
}
