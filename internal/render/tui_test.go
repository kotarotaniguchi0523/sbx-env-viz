package render_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
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
	headRe := regexp.MustCompile(`^ (\S[^（]*)（(\d+) 件） *$`) // "sbx とのズレ" holds a space
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
	u.wantRe(`^ {4}許可（実行中） +1 件  one\.test +ホストは一致 *$`,
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

	// Assert: regression; the name pushed the tabs off the right edge. A
	// lone target has no position to name.
	if l := u.lines()[0]; !strings.Contains(l, "4 ファイル") || !strings.Contains(l, "1 概要") || strings.Contains(l, "1/1") {
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

	// Assert: regression; one confirmed rule confirmed the whole row. The
	// confirmed rule and the other get rows of their own, so neither the
	// check made nor the one missing is lost.
	u.wantRe(`^ {4}許可（実行中） +1 件  active\.test +ホストは一致 *$`, `^ {4}許可（実行中） +1 件  skipped\.test +照合なし *$`)
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
	u.wantRe(`^ {4}PC 上で実行 +（実行されない）echo hi *$`)
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
	u.wantRe(`^ {4}許可（実行中） +1 件  on\.test *$`, `^ {4}許可（実行中） +（実行されない）1 件  off\.test *$`)
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

func TestTUIOverviewDrawsAnEntryOnAPaneTooShortForItsHeader(t *testing.T) {
	t.Parallel()
	// Arrange: two targets and a live error leave the pane three rows,
	// which the summary, spacer and column header would fill.
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "a", State: "宣言", Plan: p, Err: "sbx コマンドが見つかりません"}, {Name: "b", State: "宣言", Plan: p}}, "", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	u := &ui{t: t, m: m}

	// Act
	u.press("j", "j")

	// Assert: regression; no entry was drawn, yet j moved and Enter
	// opened one. The header gives way, the cursor row shows, and Enter
	// opens what is shown.
	if !strings.Contains(u.frame(), "\x1b[7m") || len(u.lines()) != 8 {
		t.Fatalf("the cursor row is on screen:\n%s", u.text())
	}
	u.press("enter")
	u.want(backLink)
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

func TestTUIDetailShowsTextBeforeAHintOnATinyPane(t *testing.T) {
	t.Parallel()
	// Arrange: an 80-column detail on 8 rows with a target line and a
	// state line leaves the body a row or two.
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "a", State: "宣言", Plan: p, Err: "sbx コマンドが見つかりません"}, {Name: "b", State: "宣言", Plan: p}}, "", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	u := &ui{t: t, m: m}

	// Act
	u.press("3", "enter")

	// Assert: regression; the hint covered the only body row, or the
	// slice before it panicked.
	if u.has("ほか ") || !u.has("kit:net/network-policy@1/runtime.allow/api.example.com") {
		t.Errorf("the detail's first line gave way to the hint:\n%s", u.text())
	}
	if n := len(u.lines()); n != 8 {
		t.Errorf("%d lines", n)
	}
}

func TestTUIOverviewKeepsTheSkipMarkBesideALongCommand(t *testing.T) {
	t.Parallel()
	// Arrange: a skipped hook longer than the 80-column value column.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "lifecycle.initialize[0]", Path: "lifecycle.initialize[0]", Risk: sbxenv.SeverityLow,
		Value: "./scripts/bootstrap.sh --with-a-very-long-list-of-flags --and-more-flags --until-it-overflows", Badges: []string{sbxenv.BadgeHost, sbxenv.BadgeSkip}}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; the trailing mark was cut with the command.
	u.wantRe(`^ {4}PC 上で実行 +（実行されない）\./scripts/bootstrap\.sh`)
}

func TestTUIOverviewMCPMatchesByNameAndKindOnly(t *testing.T) {
	t.Parallel()
	// Arrange: sbx mcp ls confirmed the name and the kind, not the URL.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "mcp.servers[docs]", Path: "mcp.servers[]", Value: "url: https://mcp.example.com/sse"}}}
	cmp := &live.Comparison{Checked: []string{"MCP サーバー"}, Compared: []string{"mcp.servers[docs]"}}

	// Act
	u := targetUI(t, 120, 30, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; the row read 一致 though the URL was not compared.
	u.wantRe(`^ {4}MCP docs +リモート（URL に接続） +名前と種類は一致 *$`)
}

func TestTUIPickerShowsTheCursorRowOnAShortPane(t *testing.T) {
	t.Parallel()
	// Arrange: two targets and a live error leave the picker three rows.
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "alpha", State: "宣言", Plan: p, Err: "sbx コマンドが見つかりません"}, {Name: "bravo", State: "宣言", Plan: p}}, "", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	u := &ui{t: t, m: m}

	// Act
	u.press("s", "j")

	// Assert: regression; the title and headings took every row.
	if !strings.Contains(u.frame(), "\x1b[7m") || !strings.Contains(u.text(), "    bravo") {
		t.Errorf("the selected target is on screen:\n%s", u.text())
	}
}

func TestTUIPickerTellsSimilarLongNamesApart(t *testing.T) {
	t.Parallel()
	// Arrange
	u := targetUI(t, 160, 30,
		live.Target{Name: "integration-worker-staging", State: "停止中・宣言ファイル未検出", Sandbox: &live.Sandbox{Name: "x"}},
		live.Target{Name: "integration-worker-prod", State: "停止中・宣言ファイル未検出", Sandbox: &live.Sandbox{Name: "y"}})

	// Act
	u.press("s")

	// Assert: regression; both names were cut to the same 18 cells.
	u.want("    integration-worker-staging", "    integration-worker-prod ")
}

