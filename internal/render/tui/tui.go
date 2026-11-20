// Package tui is the interactive Bubble Tea v2 view of a Plan.
//
// Views: 1 Risk (default), 2 Items (g cycles layer / boundary / kit
// grouping), 3 Sources. At 120 columns or wider the list and the detail
// pane sit side by side; narrower terminals show one pane at a time.
package tui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// TwoPaneMinWidth is the narrowest width that shows list and detail together.
const TwoPaneMinWidth = 120

type viewID int

const (
	viewRisk viewID = iota
	viewItems
	viewSources
)

var viewNames = []string{"1:Risk", "2:Items", "3:Sources"}

type groupMode int

const (
	groupLayer groupMode = iota
	groupBoundary
	groupKit
)

var groupNames = []string{"層別", "境界別", "kit別"}

type row struct {
	header bool
	label  string
	sev    sbxenv.Severity
	item   *sbxenv.Item
	diag   *sbxenv.Diagnostic
	cover  *sbxenv.Coverage
	source *sbxenv.Source
}

// Model is the Bubble Tea model.
type Model struct {
	plan    *sbxenv.Plan
	idx     map[string]sbxenv.Source
	view    viewID
	group   groupMode
	cursor  [3]int
	width   int
	height  int
	focus   int // 0 list, 1 detail
	detail  bool
	dScroll int
	help    bool

	filters   []string
	filterIdx int

	searching bool
	query     string

	noColor bool
	st      styles
	rows    []row
}

type styles struct {
	title, dim, sel, header, crit, high, med, low, info, focus lipgloss.Style
}

func newStyles(noColor bool) styles {
	s := styles{
		title:  lipgloss.NewStyle().Bold(true),
		dim:    lipgloss.NewStyle().Faint(true),
		sel:    lipgloss.NewStyle().Reverse(true),
		header: lipgloss.NewStyle().Bold(true),
		focus:  lipgloss.NewStyle().Bold(true),
	}
	if noColor {
		s.crit, s.high, s.med, s.low, s.info = s.title, s.title, lipgloss.NewStyle(), lipgloss.NewStyle(), s.dim
		return s
	}
	s.crit = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Bold(true)
	s.high = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")).Bold(true)
	s.med = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7d75f"))
	s.low = lipgloss.NewStyle().Foreground(lipgloss.Color("#87afd7"))
	s.info = lipgloss.NewStyle().Foreground(lipgloss.Color("#8a8a8a"))
	return s
}

// New builds a model. noColor disables all colors (NO_COLOR).
func New(p *sbxenv.Plan, noColor bool) *Model {
	p = model.ForTerminal(p)
	m := &Model{plan: p, idx: model.SourceIndex(p), width: 100, height: 30, noColor: noColor, st: newStyles(noColor)}
	m.filters = []string{""}
	for _, s := range p.Meta.Sources {
		if s.Kind == sbxenv.SourceEnv || s.Kind == sbxenv.SourceUserDefaults || s.Kind == sbxenv.SourceKit {
			m.filters = append(m.filters, s.ID)
		}
	}
	m.rebuild()
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) twoPane() bool { return m.width >= TwoPaneMinWidth }

func (m *Model) filter() string { return m.filters[m.filterIdx] }

func (m *Model) matches(text string) bool {
	if m.query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(m.query))
}

func (m *Model) itemVisible(it *sbxenv.Item) bool {
	if f := m.filter(); f != "" && !model.TouchesSource(it, f) {
		return false
	}
	return m.matches(it.ID + " " + it.Value + " " + strings.Join(it.Badges, ""))
}

