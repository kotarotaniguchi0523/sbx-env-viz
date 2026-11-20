package render_test

// Black-box tests of the three renderers. Each case builds its own plan
// from real files in its own temporary directory and inspects only what a
// user would see: the rendered text, the JSON document, or the TUI screen.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/loader"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/text"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

const baseEnv = `schemaVersion: "1"
agent: claude
workspace: .
env:
  NODE_ENV: test
ports:
  - sandbox: 3000
    hostIP: 0.0.0.0
lifecycle:
  initialize:
    - command: echo hi
sandboxOptions:
  template: ghcr.io/example/x:dev
kits:
  - ./kits/net
`

const localEnv = `env:
  NODE_ENV: prod
kits:
  - docker.io/sbx/playwright:1.61.1
`

const netKit = `schemaVersion: "3"
kind: mixin
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: [api.example.com]
`

// newPlan writes the two-layer example into a fresh home and resolves it.
func newPlan(t *testing.T, mode sbxenv.Redaction) (*sbxenv.Plan, string) {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	for rel, c := range map[string]string{"sbxenv.yaml": baseEnv, "local.yaml": localEnv, "kits/net/net.yaml": netKit} {
		p := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := loader.Load(loader.Request{Paths: []string{"sbxenv.yaml", "local.yaml"}, WorkDir: proj, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sbxenv.Resolve(res.Layers, sbxenv.ResolveOptions{ProjectDir: res.ProjectDir, HomeDir: home, Kits: loader.FSKits{}, Redaction: mode})
	if err != nil {
		t.Fatal(err)
	}
	return p, home
}

// --- JSON ---------------------------------------------------------------

func TestJSONHasTheStableTopLevelShape(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactStrict)
	var buf bytes.Buffer

	// Act
	err := jsonout.Write(&buf, p)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"meta", "invocation", "items", "boundaries", "coverage", "diagnostics", "summary"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing top-level %q", k)
		}
	}
	if doc["meta"].(map[string]any)["schemaVersion"] != "1" {
		t.Error("schemaVersion must be \"1\"")
	}
}

func TestJSONIsIdenticalForIndependentlyBuiltEqualPlans(t *testing.T) {
	t.Parallel()
	// Arrange
	p1, home1 := newPlan(t, sbxenv.RedactStrict)
	p2, home2 := newPlan(t, sbxenv.RedactStrict)
	var a, b bytes.Buffer

	// Act
	_ = jsonout.Write(&a, p1)
	_ = jsonout.Write(&b, p2)

	// Assert: different temp homes, same project → same document after ~ shortening.
	if home1 == home2 || a.String() != b.String() {
		t.Error("JSON must not depend on where the home directory is or on map order")
	}
}