func TestTUISummaryOfAParseErrorTarget(t *testing.T) {
	t.Parallel()
	// Act
	u := targetUI(t, 120, 30, live.Target{Name: "broken", State: "解析エラー", Files: []string{"~/x/sbxenv.yaml"}, Err: "設定ファイルを解析できませんでした: line 3"})

	// Assert: regression; it said no declaration exists.
	u.want("設定ファイルを解析できないため、危険度は評価していません")
}

func TestTUIOverviewDoesNotReportAFilterOrSearchItIgnores(t *testing.T) {
	t.Parallel()
	// Arrange: pick layer L2 and search on 設定, then return.
	u := newUI(t, 120, 40, true)
	u.press("3", "f", "f", "/", "N", "enter", "1")

	// Assert: regression; the status line named a filter the manifest
	// does not apply.
	if u.has("絞り込み（") || u.has("検索: N") {
		t.Errorf("Overview reports a filter or search:\n%s", u.text())
	}
	u.press("3")
	u.want("絞り込み（", "検索: N")
}

func TestTUIOverviewKeepsATabAndALiteralBackslashTApart(t *testing.T) {
	t.Parallel()
	// Arrange: two folders whose IDs escape to the same text; only the
	// second one (the literal \t) is missing.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "additionalWorkspaces[/a\tb]", Path: "additionalWorkspaces[]", Value: "/w/one"},
		{ID: `additionalWorkspaces[/a\tb]`, Path: "additionalWorkspaces[]", Value: "/w/two"},
	}}
	cmp := &live.Comparison{Checked: []string{"ワークスペース・マウント"}, Compared: []string{p.Items[0].ID, p.Items[1].ID},
		Drifts: []live.Drift{{Kind: "ワークスペース・マウント", Title: "宣言したフォルダがマウントされていない", Declared: "/w/two", Actual: "（マウントされていない）",
			Severity: sbxenv.SeverityMedium, ItemID: p.Items[1].ID}}}

	// Act
	u := targetUI(t, 120, 30, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: regression; the first row took the second one's drift.
	u.wantRe(`^ {4}追加フォルダ +/w/one（読み書き） +パスは一致 *$`, `^ !  追加フォルダ +/w/two（読み書き） +（マウントされていない） *$`)
}

func TestTUIOverviewKeepsTheActualColumnOnANarrowScreen(t *testing.T) {
	t.Parallel()
	// Arrange
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "sandboxOptions.template", Path: "sandboxOptions.template", Value: "ghcr.io/example/x:dev"}}}

	// Act
	u := targetUI(t, 32, 20, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: &live.Comparison{}})

	// Assert: regression; label, gaps and the value pushed it off screen.
	u.wantRe(`照合なし *$`)
}

func TestTUIPickerKeepsTheEndOfALongName(t *testing.T) {
	t.Parallel()
	// Arrange: at 80 columns the name column stops short of these names.
	u := targetUI(t, 80, 30,
		live.Target{Name: "integration-worker-for-the-billing-service-staging", State: "停止中", Sandbox: &live.Sandbox{Name: "x"}},
		live.Target{Name: "integration-worker-for-the-billing-service-prod", State: "停止中", Sandbox: &live.Sandbox{Name: "y"}})

	// Act
	u.press("s")

	// Assert: regression; both rows were cut to the same prefix.
	u.want("staging  ", "prod  ")
}

func TestTUITabClickEndsTypingASearch(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newUI(t, 120, 40, true)
	u.press("3", "/", "x")

	// Act
	u.clickOn("1 概要", 0)
	u.press("j")

	// Assert: regression; j was typed into an invisible search.
	if u.has("検索: xj") {
		t.Errorf("keys still edit the search:\n%s", u.text())
	}
}

func TestTUIRiskKFromTheOnlyResultStaysOnIt(t *testing.T) {
	t.Parallel()
	// Arrange: search Risk down to one finding.
	u := newUI(t, 160, 50, true).press("2", "/")
	for _, r := range "lifecycle.initialize" {
		u.press(string(r))
	}
	u.press("enter")
	first := u.detailFirstLine()

	// Act
	u.press("k")

	// Assert: regression; k selected the section header and blanked the detail.
	if got := u.detailFirstLine(); got == "" || got != first {
		t.Errorf("detail after k = %q, want %q\n%s", got, first, u.text())
	}
}

func TestTUIFOnTheOverviewChangesNoFilter(t *testing.T) {
	t.Parallel()
	// Act
	u := newUI(t, 130, 40, true).press("f", "2")

	// Assert: regression; Risk opened filtered to L1, unannounced.
	if u.has("絞り込み（") {
		t.Errorf("f on the Overview set a filter:\n%s", u.text())
	}
}

func TestTUIStateLinePutsTheSearchAndTheTargetErrorFirst(t *testing.T) {
	t.Parallel()
	// Arrange: a long standing notice and a target whose files failed to parse.
	note := "sbx コマンドが見つかりません。Docker Sandboxes をインストールすると、稼働中の sandbox と照合できます（今は設定ファイルだけを解析しています）"
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "web", State: "宣言", Plan: p, Err: "設定ファイルを解析できませんでした: line 3"}}, note, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	u := &ui{t: t, m: m}

	// Act
	u.press("2", "/", "x")

	// Assert: regression; the notice filled the line and cut both.
	u.want("検索: x_", "line 3")
}