func (m *Model) rebuild() {
	var rows []row
	switch m.view {
	case viewRisk:
		for _, r := range model.RiskList(m.plan) {
			if f := m.filter(); f != "" {
				switch {
				case r.Item != nil && !model.TouchesSource(r.Item, f):
					continue
				case r.Diag != nil && !model.DiagTouchesSource(r.Diag, f):
					continue
				case r.Cover != nil && r.Cover.Origin.SourceID != f:
					continue
				}
			}
			label := strings.TrimSpace(strings.Join(r.Badges, "") + " " + r.Label)
			if !m.matches(label) {
				continue
			}
			rows = append(rows, row{label: label, sev: r.Severity, item: r.Item, diag: r.Diag, cover: r.Cover})
		}
	case viewItems:
		groups := map[string][]*sbxenv.Item{}
		var keys []string
		for i := range m.plan.Items {
			it := &m.plan.Items[i]
			if !m.itemVisible(it) {
				continue
			}
			k := m.groupKey(it)
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], it)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rows = append(rows, row{header: true, label: k})
			for _, it := range groups[k] {
				label := strings.TrimSpace(strings.Join(it.Badges, "") + " " + it.ID + " = " + it.Value)
				rows = append(rows, row{label: label, sev: it.Risk, item: it})
			}
		}
	case viewSources:
		for i := range m.plan.Meta.Sources {
			s := &m.plan.Meta.Sources[i]
			state := "展開済み"
			if !s.Expanded {
				state = "未展開"
			}
			label := fmt.Sprintf("%-7s %-13s %s [%s]", s.ID, s.Kind, s.Path, state)
			if !m.matches(label) {
				continue
			}
			rows = append(rows, row{label: label, sev: sbxenv.SeverityInfo, source: s})
		}
		if len(m.plan.Coverage) > 0 {
			rows = append(rows, row{header: true, label: fmt.Sprintf("未展開・解析範囲外 (%d)", len(m.plan.Coverage))})
			for i := range m.plan.Coverage {
				c := &m.plan.Coverage[i]
				label := fmt.Sprintf("[%s] %s", c.Kind, c.Ref)
				if m.matches(label) {
					rows = append(rows, row{label: label, sev: sbxenv.SeverityMedium, cover: c})
				}
			}
		}
	}
	m.rows = rows
	c := &m.cursor[m.view]
	if *c >= len(rows) {
		*c = len(rows) - 1
	}
	if *c < 0 {
		*c = 0
	}
	if len(rows) > 0 && rows[*c].header {
		m.move(1)
	}
}

func (m *Model) groupKey(it *sbxenv.Item) string {
	switch m.group {
	case groupBoundary:
		bs := model.BoundariesFor(m.plan, it.ID)
		if len(bs) == 0 {
			return "境界: なし（sandbox 内の設定）"
		}
		return fmt.Sprintf("境界: %s (%s)", bs[0].Kind, bs[0].Where)
	case groupKit:
		if it.Kit != "" {
			return "kit: " + it.Kit
		}
		return "kit: (環境ファイル直書き)"
	}
	o := model.LastOrigin(it)
	if s, ok := m.idx[o.SourceID]; ok {
		return fmt.Sprintf("層 %s %s", s.ID, s.Path)
	}
	return "層 " + o.SourceID
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	c := m.cursor[m.view]
	for i := 0; i < len(m.rows); i++ {
		c += delta
		if c < 0 {
			c = 0
			delta = 1
		}
		if c >= len(m.rows) {
			c = len(m.rows) - 1
			delta = -1
		}
		if !m.rows[c].header {
			break
		}
	}
	m.cursor[m.view] = c
	m.dScroll = 0
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m *Model) key(k string) (tea.Model, tea.Cmd) {
	if m.searching {
		switch k {
		case "esc":
			m.searching, m.query = false, ""
		case "enter":
			m.searching = false
		case "backspace":
			if r := []rune(m.query); len(r) > 0 {
				m.query = string(r[:len(r)-1])
			}
		case "ctrl+c":
			return m, tea.Quit
		default:
			if k == "space" {
				k = " "
			}
			if len([]rune(k)) == 1 {
				m.query += k
			}
		}
		m.rebuild()
		return m, nil
	}
	if m.help {
		if k == "q" || k == "ctrl+c" {
			return m, tea.Quit
		}
		m.help = false
		return m, nil
	}
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = true
	case "1", "2", "3":
		m.setView(viewID(k[0] - '1'))
	case "]", "tab":
		m.setView((m.view + 1) % 3)
	case "[", "shift+tab":
		m.setView((m.view + 2) % 3)
	case "j", "down":
		if m.inDetail() {
			m.dScroll++
		} else {
			m.move(1)
		}
	case "k", "up":
		if m.inDetail() {
			if m.dScroll > 0 {
				m.dScroll--
			}
		} else {
			m.move(-1)
		}
	case "pgdown", "ctrl+d":
		for i := 0; i < 10; i++ {
			m.key("j")
		}
	case "pgup", "ctrl+u":
		for i := 0; i < 10; i++ {
			m.key("k")
		}
	case "g":
		if m.view == viewItems {
			m.group = (m.group + 1) % 3
			m.cursor[viewItems] = 0
			m.rebuild()
		}
	case "f":
		m.filterIdx = (m.filterIdx + 1) % len(m.filters)
		m.rebuild()
	case "/":
		m.searching = true
		m.query = ""
		m.rebuild()
	case "l", "right", "enter":
		if m.twoPane() {
			m.focus = 1
		} else {
			m.detail = true
		}
	case "h", "left":
		if m.twoPane() {
			m.focus = 0
		} else {
			m.detail = false
		}
	case "esc":
		if m.query != "" {
			m.query = ""
			m.rebuild()
		} else if m.twoPane() {
			m.focus = 0
		} else {
			m.detail = false
		}
	}
	return m, nil
}

