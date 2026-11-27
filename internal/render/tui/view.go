// view.go is the View half of Model: it draws the frame and records the
// click zones that tui.go's Update resolves, and it draws the Overview
// manifest overview.go builds, so the three files read and call each other.
//
//declscope:namespace tui // one Bubble Tea Model split across tui.go, view.go and overview.go

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
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
	zoneTarget
	zoneTargetRow
)

// zone is a clickable rectangle of the last frame, half-open on both axes.
type zone struct {
	x0, y0, x1, y1 int
	kind           zoneKind
	value          int
}

// zoneAt returns the most specific zone under (x, y): zones recorded later
// (rows, the back link) sit on top of the panes that contain them.
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
	lines := []string{m.titleBar(w)}
	if len(m.targets) > 1 {
		if bar, ok := m.targetBar(w, len(lines)); ok {
			lines = append(lines, bar)
		}
	}
	lines = append(lines, m.contextBar(w, len(lines)))
	if state := m.stateLine(); state != "" {
		lines = append(lines, trunc(" "+m.pal.strong.Render(state), w))
	}
	lines = append(lines, "")
	top := len(lines)
	ph := max(h-1-top, 4)
	lines = append(lines, strings.Split(m.panes(w, top, ph), "\n")...)
	lines = append(lines, m.statusBar(w))
	return strings.Join(lines, "\n")
}

// viewTotal is the count a tab shows next to its name.
func (m *Model) viewTotal(v viewID) int {
	switch v {
	case viewRisk:
		return len(m.drifts) + m.tally.findings
	case viewItems:
		return len(m.plan.Items)
	case viewSources:
		n := len(m.plan.Meta.Sources)
		for i := range m.plan.Coverage {
			if sbxenv.CoverageFromFile(m.plan.Coverage[i].Kind) {
				n++
			}
		}
		return n
	}
	return -1
}

// titleBar is the brand, the sandbox it describes, and the numbered tabs,
// right-aligned when they fit.
func (m *Model) titleBar(w int) string {
	var name, state, pos string
	if n := m.subject(); n != "" {
		name = "  " + model.EscapeControl(n)
	}
	if t := m.target(); t != nil {
		state = "  " + t.State
		if len(m.targets) > 1 {
			if _, ok := m.targetBar(w, 1); !ok {
				pos = fmt.Sprintf(" %d/%d", m.tIdx+1, len(m.targets))
			}
		}
	}
	var tabs []string
	for i, n := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if c := m.viewTotal(viewID(i)); c >= 0 {
			label += fmt.Sprintf(" %d", c)
		}
		tabs = append(tabs, label)
	}
	tabW := 0
	for _, t := range tabs {
		tabW += lipgloss.Width(t) + 3
	}
	// The tabs keep their place. When the rest does not fit beside them,
	// the state gives way first (the Overview repeats it), then the name;
	// the position stays.
	room := w - tabW - 3 - lipgloss.Width(" sbxray") - lipgloss.Width(pos)
	if over := lipgloss.Width(name) + lipgloss.Width(state) - room; over > 0 {
		if sw := lipgloss.Width(state) - over; sw >= 6 {
			state = trunc(state, sw)
		} else {
			state = ""
			name = trunc(name, max(room, 0))
		}
	}
	left := " " + m.pal.strong.Render("sbxray") + name + m.pal.muted.Render(state+pos)
	gap := w - lipgloss.Width(left) - tabW
	s := &strip{m: m, y: 0}
	s.text(left, lipgloss.NewStyle())
	s.text(strings.Repeat(" ", max(gap, 3)), lipgloss.NewStyle())
	for i, t := range tabs {
		st := m.pal.muted
		if viewID(i) == m.view {
			st = m.pal.tabActive
		}
		s.zone(t, st, zoneTab, i)
		s.text("   ", lipgloss.NewStyle())
	}
	return trunc(s.b.String(), w)
}

// subject is the name the title bar gives: the discovered sandbox, or the
// declared name.
func (m *Model) subject() string {
	if t := m.target(); t != nil {
		return t.Name
	}
	for i := range m.plan.Items {
		if m.plan.Items[i].ID == "name" {
			return m.plan.Items[i].Value
		}
	}
	return ""
}