func TestTUIOverviewDoesNotCallALocalZipKitRemote(t *testing.T) {
	t.Parallel()
	// Arrange
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "kits[0]", Path: "kits[]", Value: "./dist/bundle.zip",
		Fields: []sbxenv.Field{{Key: "kind", Value: "zip"}, {Key: "handle", Value: "bundle"}, {Key: "status", Value: "remote"}}}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; a local archive read as remote.
	u.wantRe(`^ {4}bundle +ZIP（展開していない。「4 ファイル」で確認） *$`)
}

func TestTUITitleBarKeepsTheStateOfALongNamedSandbox(t *testing.T) {
	t.Parallel()
	// Arrange: one paired target; nothing else on the Overview says it is stopped.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}}
	long := "integration-worker-for-the-billing-service-staging"

	// Act
	u := targetUI(t, 80, 24, live.Target{Name: long, State: "停止中", Plan: p, Sandbox: &live.Sandbox{Name: long, Status: "stopped"}, Comparison: &live.Comparison{}})

	// Assert: regression; the name pushed 停止中 out of the title bar.
	if l := u.lines()[0]; !strings.Contains(l, "停止中") || !strings.Contains(l, "4 ファイル") {
		t.Errorf("title bar = %q", l)
	}
}

func TestTUIOverviewReadsARulesPhaseAndDecisionFromItsSegment(t *testing.T) {
	t.Parallel()
	// Arrange: a host that contains "install", and a kit named egress.deny.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "kit:net/network-policy@1/runtime.allow/install.example.com", Path: "cap:network-policy@1", Kit: "net", Value: "runtime allow install.example.com"},
		{ID: "kit:egress.deny/network-policy@1/runtime.allow/api.example.com", Path: "cap:network-policy@1", Kit: "egress.deny", Value: "runtime allow api.example.com"},
	}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; substrings of the whole ID decided both.
	u.wantRe(`^ {4}許可（実行中） +2 件  install\.example\.com、api\.example\.com *$`)
	if u.has("準備時") || u.has("禁止") {
		t.Errorf("a runtime allow was labelled otherwise:\n%s", u.text())
	}
}

func TestTUIStateLineShowsALiveErrorThatIsAlsoTheNotice(t *testing.T) {
	t.Parallel()
	// Arrange: --live with one target passes its error as the notice too.
	const msg = "sbx コマンドが見つかりません。Docker Sandboxes をインストールすると、稼働中の sandbox と照合できます"
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "web", State: "宣言", Plan: p, Err: msg}}, msg, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	u := &ui{t: t, m: m}

	// Act
	u.press("2", "f", "/", "x")

	// Assert: regression; the shared message went last and was cut.
	if l := u.lines()[2]; !strings.HasPrefix(strings.TrimSpace(l), "検索: x_  sbx コマンドが見つかりません") {
		t.Errorf("state line = %q", l)
	}
}

func TestTUIFooterWhileTypingASearch(t *testing.T) {
	t.Parallel()
	// Act
	u := newUI(t, 120, 40, true).press("3", "/")

	// Assert: regression; it offered j/k, Enter 詳細 and q 終了, which type.
	f := u.footer()
	if !strings.Contains(f, "enter 確定") || !strings.Contains(f, "esc 取り消し") || strings.Contains(f, "q 終了") || strings.Contains(f, "j/k") {
		t.Errorf("footer = %q", f)
	}
}

func TestTUIStateLineShortensALongQuery(t *testing.T) {
	t.Parallel()
	// Arrange
	const msg = "sbx コマンドが見つかりません"
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.NewTargets([]live.Target{{Name: "web", State: "宣言", Plan: p, Err: msg}}, msg, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	u := &ui{t: t, m: m}

	// Act
	u.press("3", "/")
	for _, r := range "kit:net/network-policy@1/runtime.allow/api.example.com/and/more/text" {
		u.press(string(r))
	}

	// Assert: regression; the query pushed the live error off the line.
	if l := u.lines()[2]; !strings.Contains(l, msg) || !strings.Contains(l, "more/text_") {
		t.Errorf("state line = %q", l)
	}
}

func TestTUIOverviewDoesNotShowAnItemSuffixAsAHost(t *testing.T) {
	t.Parallel()
	// Arrange: two @2 allows for one host (GET /a and POST /b).
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "kit:k/network-policy@2/runtime.allow/api.example.com", Path: "cap:network-policy@2", Kit: "k", Value: "runtime allow api.example.com [GET] /a"},
		{ID: "kit:k/network-policy@2/runtime.allow/api.example.com#2", Path: "cap:network-policy@2", Kit: "k", Value: "runtime allow api.example.com [POST] /b"},
	}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; the second read api.example.com#2.
	u.wantRe(`^ {4}許可（実行中） +2 件  api\.example\.com、api\.example\.com *$`)
}

func TestTUISettingsKeepAKeyThatHoldsEquals(t *testing.T) {
	t.Parallel()
	// Arrange: a mount target with " = " in it.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "additionalWorkspaces[/work/a = b]", Path: "additionalWorkspaces[]", Value: "~/data"}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.press("3")

	// Assert: regression; the key was cut at its own " = ".
	u.wantRe(`paces\[/work/a = b\] +~/data`)
}