func (m *Model) inDetail() bool {
	if m.twoPane() {
		return m.focus == 1
	}
	return m.detail
}

func (m *Model) setView(v viewID) {
	m.view = v
	m.detail = false
	m.focus = 0
	m.dScroll = 0
	m.rebuild()
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.Render())
	v.AltScreen = true
	v.WindowTitle = "sbx-env-viz"
	return v
}

// Render returns the screen as a string (used by View and tests).
func (m *Model) Render() string {
	w, h := m.width, m.height
	if w < 20 {
		w = 20
	}
	if h < 8 {
		h = 8
	}
	header := m.headerLines(w)
	footer := m.footer(w)
	bodyH := h - len(header) - 1
	if bodyH < 3 {
		bodyH = 3
	}
	var body string
	switch {
	case m.help:
		body = m.helpText(w, bodyH)
	case m.twoPane():
		lw := w * 45 / 100
		rw := w - lw - 3
		list := m.listLines(lw, bodyH)
		det := m.detailLines(rw, bodyH)
		sep := "│"
		var b strings.Builder
		for i := 0; i < bodyH; i++ {
			l, r := "", ""
			if i < len(list) {
				l = list[i]
			}
			if i < len(det) {
				r = det[i]
			}
			b.WriteString(pad(l, lw) + " " + m.st.dim.Render(sep) + " " + r)
			if i < bodyH-1 {
				b.WriteString("\n")
			}
		}
		body = b.String()
	case m.detail:
		body = strings.Join(m.detailLines(w, bodyH), "\n")
	default:
		body = strings.Join(m.listLines(w, bodyH), "\n")
	}
	return strings.Join(header, "\n") + "\n" + body + "\n" + footer
}

func (m *Model) headerLines(w int) []string {
	p := m.plan
	var tabs []string
	for i, n := range viewNames {
		if viewID(i) == m.view {
			tabs = append(tabs, m.st.sel.Render(" "+n+" "))
		} else {
			tabs = append(tabs, " "+n+" ")
		}
	}
	extra := ""
	if m.view == viewItems {
		extra = "  g:" + groupNames[m.group]
	}
	lines := []string{
		trunc(m.st.title.Render("sbx-env-viz")+" "+strings.Join(tabs, "")+extra, w),
		trunc(sbxenv.SummaryLine(p.Summary), w),
		trunc(m.st.dim.Render("宣言の静的解析（稼働中 sandbox の状態ではない） ")+model.ArgsLine(p), w),
	}
	var state []string
	if f := m.filter(); f != "" {
		hidden := 0
		for i := range p.Diagnostics {
			d := &p.Diagnostics[i]
			if d.Level != sbxenv.LevelInfo && !model.DiagTouchesSource(d, f) {
				hidden++
			}
		}
		label := f
		if s, ok := m.idx[f]; ok {
			label = f + " " + s.Path
		}
		state = append(state, fmt.Sprintf("絞り込み: %s（非表示の警告・エラー %d 件）", label, hidden))
	}
	if m.searching {
		state = append(state, "検索: "+m.query+"▏")
	} else if m.query != "" {
		state = append(state, "検索: "+m.query+"（Esc で解除）")
	}
	if len(state) > 0 {
		lines = append(lines, trunc(m.st.focus.Render(strings.Join(state, "  ")), w))
	}
	return lines
}

func (m *Model) footer(w int) string {
	keys := "1-3/[]:ビュー j/k:移動 h/l:ペイン Enter:詳細 g:グループ f:絞り込み /:検索 ?:ヘルプ q:終了"
	return trunc(m.st.dim.Render(keys), w)
}

func (m *Model) sevStyle(s sbxenv.Severity) lipgloss.Style {
	switch s {
	case sbxenv.SeverityCritical:
		return m.st.crit
	case sbxenv.SeverityHigh:
		return m.st.high
	case sbxenv.SeverityMedium:
		return m.st.med
	case sbxenv.SeverityLow:
		return m.st.low
	}
	return m.st.info
}

