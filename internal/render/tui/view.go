package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// zoneKind says what a click or wheel event on a zone means.
type zoneKind int

const (
	zoneTab zoneKind = iota
	zoneGroup
	zoneFilter
	zoneListPane
	zoneDetailPane
	zoneRow
	zoneBack
)

// zone is a clickable rectangle of the last frame, half-open on both axes.
type zone struct {
	x0, y0, x1, y1 int
	kind           zoneKind
	value          int
}

// zoneAt returns the most specific zone under (x, y): zones recorded later
// (rows, the back chip) sit on top of the panes that contain them.
func (m *Model) zoneAt(x, y int) (zone, bool) {
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := m.zones[i]
		if x >= z.x0 && x < z.x1 && y >= z.y0 && y < z.y1 {
			return z, true
		}
	}
	return zone{}, false
}

func (m *Model) addZone(x0, y0, x1, y1 int, kind zoneKind, value int) {
	m.zones = append(m.zones, zone{x0, y0, x1, y1, kind, value})
}

// strip builds one screen row from styled segments and records the zone of
// every clickable segment at its exact columns.
type strip struct {
	m *Model
	y int
	x int
	b strings.Builder
}

func (s *strip) text(t string, st lipgloss.Style) {
	r := st.Render(t)
	s.b.WriteString(r)
	s.x += lipgloss.Width(r)
}

func (s *strip) zone(t string, st lipgloss.Style, kind zoneKind, value int) {
	x0 := s.x
	s.text(t, st)
	s.m.addZone(x0, s.y, s.x, s.y+1, kind, value)
}

func (m *Model) chipText(t string) string {
	if m.pal.noColor {
		return "[" + t + "]"
	}
	return t
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
	m.zones = nil
	lines := []string{m.titleBar(w), m.summaryBar(w), m.contextBar(w), m.chipBar(w)}
	if state := m.stateLine(); state != "" {
		lines = append(lines, trunc(m.pal.bold.Render(state), w))
	}
	top := len(lines)
	ph := h - 1 - top
	if ph < 4 {
		ph = 4
	}
	lines = append(lines, strings.Split(m.panes(w, top, ph), "\n")...)
	lines = append(lines, m.statusBar(w))
	return strings.Join(lines, "\n")
}

func (m *Model) titleBar(w int) string {
	s := &strip{m: m, y: 0}
	s.text("sbx-env-viz", m.pal.brand)
	s.text(" ", lipgloss.NewStyle())
	for i, n := range viewNames {
		st := m.pal.tab
		if viewID(i) == m.view {
			st = m.pal.tabActive
		}
		s.zone(n, st, zoneTab, i)
		s.text(" ", lipgloss.NewStyle())
	}
	return trunc(s.b.String(), w)
}

func (m *Model) summaryBar(w int) string {
	sum := m.plan.Summary
	chips := []struct {
		label string
		n     int
		alert bool
	}{
		{"Host実行", sum.HostExec, sum.HostExec > 0},
		{"Host永続", sum.HostPersistent, sum.HostPersistent > 0},
		{"RWマウント", sum.RWMounts, sum.RWMounts > 0},
		{"外への許可", sum.EgressAllows, false},
		{"エラー", sum.Errors, sum.Errors > 0},
		{"警告", sum.Warnings, sum.Warnings > 0},
		{"未展開", sum.Unexpanded, false},
	}
	var full []string
	for _, c := range chips {
		st := m.pal.chip
		if c.alert {
			st = m.pal.chipAlert
		}
		full = append(full, st.Render(m.chipText(fmt.Sprintf("%s %d", c.label, c.n))))
	}
	line := strings.Join(full, " ")
	if lipgloss.Width(line) <= w {
		return line
	}
	// Too narrow for pills: keep every count in a compact line.
	compact := fmt.Sprintf("Host実行%d 永続%d RW%d 許可%d E%d W%d 未展開%d", sum.HostExec, sum.HostPersistent, sum.RWMounts, sum.EgressAllows, sum.Errors, sum.Warnings, sum.Unexpanded)
	return trunc(m.pal.summaryValue.Render(compact), w)
}