func TestTUIContextLineCountsOnlyExpandedKits(t *testing.T) {
	t.Parallel()
	// Arrange: one kit read, one that failed to expand.
	p := &sbxenv.Plan{Meta: sbxenv.Meta{Sources: []sbxenv.Source{
		{ID: "L1", Kind: sbxenv.SourceEnv, Path: "sbxenv.yaml", Expanded: true},
		{ID: "K1", Kind: sbxenv.SourceKit, Path: "kits/a", Expanded: true},
		{ID: "K2", Kind: sbxenv.SourceKit, Path: "kits/broken"},
	}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; the broken kit counted as analysed.
	u.want("宣言 1 ファイル・Kit 1 を静的に解析")
}

func TestTUILiveOnlyOverviewHidesPortsAndSecretsWhenRedacted(t *testing.T) {
	t.Parallel()
	// Arrange: discovery marks a sandbox no file declares as redacted.
	sb := &live.Sandbox{Name: "<redacted>-1", Status: "stopped", Redacted: true, SecretsKnown: true, Secrets: []string{"acme-token"},
		Ports: []live.Port{{HostIP: "127.0.0.1", HostPort: 4000, SandboxPort: 80, Protocol: "tcp"}}}

	// Act
	u := targetUI(t, 120, 30, live.Target{Name: sb.Name, State: "停止中・宣言ファイル未検出", Sandbox: sb})

	// Assert: regression; the listener and secret names were printed raw.
	u.want("ポート公開: <redacted>", "シークレット（名前のみ）: <redacted>（1 件）")
	if u.has("4000") || u.has("acme-token") {
		t.Errorf("a live value leaked:\n%s", u.text())
	}
}

func TestTUITabsShrinkBeforeTheyAreCut(t *testing.T) {
	t.Parallel()
	// Arrange / Act: 45 columns cannot hold the four tabs with counts.
	u := newUI(t, 45, 24, true).press("4")

	// Assert: regression; 4 ファイル, the active tab, was cut off.
	l := u.lines()[0]
	if !strings.Contains(l, "4 ファイル") || !strings.Contains(l, "1") || !strings.Contains(l, "3") {
		t.Errorf("title bar = %q", l)
	}
	// A one-cell tab is still a click target.
	x, y := u.find("1   2", 0)
	u.click(x, y)
	u.wantView("概要")
}

func TestTUITabsMakeRoomForTheTargetPosition(t *testing.T) {
	t.Parallel()
	// Arrange: three targets at 52 columns; the target line does not fit,
	// so the title bar names the position.
	p, _ := newPlan(t, sbxenv.RedactHome)
	var ts []live.Target
	for _, n := range []string{"alpha", "bravo", "charlie"} {
		ts = append(ts, live.Target{Name: n, State: "宣言", Plan: p})
	}

	// Act
	u := targetUI(t, 52, 24, ts...).press("4")

	// Assert: regression; " 1/3" clipped the last tab.
	if l := u.lines()[0]; !strings.Contains(l, "1/3") || !strings.Contains(l, "4 ファイル") {
		t.Errorf("title bar = %q", l)
	}
}

// --- counts, hints and edges ---------------------------------------------------

// cursorLine is the plain text of the reverse-video cursor row, or "".
func (u *ui) cursorLine() string {
	for _, l := range strings.Split(u.frame(), "\n") {
		if strings.Contains(l, "\x1b[7m") {
			return strings.TrimSpace(ansi.Strip(l))
		}
	}
	return ""
}

// overviewBody returns the Overview's canvas rows, between the column
// header and the footer, and the index of the cursor row among them (-1
// when it is not drawn).
func (u *ui) overviewBody() (rows []string, cur int) {
	raw := strings.Split(u.frame(), "\n")
	head := len(raw)
	for y, l := range raw {
		if viewHeads[0].re.MatchString(ansi.Strip(l)) {
			head = y
			break
		}
	}
	cur = -1
	for y := head + 1; y < len(raw)-1; y++ {
		if strings.Contains(raw[y], "\x1b[7m") {
			cur = len(rows)
		}
		rows = append(rows, ansi.Strip(raw[y]))
	}
	return rows, cur
}

var hintRe = regexp.MustCompile(`ほか (\d+) 行（j で(下へ|スクロール)）`)

// hint returns the count of the overflow hint on screen, and its row.
func (u *ui) hint() (n, y int, ok bool) {
	for y, l := range u.lines() {
		if m := hintRe.FindStringSubmatch(l); m != nil {
			n, _ = strconv.Atoi(m[1])
			return n, y, true
		}
	}
	return 0, 0, false
}

func TestTUIOverviewHeadsOnlyTheSectionsItDraws(t *testing.T) {
	t.Parallel()
	sections := []string{"基本", "Kit", "フォルダの共有", "ポート公開", "通信", "資格情報", "コマンドの実行"}
	for _, tc := range []struct {
		name     string
		items    []sbxenv.Item
		sections []string
		want     []string
	}{
		{"nothing declared", nil, nil, []string{`^ {4}（表示できる設定がありません） *$`}},
		{"an agent", []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}}, []string{"基本"}, nil},
		{"memory and CPUs", []sbxenv.Item{{ID: "sandboxOptions.memory", Path: "sandboxOptions.memory", Value: "12g"}, {ID: "sandboxOptions.cpus", Path: "sandboxOptions.cpus", Value: "4"}},
			[]string{"基本"}, []string{`^ {4}リソース +メモリ 12g、CPU 4 *$`}},
		{"a kit's mount alone", []sbxenv.Item{{ID: "mounts./cache", Path: "mounts./cache", Value: "/work/cache"}},
			[]string{"フォルダの共有"}, []string{`^ {4}Kit 用フォルダ +/work/cache *$`}},
		{"a kit's host folder alone", []sbxenv.Item{{ID: "kit:k/host-mount@1/data", Path: "cap:host-mount@1", Value: "/work/data"}},
			[]string{"フォルダの共有"}, []string{`^ {4}Kit の共有 +/work/data *$`}},
		{"a command inside the sandbox alone", []sbxenv.Item{{ID: "kit:k/lifecycle@1/postCreate", Path: "cap:lifecycle@1", Value: "npm ci"}},
			[]string{"コマンドの実行"}, []string{`^ {4}sandbox 内で実行 +npm ci *$`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			m := tui.New(&sbxenv.Plan{Items: tc.items}, true)
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			u := &ui{t: t, m: m}

			// Assert: a section is headed only when it has a row, and an
			// empty manifest says so instead of showing a bare heading.
			for _, s := range sections {
				if got, want := u.hasLine(s), slices.Contains(tc.sections, s); got != want {
					t.Errorf("heading %s shown = %v, want %v\n%s", s, got, want, u.text())
				}
			}
			if got, want := u.has("（表示できる設定がありません）"), len(tc.items) == 0; got != want {
				t.Errorf("empty-manifest note shown = %v, want %v\n%s", got, want, u.text())
			}
			u.wantRe(tc.want...)
		})
	}
}