func (m *Model) listLines(w, h int) []string {
	if len(m.rows) == 0 {
		return []string{m.st.dim.Render("(該当なし)")}
	}
	cur := m.cursor[m.view]
	start := 0
	if cur >= h {
		start = cur - h + 1
	}
	var out []string
	for i := start; i < len(m.rows) && len(out) < h; i++ {
		r := m.rows[i]
		if r.header {
			out = append(out, m.st.header.Render(trunc(r.label, w)))
			continue
		}
		tag := model.SeverityTag(r.sev)
		text := trunc(tag+" "+r.label, w-2)
		if i == cur {
			prefix := "> "
			if m.twoPane() && m.focus == 1 {
				prefix = "* "
			}
			out = append(out, m.st.sel.Render(pad(prefix+text, w)))
			continue
		}
		out = append(out, "  "+m.sevStyle(r.sev).Render(tag)+trimPrefixWidth(text, len(tag)))
	}
	return out
}

func trimPrefixWidth(s string, n int) string {
	if len(s) <= n {
		return ""
	}
	return s[n:]
}

func (m *Model) current() *row {
	if len(m.rows) == 0 {
		return nil
	}
	return &m.rows[m.cursor[m.view]]
}

func (m *Model) detailLines(w, h int) []string {
	r := m.current()
	if r == nil {
		return []string{m.st.dim.Render("(項目がありません)")}
	}
	var b strings.Builder
	switch {
	case r.item != nil:
		m.writeItem(&b, r.item)
	case r.diag != nil:
		m.writeDiag(&b, r.diag)
	case r.cover != nil:
		c := r.cover
		fmt.Fprintf(&b, "%s\n\n種別: %s\n対象: %s\n理由: %s\n出典: %s\n", m.st.title.Render("解析範囲外（Coverage）"), c.Kind, c.Ref, c.Reason, model.FormatOrigin(m.idx, c.Origin))
		b.WriteString("\nこの部分は推測で埋めず、未解析として常に表示しています。\n")
	case r.source != nil:
		m.writeSource(&b, r.source)
	}
	wrapped := lipgloss.NewStyle().Width(w).Render(b.String())
	lines := strings.Split(wrapped, "\n")
	if m.dScroll > len(lines)-1 {
		m.dScroll = max(0, len(lines)-1)
	}
	lines = lines[m.dScroll:]
	if len(lines) > h {
		lines = lines[:h]
	}
	return lines
}

func (m *Model) writeItem(b *strings.Builder, it *sbxenv.Item) {
	fmt.Fprintf(b, "%s\n", m.st.title.Render(it.ID))
	fmt.Fprintf(b, "値: %s\n", it.Value)
	fmt.Fprintf(b, "リスク: %s  %s\n", it.Risk, strings.Join(it.Badges, " "))
	if it.Kit != "" {
		fmt.Fprintf(b, "kit: %s\n", it.Kit)
	}
	for _, f := range it.Fields {
		fmt.Fprintf(b, "  %s: %s\n", f.Key, f.Value)
	}
	if d := it.Doc; d != nil {
		fmt.Fprintf(b, "\n%s\n", m.st.header.Render("解説（"+d.Title+"）"))
		fmt.Fprintf(b, "要は: %s\n", d.Meaning)
		if d.Detail != "" {
			fmt.Fprintf(b, "注意: %s\n", d.Detail)
		}
		fmt.Fprintf(b, "上書き規則: %s — %s\n", d.MergeLabel, d.MergeDetail)
		if d.Recreate {
			b.WriteString("反映: 既存 sandbox には効かず、sbx env rm → create で作り直したときに反映されます。\n")
		}
		fmt.Fprintf(b, "参照: %s\n", d.Reference)
	}
	fmt.Fprintf(b, "\n%s\n", m.st.header.Render("上書き履歴"))
	for _, h := range it.History {
		fmt.Fprintf(b, "  %-7s %s  @ %s\n", h.Op, h.Value, model.FormatOrigin(m.idx, h.Origin))
	}
	fmt.Fprintf(b, "\n%s\n", m.st.header.Render("出典"))
	for _, o := range it.Origins {
		fmt.Fprintf(b, "  %s\n", model.FormatOrigin(m.idx, o))
	}
	if bs := model.BoundariesFor(m.plan, it.ID); len(bs) > 0 {
		fmt.Fprintf(b, "\n%s\n", m.st.header.Render("境界"))
		for _, x := range bs {
			flags := ""
			if x.Opaque {
				flags += " [OPAQUE]"
			}
			if x.Skipped {
				flags += " [SKIP]"
			}
			access := ""
			if x.Access != "" {
				access = " " + x.Access
			}
			fmt.Fprintf(b, "  %s 実行/所在=%s 寿命=%s%s%s\n    %s\n", x.Kind, x.Where, x.Lifetime, access, flags, x.Reason)
		}
	}
	if ds := model.DiagnosticsFor(m.plan, it.ID); len(ds) > 0 {
		fmt.Fprintf(b, "\n%s\n", m.st.header.Render("診断"))
		for _, d := range ds {
			fmt.Fprintf(b, "  [%s] %s: %s\n", d.Level, d.Code, d.Message)
		}
	}
}