func TestJSONSortsSourcesNumerically(t *testing.T) {
	t.Parallel()
	// Arrange
	home := t.TempDir()
	var layers []sbxenv.LayerInput
	for i := 1; i <= 11; i++ {
		content := "env:\n  K: \"x\"\n"
		if i == 1 {
			content = "schemaVersion: \"1\"\nagent: claude\n"
		}
		layers = append(layers, sbxenv.LayerInput{Path: filepath.Join(home, "f", strings.Repeat("a", i)+".yaml"), Content: []byte(content), Kind: sbxenv.LayerEnv})
	}
	p, err := sbxenv.Resolve(layers, sbxenv.ResolveOptions{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer

	// Act
	_ = jsonout.Write(&buf, p)

	// Assert
	var doc struct {
		Meta struct {
			Sources []struct{ ID string } `json:"sources"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(buf.Bytes(), &doc)
	var ids []string
	for _, s := range doc.Meta.Sources {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids[:11], ","); got != "L1,L2,L3,L4,L5,L6,L7,L8,L9,L10,L11" {
		t.Errorf("source order = %s", got)
	}
}

// --- text -----------------------------------------------------------------

func TestTextListsEveryHighRiskEntry(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactHome)
	var buf bytes.Buffer

	// Act
	err := text.Write(&buf, p)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"宣言の静的解析", "PORT_ALL_INTERFACES", "PULL_ALWAYS_DEV", "lifecycle.initialize[0]", "~/proj/sbxenv.yaml:", "env.NODE_ENV = prod", "(上書き済み) test", "remote-kit", "== Sources =="} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q", want)
		}
	}
}

func TestTextCapsLowEntriesButNeverHighOnes(t *testing.T) {
	t.Parallel()
	// Arrange: more high-risk entries (20 host commands) than the low cap,
	// plus 40 medium-risk ports.
	var b strings.Builder
	b.WriteString("schemaVersion: \"1\"\nagent: claude\nports:\n")
	for i := 1; i <= 40; i++ {
		b.WriteString("  - {sandbox: " + strconv.Itoa(i) + ", hostIP: 192.168.0.1}\n")
	}
	b.WriteString("lifecycle:\n  initialize:\n")
	for i := 0; i < 20; i++ {
		b.WriteString("    - command: step" + strconv.Itoa(i) + "\n")
	}
	home := t.TempDir()
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: filepath.Join(home, "e.yaml"), Content: []byte(b.String()), Kind: sbxenv.LayerEnv}}, sbxenv.ResolveOptions{HomeDir: home, Redaction: sbxenv.RedactHome})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer

	// Act
	_ = text.Write(&buf, p)

	// Assert
	risk := buf.String()[:strings.Index(buf.String(), "== Coverage")]
	for i := 0; i < 20; i++ {
		if id := "lifecycle.initialize[" + strconv.Itoa(i) + "]"; !strings.Contains(risk, id) {
			t.Errorf("high-risk %s must always be listed in Risk", id)
		}
	}
	if !strings.Contains(risk, "件を省略") {
		t.Error("medium entries beyond the cap should be summarised")
	}
}

// --- TUI --------------------------------------------------------------------

func press(m *tui.Model, keys ...string) {
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
		m.Update(msg)
	}
}

func screen(t *testing.T, width int, keys ...string) string {
	t.Helper()
	p, _ := newPlan(t, sbxenv.RedactHome)
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	press(m, keys...)
	return m.Render()
}

// colorRe matches SGR color parameters (truecolor, 256-color, basic).
var colorRe = regexp.MustCompile("\x1b\\[[0-9;]*(38;[25];|3[0-7]m|9[0-7]m)")

func TestTUIStartsOnRiskWithTheSummaryHeader(t *testing.T) {
	t.Parallel()
	// Act
	s := screen(t, 130)

	// Assert
	for _, want := range []string{"1:Risk", "Host実行", "宣言の静的解析", "env-args:"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen missing %q", want)
		}
	}
	if strings.Contains(s, "安全") {
		t.Error("the TUI must never claim safety")
	}
}

func TestTUILayoutDependsOnWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width   int
		twoPane bool
	}{{80, false}, {119, false}, {120, true}, {160, true}}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			// Act
			s := screen(t, tc.width)

			// Assert
			if got := strings.Contains(s, "│"); got != tc.twoPane {
				t.Errorf("width %d: two panes = %v, want %v", tc.width, got, tc.twoPane)
			}
		})
	}
}

func TestTUIDetailExplainsTheSelectedItem(t *testing.T) {
	t.Parallel()
	// Act: items view, search for the overridden env var, open the detail.
	s := screen(t, 130, "2", "/", "N", "O", "D", "E", "enter")

	// Assert
	for _, want := range []string{"env.NODE_ENV", "要は:", "上書き規則: マップ結合", "上書き履歴", "shadow", "replace", "~/proj/local.yaml:2"} {
		if !strings.Contains(s, want) {
			t.Errorf("detail missing %q\n%s", want, s)
		}
	}
}

func TestTUIViewsGroupingFilterSearchAndHelp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		keys []string
		want []string
		not  []string
	}{
		{"items grouped by layer", []string{"2"}, []string{"g:層別", "層 L1"}, nil},
		{"g cycles to boundary", []string{"2", "g"}, []string{"g:境界別", "境界: host-exec"}, nil},
		{"g cycles to kit", []string{"2", "g", "g"}, []string{"g:kit別", "kit: net"}, nil},
		{"g cycles back to layer", []string{"2", "g", "g", "g"}, []string{"g:層別"}, nil},
		{"tab moves to the next view", []string{"tab"}, []string{"g:層別"}, nil},
		{"] and [ move between views", []string{"]", "]", "["}, []string{"g:層別"}, nil},
		{"sources view", []string{"3"}, []string{"~/proj/sbxenv.yaml", "未展開・解析範囲外"}, nil},
		{"f filters by the first layer", []string{"f"}, []string{"絞り込み: L1", "非表示の警告・エラー"}, nil},
		{"f cycles through every source", []string{"f", "f", "f", "f"}, nil, []string{"絞り込み:"}},
		{"search narrows the list", []string{"2", "/", "a", "g", "e", "n", "t", "enter"}, []string{"agent = claude"}, []string{"ports["}},
		{"backspace edits the query", []string{"2", "/", "a", "x", "backspace", "enter"}, []string{"検索: a"}, nil},
		{"esc clears the search", []string{"2", "/", "a", "g", "enter", "esc"}, []string{"ports["}, []string{"検索:"}},
		{"help", []string{"?"}, []string{"キー操作", "[HOST]"}, nil},
		{"any key closes help", []string{"?", "x"}, []string{"1:Risk"}, []string{"キー操作"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			s := screen(t, 130, tc.keys...)

			// Assert
			for _, w := range tc.want {
				if !strings.Contains(s, w) {
					t.Errorf("screen missing %q\n%s", w, s)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(s, n) {
					t.Errorf("screen should not contain %q\n%s", n, s)
				}
			}
		})
	}
}

func TestTUINarrowScreenOpensAndClosesTheDetail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		keys   []string
		detail bool
	}{
		{"list by default", nil, false},
		{"enter opens", []string{"enter"}, true},
		{"l opens", []string{"l"}, true},
		{"esc returns", []string{"enter", "esc"}, false},
		{"h returns", []string{"l", "h"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			s := screen(t, 80, tc.keys...)

			// Assert
			if got := strings.Contains(s, "場所") || strings.Contains(s, "出典"); got != tc.detail {
				t.Errorf("detail shown = %v, want %v\n%s", got, tc.detail, s)
			}
		})
	}
}

func TestTUICursorMovesAndScrolls(t *testing.T) {
	t.Parallel()
	// Arrange
	first := screen(t, 130, "2")

	// Act
	moved := screen(t, 130, "2", "j", "down")
	paged := screen(t, 130, "2", "pgdown")
	detailScroll := screen(t, 130, "2", "l", "j", "j", "k")

	// Assert
	if first == moved || first == paged {
		t.Error("moving the cursor must change the selection")
	}
	if !strings.Contains(detailScroll, "* ") {
		t.Error("focusing the detail pane marks the list cursor with *")
	}
}

func TestTUIColorAndNoColor(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactHome)
	plain, colored := tui.New(p, true), tui.New(p, false)
	for _, m := range []*tui.Model{plain, colored} {
		m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	}

	// Act
	ps, cs := plain.Render(), colored.Render()

	// Assert
	if colorRe.MatchString(ps) {
		t.Error("NO_COLOR output must not contain color escapes")
	}
	if !colorRe.MatchString(cs) {
		t.Error("colors expected when NO_COLOR is not set")
	}
}

func TestTUIRunQuitsAndReturnsTheSummaryLine(t *testing.T) {
	t.Parallel()
	// Arrange
	p, _ := newPlan(t, sbxenv.RedactHome)
	in := strings.NewReader("2gq")
	var out bytes.Buffer

	// Act
	done := make(chan struct{})
	var line string
	var err error
	go func() {
		line, err = tui.Run(p, in, &out)
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
	if !strings.HasPrefix(line, "sbx-env-viz: Host実行") || !strings.Contains(line, "警告") {
		t.Errorf("summary line = %q", line)
	}
	if out.Len() == 0 {
		t.Error("the program should have drawn to its output")
	}
}