func TestTUIFooterOffersTheKeysOfEachView(t *testing.T) {
	t.Parallel()
	// The footer offers g only where it groups and f only where it filters.
	for _, tc := range []struct{ key, want string }{
		{"1", " j/k 移動   enter 詳細   / 設定を検索   ? ヘルプ   q 終了"},
		{"2", " j/k 移動   enter 詳細   / 検索   f 絞り込み   ? ヘルプ   q 終了"},
		{"3", " j/k 移動   enter 詳細   / 検索   g グループ   f 絞り込み   ? ヘルプ   q 終了"},
		{"4", " j/k 移動   enter 詳細   / 検索   f 絞り込み   ? ヘルプ   q 終了"},
	} {
		// Act
		u := newUI(t, 140, 30, true).press(tc.key)

		// Assert
		if got := strings.TrimRight(u.footer(), " "); got != tc.want {
			t.Errorf("view %s footer = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestTUIStateLineKeepsTheLiveNoteBesideATargetError(t *testing.T) {
	t.Parallel()
	const note = "sbx policy ls が失敗しました"
	for _, tc := range []struct {
		name, err string
		want      []string
	}{
		{"no target error", "", []string{note}},
		{"a different target error", "sbx ls が失敗しました", []string{"sbx ls が失敗しました", note}},
		{"the same message", note, []string{note}},
	} {
		// Arrange / Act
		p, _ := newPlan(t, sbxenv.RedactHome)
		m := tui.NewTargets([]live.Target{{Name: "web", State: "宣言", Plan: p, Err: tc.err}}, note, true)
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		u := &ui{t: t, m: m}

		// Assert: neither message hides the other, and one shared message
		// is shown once.
		l := u.lines()[2]
		for _, w := range tc.want {
			if strings.Count(l, w) != 1 {
				t.Errorf("%s: state line = %q, want %q once", tc.name, l, w)
			}
		}
	}
}

func TestTUITargetBarUnderlinesTheShownTarget(t *testing.T) {
	t.Parallel()
	// Arrange
	u := newTargetsUI(t, 160, 50)

	// Act / Assert: the active target is the underlined one, before and
	// after switching.
	for _, tc := range []struct{ key, on, off string }{{"", "web", "orphan"}, {">", "orphan", "web"}} {
		if tc.key != "" {
			u.press(tc.key)
		}
		raw := u.lines2raw(1)
		if !attrsAt(raw, tc.on)["4"] || attrsAt(raw, tc.off)["4"] {
			t.Errorf("after %q only %s is underlined: %q", tc.key, tc.on, raw)
		}
	}
}

func TestTUITitleBarOfASinglePlanNamesItAndCountsZero(t *testing.T) {
	t.Parallel()
	// Arrange / Act: a declared name after another setting, and nothing risky.
	m := tui.New(&sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude", Risk: sbxenv.SeverityInfo},
		{ID: "name", Path: "name", Value: "web", Risk: sbxenv.SeverityInfo}}}, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: the title names the sandbox, and an empty tab still says 0.
	l := u.lines()[0]
	if !strings.HasPrefix(l, " sbxray  web ") || !strings.Contains(l, "2 リスク 0") || !strings.Contains(l, "4 ファイル 0") {
		t.Errorf("title bar = %q", l)
	}
}

func TestTUIOverviewJAndKMoveTheCursorAndUndoAScrollPastTheEnd(t *testing.T) {
	t.Parallel()
	// Arrange: comparison notes below the last entry, on a short screen.
	p, _ := newPlan(t, sbxenv.RedactHome)
	cmp := &live.Comparison{}
	for i := range 12 {
		cmp.Skipped = append(cmp.Skipped, fmt.Sprintf("比較していない項目 %02d", i))
	}
	u := targetUI(t, 80, 16, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})
	first := u.cursorLine()

	// Act / Assert: j and k step between entries, back to the first one.
	if u.press("j"); !strings.HasPrefix(u.cursorLine(), "テンプレート") {
		t.Fatalf("j moves to the second entry: %q", u.cursorLine())
	}
	if u.press("k"); u.cursorLine() != first {
		t.Fatalf("k returns to the first entry: %q, want %q", u.cursorLine(), first)
	}

	// Act: j well past the last entry, then k once.
	for range 40 {
		u.press("j")
	}
	last := u.cursorLine()
	before, _ := u.overviewBody()
	u.press("k")

	// Assert: k first scrolls back through the notes it passed; the
	// cursor stays on the last entry.
	if after, _ := u.overviewBody(); u.cursorLine() != last || slices.Equal(after, before) {
		t.Errorf("k after j past the end scrolls back and keeps %q; cursor %q\n%s", last, u.cursorLine(), u.text())
	}
}

func TestTUIOverviewJUndoesAScrollAboveTheFirstEntry(t *testing.T) {
	t.Parallel()
	// Arrange: a sandbox not yet created has two lines above its first
	// entry. Walk down and back so those lines are scrolled away.
	p, _ := newPlan(t, sbxenv.RedactHome)
	u := targetUI(t, 80, 16, live.Target{Name: "api", State: "宣言のみ（未作成）", Plan: p})
	first := u.cursorLine()
	for _, k := range []string{"j", "k"} {
		for range 40 {
			u.press(k)
		}
	}
	if u.cursorLine() != first || !u.hasLine("sbx の sandbox") {
		t.Fatalf("k past the first entry shows the lines above it:\n%s", u.text())
	}
	before, _ := u.overviewBody()

	// Act
	u.press("j")

	// Assert: j first scrolls back through the lines k passed; the cursor
	// stays on the first entry.
	if after, _ := u.overviewBody(); u.cursorLine() != first || slices.Equal(after, before) {
		t.Errorf("j after k past the first entry scrolls back and keeps %q; cursor %q\n%s", first, u.cursorLine(), u.text())
	}
}

func TestTUIOverviewKeepsALineOfContextAroundTheCursor(t *testing.T) {
	t.Parallel()
	// Arrange: a manifest taller than the pane, two sections and a run of
	// rows in one, with notes after its last entry.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "agent", Path: "agent", Value: "claude"}, {ID: "sandboxOptions.template", Path: "sandboxOptions.template", Value: "x:dev"}}}
	for i := range 12 {
		id := fmt.Sprintf("secrets.s%02d", i)
		p.Items = append(p.Items, sbxenv.Item{ID: id, Path: id, Value: "ref: op://x"})
	}
	cmp := &live.Comparison{Skipped: []string{"比較していない項目 A", "比較していない項目 B"}}
	u := targetUI(t, 80, 16, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Act / Assert: walking down and back up, the cursor always has a line
	// above and below it, and the canvas scrolls only when the cursor
	// reaches that margin (to the last-but-one row going down, the second
	// row going up).
	check := func(key string, edge func(n int) int) {
		for range 30 {
			prev, _ := u.overviewBody()
			was := u.cursorLine()
			u.press(key)
			if u.cursorLine() == was {
				return
			}
			rows, cur := u.overviewBody()
			if cur < 1 || cur > len(rows)-2 {
				t.Fatalf("%s: cursor row %d of %d has no context line:\n%s", key, cur, len(rows), u.text())
			}
			if !slices.Equal(rows, prev) && cur != edge(len(rows)) {
				t.Fatalf("%s: the canvas scrolled with the cursor at row %d of %d:\n%s", key, cur, len(rows), u.text())
			}
		}
		t.Fatalf("%s never reached the end", key)
	}
	check("j", func(n int) int { return n - 2 })
	u.press("k") // undo the scroll past the end
	check("k", func(int) int { return 1 })
}

func TestTUIOverviewRowsAndPaneAnswerTheMouseOnASmallScreen(t *testing.T) {
	t.Parallel()
	// Arrange: the body has room for one row; the second entry is scrolled
	// to its top.
	u := newUI(t, 80, 8, true).press("j")
	_, y := u.find("テンプレート", 0)

	// Act
	u.click(2, y)

	// Assert: regression guard; the row under the pointer is the one opened.
	if got := u.detailFirstLine(); got != "sandboxOptions.template" {
		t.Errorf("detail = %q\n%s", got, u.text())
	}

	// Arrange / Act: the wheel over the summary line, which is not a row.
	u = newUI(t, 80, 24, true)
	_, y = u.find("最重要・高", 0)
	u.wheel(2, y, true).press("enter")

	// Assert: it still moves the cursor.
	if got := u.detailFirstLine(); got != "sandboxOptions.template" {
		t.Errorf("detail after the wheel = %q\n%s", got, u.text())
	}
}

func TestTUIOverflowHintsCountWhatIsHidden(t *testing.T) {
	t.Parallel()
	// The Overview: rows above the hint plus its count are the whole canvas.
	full, _ := newUI(t, 80, 200, true).overviewBody()
	for len(full) > 0 && strings.TrimSpace(full[len(full)-1]) == "" {
		full = full[:len(full)-1]
	}
	for _, h := range []int{10, 16, 24} {
		u := newUI(t, 80, h, true)
		n, y, ok := u.hint()
		if !ok {
			t.Fatalf("height %d: no hint\n%s", h, u.text())
		}
		rows, _ := u.overviewBody()
		if shown := len(rows) - (len(u.lines()) - 1 - y); shown+n != len(full) {
			t.Errorf("height %d: %d rows shown and ほか %d, of %d", h, shown, n, len(full))
		}
	}

	// A list, scrolled or not, and one that just fits.
	var srcs []sbxenv.Source
	for i := range 30 {
		srcs = append(srcs, sbxenv.Source{ID: fmt.Sprintf("S%02d", i), Kind: sbxenv.SourceEnv, Path: fmt.Sprintf("/work/s%02d.yaml", i), Expanded: true})
	}
	rowRe := regexp.MustCompile(`^ {4}S(\d\d) `)
	sources := func(h int) *ui {
		m := tui.New(&sbxenv.Plan{Meta: sbxenv.Meta{Sources: srcs}}, true)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: h})
		return (&ui{t: t, m: m}).press("4")
	}
	u := sources(16)
	for _, j := range []int{0, 15, 14} {
		for range j {
			u.press("j")
		}
		last := -1
		for _, l := range u.lines() {
			if m := rowRe.FindStringSubmatch(l); m != nil {
				last, _ = strconv.Atoi(m[1])
			}
		}
		if n, _, _ := u.hint(); n != len(srcs)-1-last {
			t.Errorf("after %d j: last row S%02d, ほか %d", j, last, n)
		}
	}
	if u := sources(len(srcs) + 5); strings.Count(u.text(), "/work/s") != len(srcs) || u.has("ほか ") {
		t.Errorf("a list that just fits is drawn whole:\n%s", u.text())
	}

	// The detail and the help: the count is how many more lines j reveals.
	for name, u := range map[string]*ui{"detail": newUI(t, 80, 16, true).press("2", "enter"), "help": newUI(t, 80, 16, true).press("?")} {
		n, y, ok := u.hint()
		if !ok || y != len(u.lines())-2 {
			t.Fatalf("%s: the hint ends the pane\n%s", name, u.text())
		}
		for range n - 2 {
			u.press("j")
		}
		if got, _, _ := u.hint(); got != 2 {
			t.Errorf("%s: after %d j, ほか %d, want 2", name, n-2, got)
		}
		if u.press("j"); u.has("ほか ") {
			t.Errorf("%s: after %d j everything is shown\n%s", name, n-1, u.text())
		}
	}
}