func (m *Model) writeDiag(b *strings.Builder, d *sbxenv.Diagnostic) {
	fmt.Fprintf(b, "%s\n\nレベル: %s\n分類: %s\n\n%s\n", m.st.title.Render(d.Code), d.Level, d.Category, d.Message)
	if len(d.Origins) > 0 {
		fmt.Fprintf(b, "\n%s\n", m.st.header.Render("場所"))
		for _, o := range d.Origins {
			fmt.Fprintf(b, "  %s\n", model.FormatOrigin(m.idx, o))
		}
	}
	if d.ItemID != "" {
		for i := range m.plan.Items {
			if m.plan.Items[i].ID == d.ItemID {
				b.WriteString("\n")
				m.writeItem(b, &m.plan.Items[i])
				break
			}
		}
	}
}

func (m *Model) writeSource(b *strings.Builder, s *sbxenv.Source) {
	fmt.Fprintf(b, "%s\n\n種別: %s\nパス: %s\n", m.st.title.Render(s.ID), s.Kind, s.Path)
	if s.SHA256 != "" {
		fmt.Fprintf(b, "sha256: %s\n", s.SHA256)
	}
	if s.Layer > 0 {
		fmt.Fprintf(b, "マージ順: %d（大きいほど後勝ち）\n", s.Layer)
	}
	state := "展開済み"
	if !s.Expanded {
		state = "未展開"
	}
	fmt.Fprintf(b, "状態: %s %s\n", state, s.Status)
	n := 0
	for i := range m.plan.Items {
		if model.TouchesSource(&m.plan.Items[i], s.ID) {
			n++
		}
	}
	fmt.Fprintf(b, "この入力が関わる項目: %d 件（f で絞り込み）\n", n)
	var lines []string
	for i := range m.plan.Items {
		it := &m.plan.Items[i]
		for _, h := range it.History {
			if h.Origin.SourceID == s.ID && h.Origin.Line > 0 {
				lines = append(lines, fmt.Sprintf("  %5d  %s", h.Origin.Line, it.ID))
				break
			}
		}
	}
	if len(lines) > 0 {
		fmt.Fprintf(b, "\n%s\n%s\n", m.st.header.Render("行番号"), strings.Join(lines, "\n"))
	}
}

func (m *Model) helpText(w, h int) string {
	t := `キー操作
  1 / 2 / 3, [ / ]   ビュー切替（Risk / Items / Sources）
  j / k, ↑ / ↓       移動（詳細ペインではスクロール）
  h / l              ペイン移動（80 桁以下の 1 ペインでは戻る / 詳細）
  Enter / Esc        詳細を開く / 戻る・検索解除
  g                  Items のグループ切替（層別 / 境界別 / kit別）
  f                  レイヤー絞り込み（入力ファイル・kit を順に切替）
  /                  検索（Enter で確定、Esc で解除）
  ?                  このヘルプ
  q                  終了（1 行要約を出力）

バッジ
  [HOST] ホストで実行   [OPAQUE] 中身を解析しないコマンド   [RW]/[RO] マウント
  [PERSIST] ホストに残る   [NET] 外向き通信の許可   [CRED] 資格情報経路
  [SSH] SSH エージェント   [SKIP] --skip-host-commands で無効   [!] 診断あり
  [UNKNOWN] 未解析   [LITERAL] 平文の秘密

これは宣言の静的解析です。緑の「安全」表示は出しません。
未展開の kit・テンプレート・ホスト共通ポリシーは Coverage として常に表示します。`
	return lipgloss.NewStyle().Width(w).MaxHeight(h).Render(t)
}

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	s = trunc(s, w)
	if n := lipgloss.Width(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

// Run starts the TUI and returns the one-line summary to print on exit.
func Run(p *sbxenv.Plan, in io.Reader, out io.Writer) (string, error) {
	noColor := os.Getenv("NO_COLOR") != ""
	m := New(p, noColor)
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		return "", err
	}
	return ExitSummary(p), nil
}

// ExitSummary is printed to stdout after the TUI closes.
func ExitSummary(p *sbxenv.Plan) string {
	return "sbx-env-viz: " + sbxenv.SummaryLine(p.Summary)
}