func (m *Model) contextBar(w int) string {
	return trunc(m.pal.dim.Render("宣言の静的解析（稼働中 sandbox の状態ではない） ")+model.ArgsLine(m.plan), w)
}

func (m *Model) chipBar(w int) string {
	s := &strip{m: m, y: 3}
	if m.view == viewItems {
		s.text("グループ ", m.pal.chipLabel)
		for i, n := range groupNames {
			st := m.pal.chip
			if groupMode(i) == m.group {
				st = m.pal.chipActive
			}
			s.zone(n, st, zoneGroup, i)
			s.text(" ", lipgloss.NewStyle())
		}
		s.text(" ", lipgloss.NewStyle())
	}
	s.text("絞り込み ", m.pal.chipLabel)
	for i, f := range m.filters {
		label := f
		if f == "" {
			label = "全て"
		}
		st := m.pal.chip
		if i == m.filterIdx {
			st = m.pal.chipActive
		}
		s.zone(label, st, zoneFilter, i)
		s.text(" ", lipgloss.NewStyle())
	}
	return trunc(s.b.String(), w)
}

// stateLine reports an active filter or search; empty when neither is on.
func (m *Model) stateLine() string {
	p := m.plan
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
		state = append(state, fmt.Sprintf("絞り込み（非表示の警告・エラー %d 件）: %s", hidden, label))
	}
	if m.searching {
		state = append(state, "検索: "+m.query+"▏")
	} else if m.query != "" {
		state = append(state, "検索: "+m.query+"（Esc で解除）")
	}
	return strings.Join(state, "  ")
}

func (m *Model) statusBar(w int) string {
	var b strings.Builder
	for _, kv := range [][2]string{
		{"q", "終了"}, {"?", "ヘルプ"}, {"1-3", "ビュー"}, {"j/k", "移動"}, {"Enter", "詳細"},
		{"h/l", "ペイン"}, {"g", "グループ"}, {"f", "絞り込み"}, {"/", "検索"},
	} {
		b.WriteString(m.pal.keyCap.Render(kv[0]))
		b.WriteString(m.pal.keyText.Render(" " + kv[1] + " "))
	}
	b.WriteString(m.pal.dim.Render(" マウス: クリックで選択・ホイールでスクロール"))
	return trunc(b.String(), w)
}

// panes draws the list and detail panels (or the help panel) starting at
// screen row top, ph rows tall, and records their zones.
func (m *Model) panes(w, top, ph int) string {
	switch {
	case m.help:
		return m.box(w, ph, m.helpLines(w-2), true)
	case m.twoPane():
		lw := w * 45 / 100
		rw := w - lw
		m.addZone(0, top, lw, top+ph, zoneListPane, 0)
		m.addZone(lw, top, w, top+ph, zoneDetailPane, 0)
		left := m.box(lw, ph, m.listLines(lw-2, ph-2, 0, top), m.focus == 0)
		right := m.box(rw, ph, m.detailLines(rw-2, ph-2, top, lw), m.focus == 1)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case m.detail:
		m.addZone(0, top, w, top+ph, zoneDetailPane, 0)
		return m.box(w, ph, m.detailLines(w-2, ph-2, top, 0), true)
	default:
		m.addZone(0, top, w, top+ph, zoneListPane, 0)
		return m.box(w, ph, m.listLines(w-2, ph-2, 0, top), true)
	}
}

// box frames content lines in a rounded border, ww wide and hh tall; the
// focused panel's border uses the accent colour.
func (m *Model) box(ww, hh int, content []string, focused bool) string {
	iw, ih := ww-2, hh-2
	if iw < 1 {
		iw = 1
	}
	out := make([]string, 0, ih)
	for i := 0; i < ih; i++ {
		l := ""
		if i < len(content) {
			l = content[i]
		}
		out = append(out, pad(l, iw))
	}
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	if !m.pal.noColor {
		if focused {
			st = st.BorderForeground(m.pal.borderFocus)
		} else {
			st = st.BorderForeground(m.pal.borderBlur)
		}
	}
	return st.Render(strings.Join(out, "\n"))
}