func TestTUIScreensFitTheirLinesWithoutCuttingThem(t *testing.T) {
	t.Parallel()
	// Short values, so only a layout error would cut a line at an edge.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "agent", Path: "agent", Value: "claude"},
		{ID: "ports[0]", Path: "ports[]", Value: "0.0.0.0:3000", Risk: sbxenv.SeverityHigh},
	}}
	cmp := &live.Comparison{Skipped: []string{strings.Repeat("この項目は sbx の出力に含まれないため比較していません。", 4)}}
	single := func(w int) *ui {
		m := tui.New(p, true)
		m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		return &ui{t: t, m: m}
	}
	for _, tc := range []struct {
		name string
		u    *ui
	}{
		{"overview", targetUI(t, 80, 30, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})},
		{"risk list", single(80).press("2")},
		{"risk list beside its detail", single(130).press("2")},
		{"detail", single(80).press("2", "enter")},
		{"help", single(80).press("?")},
		{"picker", newTargetsUI(t, 100, 30).press("s")},
	} {
		// Assert: no line (or pane of a line) ends in the cut mark.
		for _, l := range tc.u.lines() {
			left, right, _ := splitPanes(l)
			for _, s := range []string{left, right} {
				if strings.HasSuffix(strings.TrimRight(s, " "), "..") {
					t.Errorf("%s: line cut at the edge: %q\n%s", tc.name, l, tc.u.text())
				}
			}
		}
	}
}

