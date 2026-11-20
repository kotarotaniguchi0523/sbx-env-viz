package render_test

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/text"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

const env = `schemaVersion: "1"
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
`

const env2 = `env:
  NODE_ENV: prod
kits:
  - docker.io/sbx/playwright:1.61.1
`

func plan(t *testing.T, mode sbxenv.Redaction) *sbxenv.Plan {
	t.Helper()
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{
		{Path: "/home/tester/proj/sbxenv.yaml", Content: []byte(env), Kind: sbxenv.LayerEnv},
		{Path: "/home/tester/proj/local.yaml", Content: []byte(env2), Kind: sbxenv.LayerEnv},
	}, sbxenv.ResolveOptions{ProjectDir: "/home/tester/proj", HomeDir: "/home/tester", Redaction: mode})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestJSONStableAndValid(t *testing.T) {
	var a, b bytes.Buffer
	if err := jsonout.Write(&a, plan(t, sbxenv.RedactStrict)); err != nil {
		t.Fatal(err)
	}
	if err := jsonout.Write(&b, plan(t, sbxenv.RedactStrict)); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("JSON output differs between runs")
	}
	var decoded map[string]any
	if err := json.Unmarshal(a.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	meta := decoded["meta"].(map[string]any)
	if meta["schemaVersion"] != "1" || meta["redaction"] != "strict" {
		t.Errorf("meta = %v", meta)
	}
	for _, k := range []string{"items", "boundaries", "coverage", "diagnostics", "summary", "invocation"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	items := decoded["items"].([]any)
	var ids []string
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	// Within a section items are sorted by ID.
	envIdx := -1
	for i, id := range ids {
		if strings.HasPrefix(id, "env.") {
			envIdx = i
		}
	}
	if envIdx < 0 {
		t.Fatalf("ids = %v", ids)
	}
	if strings.Contains(a.String(), "\"prod\"") || strings.Contains(a.String(), "/home/tester") {
		t.Error("strict JSON must hide env values and the home directory")
	}
}

func TestTextShowsAllHighEntries(t *testing.T) {
	var buf bytes.Buffer
	p := plan(t, sbxenv.RedactHome)
	if err := text.Write(&buf, p); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"宣言の静的解析", "PORT_ALL_INTERFACES", "PULL_ALWAYS_DEV", "lifecycle.initialize[0]", "Coverage", "~/proj/sbxenv.yaml", "env.NODE_ENV = prod", "(上書き済み) test"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "安全") || strings.Contains(out, "OK") {
		t.Error("text output must not claim safety")
	}
}

// colorRe matches SGR color parameters (truecolor / 256 / basic foreground).
var colorRe = regexp.MustCompile("\x1b\\[[0-9;]*(38;[25];|3[0-7]m|9[0-7]m)")

func send(m *tui.Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			r := []rune(k)
			msg = tea.KeyPressMsg{Code: r[0], Text: k}
		}
		m.Update(msg)
	}
}

func TestTUIViewsAndLayouts(t *testing.T) {
	p := plan(t, sbxenv.RedactHome)
	m := tui.New(p, true)
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	out := m.Render()
	if colorRe.MatchString(out) {
		t.Error("NO_COLOR output must not contain color escapes")
	}
	for _, want := range []string{"1:Risk", "Host実行", "宣言の静的解析", "│", "解説", "上書き規則"} {
		if !strings.Contains(out, want) {
			t.Errorf("two-pane risk view missing %q\n%s", want, out)
		}
	}
	send(m, "2")
	out = m.Render()
	if !strings.Contains(out, "g:層別") || !strings.Contains(out, "層 L") {
		t.Errorf("items view should group by layer\n%s", out)
	}
	send(m, "g")
	if !strings.Contains(m.Render(), "境界") {
		t.Error("g should switch to boundary grouping")
	}
	send(m, "g")
	if !strings.Contains(m.Render(), "kit: ") {
		t.Error("g should switch to kit grouping")
	}
	send(m, "f")
	if !strings.Contains(m.Render(), "絞り込み: L1") {
		t.Error("f should filter by the first layer")
	}
	send(m, "/", "N", "O", "D", "E", "enter")
	out = m.Render()
	if !strings.Contains(out, "env.NODE_ENV") || strings.Contains(out, "ports[") {
		t.Errorf("search should narrow the list\n%s", out)
	}
	send(m, "esc", "3")
	if !strings.Contains(m.Render(), "~/proj/sbxenv.yaml") {
		t.Error("sources view should list files")
	}
	send(m, "?")
	if !strings.Contains(m.Render(), "キー操作") {
		t.Error("? should show help")
	}

	// Narrow terminal: one pane, Enter opens the detail, Esc returns.
	n := tui.New(p, true)
	n.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if strings.Contains(n.Render(), "│") {
		t.Error("80 columns should use a single pane")
	}
	send(n, "enter")
	if !strings.Contains(n.Render(), "出典") && !strings.Contains(n.Render(), "場所") {
		t.Errorf("enter should open the detail\n%s", n.Render())
	}
	send(n, "esc")
	if strings.Contains(n.Render(), "出典") {
		t.Error("esc should return to the list")
	}
	if _, cmd := n.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Error("q should quit")
	}
	if s := tui.ExitSummary(p); !strings.HasPrefix(s, "sbx-env-viz: Host実行") {
		t.Errorf("exit summary = %q", s)
	}
}

func TestTUIColorWhenAllowed(t *testing.T) {
	m := tui.New(plan(t, sbxenv.RedactHome), false)
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	if !colorRe.MatchString(m.Render()) {
		t.Error("colors expected when NO_COLOR is not set")
	}
}