func (m *Model) paneTitle(name string, focused bool) string {
	marker := "  "
	if focused {
		marker = "● "
	}
	return m.pal.title.Render(marker + name)
}

// listLines renders the list panel body; x0/top locate it for row zones.
func (m *Model) listLines(iw, ih, x0, top int) []string {
	count := 0
	for _, r := range m.rows {
		if !r.header {
			count++
		}
	}
	title := fmt.Sprintf("一覧 · %s（%d 件）", strings.SplitN(viewNames[m.view], ":", 2)[1], count)
	lines := []string{m.paneTitle(title, m.focus == 0 || !m.twoPane())}
	if len(m.rows) == 0 {
		return append(lines, m.pal.dim.Render(" (該当なし)"))
	}
	vis := ih - 1
	if vis < 1 {
		vis = 1
	}
	cur := m.cursor[m.view]
	start := 0
	if cur >= vis {
		start = cur - vis + 1
	}
	for i := start; i < len(m.rows) && len(lines) <= vis; i++ {
		r := m.rows[i]
		y := top + 1 + len(lines)
		m.addZone(x0+1, y, x0+1+iw, y+1, zoneRow, i)
		if r.header {
			lines = append(lines, trunc(m.pal.groupHeader.Render("▸ "+r.label), iw))
			continue
		}
		badges := strings.Join(r.badges, "")
		if i == cur {
			plain := sevTag(r.sev) + " " + strings.TrimSpace(badges+" "+r.label)
			lines = append(lines, m.pal.selected.Render(pad(trunc(plain, iw), iw)))
			continue
		}
		styled := m.pal.sev[r.sev].Render(sevTag(r.sev)) + " "
		if badges != "" {
			styled += m.pal.badges(r.badges) + " "
		}
		lines = append(lines, trunc(styled+r.label, iw))
	}
	return lines
}

func (m *Model) current() *row {
	if len(m.rows) == 0 {
		return nil
	}
	return &m.rows[m.cursor[m.view]]
}

// detailLines renders the detail panel body. In the single-pane layout its
// title carries a clickable "← 戻る" chip.
func (m *Model) detailLines(iw, ih, top, x0 int) []string {
	var d doc
	switch r := m.current(); {
	case r == nil:
		d.text("(項目がありません)", 0)
	case r.item != nil:
		m.itemDoc(&d, r.item)
	case r.diag != nil:
		m.diagDoc(&d, r.diag)
	case r.cover != nil:
		m.coverageDoc(&d, r.cover)
	case r.source != nil:
		m.sourceDoc(&d, r.source)
	}
	focused := m.focus == 1 || !m.twoPane()
	title := m.paneTitle("詳細", focused)
	if !m.twoPane() {
		back := m.pal.chip.Render(m.chipText("← 戻る"))
		x := x0 + 1 + lipgloss.Width(title) + 1
		m.addZone(x, top+1, x+lipgloss.Width(back), top+2, zoneBack, 0)
		title += " " + back
	}
	body := d.render(m.pal, iw-1)
	for len(body) > 0 && strings.TrimSpace(ansi.Strip(body[len(body)-1])) == "" {
		body = body[:len(body)-1]
	}
	vis := ih - 1
	if limit := max(0, len(body)-vis); m.dScroll > limit {
		m.dScroll = limit
	}
	body = body[m.dScroll:]
	if len(body) > vis {
		body = body[:vis]
	}
	lines := []string{title}
	for _, l := range body {
		lines = append(lines, " "+l)
	}
	return lines
}

func (m *Model) helpLines(iw int) []string {
	return append([]string{m.paneTitle("ヘルプ（どのキーでも閉じる）", true)}, helpDoc().render(m.pal, iw-1)...)
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