func TestTUIRiskRowKeepsTheValueBesideALongID(t *testing.T) {
	t.Parallel()
	// Arrange
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "kit:playwright-browsers/network-policy@1/runtime.allow/*.cdn.example.com", Path: "cap:network-policy@1",
		Kit: "playwright-browsers", Value: "*.cdn.example.com", Risk: sbxenv.SeverityMedium}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	u := &ui{t: t, m: m}

	// Act
	u.press("2")

	// Assert: the ID is shortened in the middle, so the value stays on the row.
	u.wantRe(`^ !  中 +kit:playwright.*\.\..*\.cdn\.example\.com  \*\.cdn\.example\.com *$`)
}

func TestTUIPickerKeepsTheStateAndTheFirstRowInView(t *testing.T) {
	t.Parallel()
	// Arrange: a very long first name, then many targets on a short screen.
	ts := []live.Target{{Name: strings.Repeat("x", 120), State: "稼働中", Sandbox: &live.Sandbox{Name: "x"}}}
	for i := range 10 {
		ts = append(ts, live.Target{Name: fmt.Sprintf("box-%02d", i), State: "停止中", Sandbox: &live.Sandbox{Name: "y"}})
	}
	u := targetUI(t, 100, 12, ts...)

	// Act
	u.press("s")

	// Assert: the name column stops at two fifths of the pane, so the
	// state stays on the row; the picker opens at the top.
	u.wantRe(`^ +x+\.\.x+ +稼働中 `)
	if !strings.Contains(u.cursorLine(), "xxxx") {
		t.Errorf("the cursor row (the shown target) is on screen:\n%s", u.text())
	}

	// Act / Assert: a click below the drawn rows switches to no unseen target.
	u.click(10, len(u.lines())-1)
	if !strings.HasPrefix(u.lines()[0], " sbxray  xxxx") {
		t.Errorf("title bar = %q", u.lines()[0])
	}
}

func TestTUIBackLinkIsClickableAcrossItsText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		dx   int // from the link's first cell
		back bool
	}{
		{"first cell", 0, true},
		{"last cell", ansi.StringWidth(backLink) - 1, true},
		{"the margin before it", -1, false},
	} {
		// Arrange
		u := newUI(t, 80, 30, true).press("3", "enter")
		x, y := u.find(backLink, 0)

		// Act
		u.click(x+tc.dx, y)

		// Assert
		if got := !u.has(backLink); got != tc.back {
			t.Errorf("%s: back to the list = %v, want %v\n%s", tc.name, got, tc.back, u.text())
		}
	}
}