// targetBar lists the discovered sandboxes; a click switches to one. It
// reports false when they do not all fit, and the title bar then names the
// position instead ("1/3"; the footer offers s to switch).
func (m *Model) targetBar(w, y int) (string, bool) {
	zones := len(m.zones)
	s := &strip{m: m, y: y}
	s.text(" 対象  ", m.pal.muted)
	for i := range m.targets {
		t := &m.targets[i]
		label := model.EscapeControl(t.Name)
		note := t.State
		if n := t.Drifts(); n > 0 {
			note += fmt.Sprintf("・ズレ %d", n)
		}
		st := m.pal.ink
		if i == m.tIdx {
			st = m.pal.tabActive
		}
		s.zone(label, st, zoneTarget, i)
		s.text("（"+note+"）  ", m.pal.muted)
	}
	if lipgloss.Width(s.b.String()) > w {
		m.zones = m.zones[:zones]
		return "", false
	}
	return s.b.String(), true
}

// contextBar says where the data comes from, and on list views carries the
// grouping and filter switches.
func (m *Model) contextBar(w, y int) string {
	s := &strip{m: m, y: y}
	files, kits := 0, 0
	for _, src := range m.plan.Meta.Sources {
		switch src.Kind {
		case sbxenv.SourceEnv, sbxenv.SourceUserDefaults:
			files++
		case sbxenv.SourceKit:
			kits++
		}
	}
	notice := fmt.Sprintf(" 宣言 %d ファイル・Kit %d を静的に解析（稼働中の状態ではありません）", files, kits)
	if t := m.target(); t != nil {
		switch {
		case t.Comparison != nil:
			notice = fmt.Sprintf(" 宣言 %d ファイルと稼働中の sandbox（sbx の読み取り専用コマンド）を照合", files)
		case t.Plan == nil:
			notice = " 稼働中の実体のみ（宣言ファイル未検出）"
		}
	}
	if m.view == viewOverview {
		s.text(notice, m.pal.muted)
		return trunc(s.b.String(), w)
	}
	if m.view == viewItems {
		s.text(" グループ ", m.pal.muted)
		for i, n := range groupNames {
			st := m.pal.muted
			if groupMode(i) == m.group {
				st = m.pal.tabActive
			}
			s.text(" ", lipgloss.NewStyle())
			s.zone(n, st, zoneGroup, i)
		}
		s.text("    ", lipgloss.NewStyle())
	}
	s.text(" 絞り込み ", m.pal.muted)
	for i, f := range m.filters {
		label := f
		if f == "" {
			label = "全て"
		}
		st := m.pal.muted
		if i == m.filterIdx {
			st = m.pal.tabActive
		}
		s.text(" ", lipgloss.NewStyle())
		s.zone(label, st, zoneFilter, i)
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
	if m.liveNote != "" {
		state = append(state, m.liveNote)
	}
	if t := m.target(); t != nil && t.Err != "" && t.Err != m.liveNote {
		state = append(state, model.EscapeControl(t.Err))
	}
	if m.searching {
		state = append(state, "検索: "+m.query+"_")
	} else if m.query != "" {
		state = append(state, "検索: "+m.query+"（Esc で解除）")
	}
	return strings.Join(state, "  ")
}

// statusBar is one line of the keys that matter where the user is.
func (m *Model) statusBar(w int) string {
	var keys [][2]string
	switch {
	case m.help:
		keys = [][2]string{{"j/k", "スクロール"}, {"ほかのキー", "閉じる"}}
	case m.picker:
		keys = [][2]string{{"j/k", "選択"}, {"enter", "切替"}, {"esc", "閉じる"}}
	case m.inDetail():
		keys = [][2]string{{"j/k", "スクロール"}, {"esc", "戻る"}}
	default:
		keys = [][2]string{{"j/k", "移動"}, {"enter", "詳細"}, {"/", "検索"}}
		if m.view == viewItems {
			keys = append(keys, [2]string{"g", "グループ"})
		}
		if m.view != viewOverview {
			keys = append(keys, [2]string{"f", "絞り込み"})
		}
		if m.multi() {
			keys = append(keys, [2]string{"s", "対象"})
		}
	}
	keys = append(keys, [2]string{"?", "ヘルプ"}, [2]string{"q", "終了"})
	var b strings.Builder
	b.WriteString(" ")
	for i, kv := range keys {
		if i > 0 {
			b.WriteString("   ")
		}
		b.WriteString(m.pal.key.Render(kv[0]))
		b.WriteString(" " + m.pal.muted.Render(kv[1]))
	}
	return trunc(b.String(), w)
}

// panes draws the list and detail (or the help or picker) starting at
// screen row top, ph rows tall, and records their zones.
func (m *Model) panes(w, top, ph int) string {
	switch {
	case m.help:
		return frame(m.helpLines(w-2, ph), w, ph)
	case m.picker:
		return frame(m.pickerLines(w-2, top), w, ph)
	case m.view == viewOverview:
		return m.overviewPanes(w, top, ph)
	case m.twoPane():
		lw := w * 45 / 100
		rw := w - lw - 3
		m.addZone(0, top, lw, top+ph, zoneListPane, 0)
		m.addZone(lw, top, w, top+ph, zoneDetailPane, 0)
		left := strings.Split(frame(m.listLines(lw, ph, 0, top), lw, ph), "\n")
		right := strings.Split(frame(m.detailLines(rw, ph, top, lw+3), rw, ph), "\n")
		rule := m.pal.rule.Render("|")
		out := make([]string, ph)
		for i := range out {
			out[i] = left[i] + " " + rule + " " + right[i]
		}
		return strings.Join(out, "\n")
	case m.detail:
		m.addZone(0, top, w, top+ph, zoneDetailPane, 0)
		return frame(m.detailLines(w, ph, top, 0), w, ph)
	default:
		m.addZone(0, top, w, top+ph, zoneListPane, 0)
		return frame(m.listLines(w, ph, 0, top), w, ph)
	}
}

// frame pads content to exactly ww × hh cells.
func frame(content []string, ww, hh int) string {
	out := make([]string, hh)
	for i := range out {
		l := ""
		if i < len(content) {
			l = content[i]
		}
		out[i] = pad(l, ww)
	}
	return strings.Join(out, "\n")
}

// listColumns is the column-header row of each list view, and the width
// the first column takes.
func (m *Model) listColumns(iw int) (string, int) {
	keyW := 0
	if m.view == viewItems {
		for _, r := range m.rows {
			if k, _, ok := strings.Cut(r.label, " = "); ok && !r.header {
				keyW = max(keyW, widthOf(k))
			}
		}
		keyW = min(keyW, iw*50/100)
		return "    " + pad("設定", keyW) + "  値", keyW
	}
	switch m.view {
	case viewRisk:
		return "    危険度  内容", 0
	case viewSources:
		return "    " + pad("ID", 7) + "  " + pad("状態", 8) + "  ファイル", 0
	}
	return "", 0
}

// listLines renders a list view; x0/top locate it for row zones.
func (m *Model) listLines(iw, ih, x0, top int) []string {
	p := m.pal
	head, keyW := m.listColumns(iw)
	lines := []string{p.colHead.Render(pad(head, iw-1))}
	if len(m.rows) == 0 {
		return append(lines, p.muted.Render("    （該当なし）"))
	}
	vis := max(ih-1, 1)
	if len(m.rows) > vis {
		vis = max(vis-1, 1) // the last line says how many rows follow
	}
	cur := m.cursor[m.view]
	start := 0
	if cur >= vis {
		start = cur - vis + 1
	}
	listFocused := !m.twoPane() || m.focus == 0
	for i := start; i < len(m.rows) && len(lines) <= vis; i++ {
		r := m.rows[i]
		y := top + len(lines)
		m.addZone(x0, y, x0+iw, y+1, zoneRow, i)
		if r.header {
			lines = append(lines, trunc(" "+p.strong.Render(r.label), iw))
			continue
		}
		text, note := r.label, r.note
		if m.view == viewItems && keyW > 0 {
			if k, v, ok := strings.Cut(r.label, " = "); ok {
				text, note = pad(truncMiddle(k, keyW), keyW), v
			}
		}
		if note != "" && m.view == viewRisk {
			text = truncMiddle(text, max(iw*70/100-10, 12))
		}
		if m.view == viewRisk {
			text = pad(sbxenv.SeverityLabel(r.sev), 6) + "  " + text
		}
		plain := text
		if note != "" {
			plain += "  " + note
		}
		switch {
		case i == cur && listFocused:
			lines = append(lines, p.selected.Render(pad(" "+markText(r.sev)+"  "+plain, iw-1)))
		case i == cur:
			lines = append(lines, trunc(p.cursor.Render("›")+p.mark(r.sev)+"  "+p.strong.Render(trunc(plain, iw-4)), iw))
		default:
			s := " " + p.mark(r.sev) + "  " + text
			if note != "" {
				s += "  " + p.muted.Render(note)
			}
			lines = append(lines, trunc(s, iw))
		}
	}
	if rest := len(m.rows) - start - (len(lines) - 1); rest > 0 {
		lines = append(lines, p.muted.Render(fmt.Sprintf("    ほか %d 行（j で下へ）", rest)))
	}
	return lines
}

func (m *Model) current() *row {
	if len(m.rows) == 0 {
		return nil
	}
	return &m.rows[m.cursor[m.view]]
}

// detailLines renders the detail pane. Shown alone it starts with a
// clickable link back to the list.
func (m *Model) detailLines(iw, ih, top, x0 int) []string {
	var d doc
	switch r := m.current(); {
	case r == nil:
		d.text("（項目がありません）", 0)
	case r.item != nil:
		m.itemDoc(&d, r.item)
	case r.diag != nil:
		m.diagDoc(&d, r.diag, r.sev)
	case r.cover != nil:
		m.coverageDoc(&d, r.cover)
	case r.source != nil:
		m.sourceDoc(&d, r.source)
	case r.drift != nil:
		driftDoc(&d, r.drift)
	}
	var lines []string
	if !m.twoPane() {
		back := "< 一覧に戻る（esc）"
		m.addZone(x0+1, top, x0+1+lipgloss.Width(back), top+1, zoneBack, 0)
		lines = append(lines, " "+m.pal.accent.Render(back), "")
	}
	body := d.render(m.pal, iw-2)
	for len(body) > 0 && strings.TrimSpace(ansi.Strip(body[len(body)-1])) == "" {
		body = body[:len(body)-1]
	}
	vis := ih - len(lines)
	if limit := max(0, len(body)-vis); m.dScroll > limit {
		m.dScroll = limit
	}
	body = body[m.dScroll:]
	if len(body) > vis {
		more := len(body) - vis + 1
		body = append(body[:vis-1:vis-1], m.pal.muted.Render(fmt.Sprintf("ほか %d 行（j でスクロール）", more)))
	}
	for _, l := range body {
		lines = append(lines, " "+l)
	}
	return lines
}

// pickerLines lists every target with its state, files, and drift count;
// a click or Enter on a row switches to it.
func (m *Model) pickerLines(iw, top int) []string {
	p := m.pal
	lines := []string{" " + p.title.Render("対象の sandbox を選ぶ"), ""}
	lines = append(lines, p.colHead.Render(pad("    "+pad("名前", 18)+"  "+pad("状態", 24)+"  "+pad("ズレ", 4)+"  ファイル", iw)))
	for i := range m.targets {
		t := &m.targets[i]
		files := "（設定ファイルなし）"
		if len(t.Files) > 0 {
			files = strings.Join(t.Files, " + ")
		}
		drift := "-"
		if t.Comparison != nil {
			drift = fmt.Sprint(t.Drifts())
		}
		text := pad(model.EscapeControl(t.Name), 18) + "  " + pad(t.State, 24) + "  " + pad(drift, 4) + "  " + model.EscapeControl(files)
		y := top + len(lines)
		m.addZone(1, y, 1+iw, y+1, zoneTargetRow, i)
		if i == m.pCursor {
			lines = append(lines, p.selected.Render(pad("    "+text, iw)))
		} else {
			lines = append(lines, trunc("    "+text, iw))
		}
	}
	return lines
}

// helpLines renders the help, scrolled by j/k, with a closing line when
// more follows.
func (m *Model) helpLines(iw, ih int) []string {
	body := helpDoc().render(m.pal, iw-1)
	if limit := max(0, len(body)-ih+1); m.hScroll > limit {
		m.hScroll = limit
	}
	body = body[m.hScroll:]
	if len(body) > ih {
		more := len(body) - ih + 1
		body = append(body[:ih-1:ih-1], m.pal.muted.Render(fmt.Sprintf("ほか %d 行（j でスクロール）", more)))
	}
	out := make([]string, 0, len(body))
	for _, l := range body {
		out = append(out, " "+l)
	}
	return out
}

func widthOf(s string) int { return lipgloss.Width(s) }

// truncMiddle shortens s to w cells by cutting its middle, so the start
// and the last path segment, where keys differ, both stay readable.
func truncMiddle(s string, w int) string {
	if lipgloss.Width(s) <= w || w < 8 {
		return trunc(s, w)
	}
	tail := w * 2 / 3
	head := w - tail - 2
	r := []rune(s)
	back := ""
	for i := len(r) - 1; i >= 0 && lipgloss.Width(string(r[i:])) <= tail; i-- {
		back = string(r[i:])
	}
	return ansi.Truncate(s, head, "") + ".." + back
}

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "..")
}

func pad(s string, w int) string {
	s = trunc(s, w)
	if n := lipgloss.Width(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}