func TestTUIStateLineGivesTheQueryAQuarterOfTheWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width       int
		query, want string
	}{
		{80, "abcdefghijklmnopqrst", "abcdefghijklmnopqrst"},            // 20 cells fit
		{80, "abcdefghijklmnopqrstu", "..defghijklmnopqrstu"},           // 21 do not
		{40, "abcdefghijklmnopqrstuvwxyz", "..stuvwxyz"},                // a quarter of 40
		{200, "abcdefghijklmnopqrstuvwxyz", "..efghijklmnopqrstuvwxyz"}, // at most 24
	} {
		// Arrange
		m := tui.New(&sbxenv.Plan{}, true)
		m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 30})
		u := &ui{t: t, m: m}

		// Act
		u.press("/")
		for _, r := range tc.query {
			u.press(string(r))
		}

		// Assert
		if l := u.lines()[2]; strings.TrimSpace(l) != "検索: "+tc.want+"_" {
			t.Errorf("width %d: state line = %q, want 検索: %s_", tc.width, l, tc.want)
		}
	}
}

func TestTUIOverviewStampsTheDriftCountOnlyWhenThereAreDrifts(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	none := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: &sbxenv.Plan{}, Sandbox: &live.Sandbox{Name: "web"}, Comparison: &live.Comparison{}})
	some := liveDriftUI(t)

	// Assert: ズレ is bold beside a count above zero, plain beside 0.
	if a := attrsAt(none.rawLine("ズレ 0"), "ズレ"); a["1"] {
		t.Errorf("ズレ 0 attrs = %v", a)
	}
	if a := attrsAt(some.rawLine("ズレ 1"), "ズレ"); !a["1"] {
		t.Errorf("ズレ 1 attrs = %v", a)
	}
}

func TestTUIOverviewKeepsTheDeclarationBesideALongActualValue(t *testing.T) {
	t.Parallel()
	// Arrange: sbx reports far more than the declaration says.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "ports[0]", Path: "ports[]", Value: "127.0.0.1:3000"}}}
	cmp := &live.Comparison{Checked: []string{"ポート公開"}, Compared: []string{"ports[0]"}, Drifts: []live.Drift{{Kind: "ポート公開",
		Title: "公開先が宣言と違う", Declared: "127.0.0.1:3000", Actual: strings.Repeat("0.0.0.0:3000 -> sandbox:3000/tcp, ", 3), Severity: sbxenv.SeverityHigh, ItemID: "ports[0]"}}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: the 実体 column stops at 30% of the width (36 of 120 cells),
	// so it starts no further left than cell 119-36.
	u.wantRe(`^ ✗  ポート +127\.0\.0\.1:3000 +0\.0\.0\.0:3000`)
	if x, _ := u.find("0.0.0.0:3000", 0); x < 119-36 {
		t.Errorf("the 実体 column starts at cell %d, wider than 30%%", x)
	}
}

func TestTUIOverviewSummaryCountsFindingsAndDriftsByLevel(t *testing.T) {
	t.Parallel()
	// Arrange: declared settings and drifts, one high, two medium and one
	// low of each.
	p := &sbxenv.Plan{Items: []sbxenv.Item{
		{ID: "ports[0]", Path: "ports[]", Value: "0.0.0.0:3000", Risk: sbxenv.SeverityHigh},
		{ID: "secrets.a", Path: "secrets.a", Value: "ref: op://a", Risk: sbxenv.SeverityMedium},
		{ID: "secrets.b", Path: "secrets.b", Value: "ref: op://b", Risk: sbxenv.SeverityMedium},
		{ID: "agent", Path: "agent", Value: "claude", Risk: sbxenv.SeverityLow},
	}}
	drift := func(sev sbxenv.Severity) live.Drift {
		return live.Drift{Kind: "ポート公開", Title: "宣言にないポートが公開されている", Declared: "（宣言なし）", Actual: string(sev), Severity: sev}
	}
	cmp := &live.Comparison{Drifts: []live.Drift{drift(sbxenv.SeverityHigh), drift(sbxenv.SeverityMedium), drift(sbxenv.SeverityMedium), drift(sbxenv.SeverityLow)}}

	// Act
	u := targetUI(t, 120, 40, live.Target{Name: "web", State: "稼働中", Plan: p, Sandbox: &live.Sandbox{Name: "web"}, Comparison: cmp})

	// Assert: high counts the high ones, medium the medium ones; low is in neither.
	u.wantRe(`^ ✗ 最重要・高 2    ! 中 4    ズレ 4 `)
}

func TestTUIOverviewListsARiskySettingNoSectionCovers(t *testing.T) {
	t.Parallel()
	// Arrange: writableEnvFiles is rated high but belongs to no section.
	p := &sbxenv.Plan{Items: []sbxenv.Item{{ID: "sandboxOptions.writableEnvFiles", Path: "sandboxOptions.writableEnvFiles", Value: "true", Risk: sbxenv.SeverityHigh}}}
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	u := &ui{t: t, m: m}

	// Assert: regression; the summary counted it while the manifest said
	// （表示できる設定がありません）.
	u.want("その他の危険度のある設定")
	u.wantRe(`^ ✗  writableEnvFiles +true *$`)
	if u.has("表示できる設定がありません") {
		t.Errorf("a counted setting had no row:\n%s", u.text())
	}
}
