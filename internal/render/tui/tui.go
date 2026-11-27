// Package tui is the interactive Bubble Tea v2 view of a Plan.
//
// Views: 1 概要 (default; a manifest of what is declared and, with live
// data, what sbx has, that jumps to details), 2 リスク, 3 設定 (g cycles
// layer / boundary / kit grouping), 4 ファイル. At 120 columns or wider the
// list and the detail pane sit side by side; narrower terminals show one
// pane at a time. Tabs, switches, list rows, and panes respond to the mouse.
package tui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// twoPaneMinWidth is the narrowest width that shows list and detail together.
const twoPaneMinWidth = 120

type viewID int

const (
	viewOverview viewID = iota
	viewRisk
	viewItems
	viewSources
	viewCount
)

var viewNames = []string{"概要", "リスク", "設定", "ファイル"}

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
	// note follows the label in muted ink (a risky item's value).
	note string
	// key is a 設定 row's setting ID, the part of label before " = "; it
	// is kept apart because the ID itself may hold " = " (a mount target).
	key    string
	badges []string
	sev    sbxenv.Severity
	item   *sbxenv.Item
	diag   *sbxenv.Diagnostic
	cover  *sbxenv.Coverage
	source *sbxenv.Source
	drift  *live.Drift
}

// Model is the Bubble Tea model.
type Model struct {
	plan    *sbxenv.Plan             //declscope:shared // detail.go builds the detail docs from the plan
	idx     map[string]sbxenv.Source //declscope:shared // detail.go's origin resolves source IDs to paths with it
	view    viewID
	group   groupMode
	cursor  [viewCount]int
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

	pal      palette
	rows     []row
	manifest []manifestLine

	// targets are the sandboxes found by discovery; empty for a single
	// plan given on the command line.
	targets []live.Target
	tIdx    int
	// liveNote explains in Japanese why live data is missing or partial.
	liveNote string
	picker   bool
	pCursor  int
	drifts   []live.Drift
	// driftItem is the plan item each drift is about (nil for none), and
	// compared the items the comparison held against sbx. Both are found
	// by the comparison's own IDs, not the escaped ones on screen, which a
	// tab and a literal \t share.
	driftItem []*sbxenv.Item
	compared  map[*sbxenv.Item]bool
	tally     riskTally
	// keyW is the widest setting key of the 設定 rows, measured when they
	// are built rather than on every frame.
	keyW int
	// oScroll is the first visible canvas line of the Overview; hScroll
	// is the first visible line of the help.
	oScroll, hScroll int
	// oExtra is how far the Overview is scrolled beyond the position that
	// keeps the cursor in view: j past the last entry, or k past the first.
	// oCursorShown tells whether the cursor row was on the last frame;
	// Enter opens nothing the user cannot see.
	oExtra       int
	oCursorShown bool
	// zones are the clickable regions of the last frame; mouse events are
	// resolved against them so drawing and hit-testing never disagree.
	zones []zone
}

// New builds a model. noColor disables all colours (NO_COLOR); structure,
// borders, bold, and reverse video remain.
func New(p *sbxenv.Plan, noColor bool) *Model {
	p = model.ForTerminal(p)
	m := &Model{plan: p, idx: model.SourceIndex(p), tally: tallyRisks(p), width: 100, height: 30, pal: newPalette(noColor)}
	m.resetFilters()
	m.rebuild()
	return m
}

// NewTargets builds a model that switches between discovered sandboxes.
// note explains, in Japanese, why live data is missing or partial.
func NewTargets(targets []live.Target, note string, noColor bool) *Model {
	m := &Model{targets: targets, liveNote: model.EscapeControl(note), width: 100, height: 30, pal: newPalette(noColor)}
	m.setTarget(0)
	return m
}

func (m *Model) resetFilters() {
	m.filters, m.filterIdx = []string{""}, 0
	for _, s := range m.plan.Meta.Sources {
		if s.Kind == sbxenv.SourceEnv || s.Kind == sbxenv.SourceUserDefaults || s.Kind == sbxenv.SourceKit {
			m.filters = append(m.filters, s.ID)
		}
	}
}

func (m *Model) multi() bool { return len(m.targets) > 0 }

func (m *Model) target() *live.Target {
	if !m.multi() {
		return nil
	}
	return &m.targets[m.tIdx]
}

// setTarget shows target i: its plan (an empty one for a sandbox without
// files) and its drift list, with every view reset.
func (m *Model) setTarget(i int) {
	m.tIdx = i
	t := &m.targets[i]
	p := t.Plan
	if p == nil {
		p = &sbxenv.Plan{Meta: sbxenv.Meta{Notice: "no environment file found for this sandbox"}}
	}
	m.plan = model.ForTerminal(p)
	m.idx = model.SourceIndex(m.plan)
	m.drifts, m.driftItem, m.compared = nil, nil, map[*sbxenv.Item]bool{}
	if t.Comparison != nil {
		// ForTerminal keeps the items in order, so the unescaped IDs
		// (unique, as addItem numbers repeats) find each one.
		byID := make(map[string]*sbxenv.Item, len(p.Items))
		for i := range p.Items {
			byID[p.Items[i].ID] = &m.plan.Items[i]
		}
		for _, d := range t.Comparison.Drifts {
			it := byID[d.ItemID]
			for _, f := range []*string{&d.Kind, &d.Title, &d.Declared, &d.Actual, &d.Fix, &d.Source, &d.ItemID} {
				*f = model.EscapeControl(*f)
			}
			m.drifts = append(m.drifts, d)
			m.driftItem = append(m.driftItem, it)
		}
		for _, id := range t.Comparison.Compared {
			if it := byID[id]; it != nil {
				m.compared[it] = true
			}
		}
	}
	m.tally = tallyRisks(m.plan)
	// The summary's counts take in the drifts too: a port open to every
	// network is as high a risk found live as declared.
	for _, d := range m.drifts {
		switch {
		case d.Severity.Rank() >= sbxenv.SeverityHigh.Rank():
			m.tally.high++
		case d.Severity == sbxenv.SeverityMedium:
			m.tally.medium++
		}
	}
	m.resetFilters()
	m.cursor = [viewCount]int{}
	m.query, m.searching, m.detail, m.focus, m.dScroll, m.oScroll, m.oExtra = "", false, false, 0, 0, 0, 0
	m.rebuild()
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) twoPane() bool { return m.width >= twoPaneMinWidth }

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

// relist rebuilds the list after its search, filter or grouping changed,
// and shows the list: the detail on screen belonged to the old one.
func (m *Model) relist() {
	m.detail, m.focus = false, 0
	m.rebuild()
}

func (m *Model) rebuild() {
	// The rows may change under the cursor, so a detail starts at its top.
	m.dScroll = 0
	var rows []row
	switch m.view {
	case viewOverview:
		m.buildOverview()
		rows = m.rows
	case viewRisk:
		// Three sections, each ordered by risk level: drift from the
		// running sandbox, this tool's findings, and risky settings.
		var drifts, diags, items []row
		if m.filter() == "" {
			for i := range m.drifts {
				d := &m.drifts[i]
				// The section's own word (ズレ) finds them, as do both sides.
				if m.matches("ズレ DRIFT " + d.Kind + " " + d.Title + " " + d.Declared + " " + d.Actual) {
					drifts = append(drifts, row{label: d.Kind + ": " + d.Title, badges: []string{"[DRIFT]"}, sev: d.Severity, drift: d})
				}
			}
			sort.SliceStable(drifts, func(i, j int) bool { return drifts[i].sev.Rank() > drifts[j].sev.Rank() })
		}
		for _, r := range model.RiskList(m.plan) {
			if r.Cover != nil {
				// Gaps are premises of a static analysis, not findings;
				// the explicit ones are listed quietly under Sources.
				continue
			}
			if f := m.filter(); f != "" {
				switch {
				case r.Item != nil && !model.TouchesSource(r.Item, f):
					continue
				case r.Diag != nil && !model.DiagTouchesSource(r.Diag, f):
					continue
				}
			}
			if r.Diag != nil {
				label := sbxenv.ExplainDiagnostic(r.Diag.Code).Title
				if m.matches(strings.Join(r.Badges, "") + " " + label + " " + r.Label) {
					diags = append(diags, row{label: label, badges: r.Badges, sev: r.Severity, diag: r.Diag})
				}
				continue
			}
			if m.matches(strings.Join(r.Badges, "") + " " + r.Label) {
				items = append(items, row{label: r.Item.ID, note: r.Item.Value, badges: r.Badges, sev: r.Severity, item: r.Item})
			}
		}
		for _, sec := range []struct {
			title string
			rows  []row
		}{{"sbx とのズレ", drifts}, {"このツールの指摘", diags}, {"設定ごとの危険度", items}} {
			if len(sec.rows) > 0 {
				rows = append(rows, row{header: true, label: fmt.Sprintf("%s（%d 件）", sec.title, len(sec.rows))})
				rows = append(rows, sec.rows...)
			}
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
				id := it.ID
				if m.group != groupBoundary && it.Kit != "" {
					// The group header already names the kit.
					id = strings.TrimPrefix(id, "kit:"+it.Kit+"/")
				}
				rows = append(rows, row{label: id + " = " + it.Value, key: id, badges: it.Badges, sev: it.Risk, item: it})
			}
		}
	case viewSources:
		for i := range m.plan.Meta.Sources {
			s := &m.plan.Meta.Sources[i]
			if f := m.filter(); f != "" && s.ID != f {
				continue
			}
			state := "展開済み"
			if !s.Expanded {
				state = "未展開"
			}
			label := pad(s.ID, 7) + "  " + pad(state, 8) + "  " + s.Path + "（" + sbxenv.SourceKindLabel(s.Kind) + "）"
			if !m.matches(label) {
				continue
			}
			rows = append(rows, row{label: label, sev: sbxenv.SeverityInfo, source: s})
		}
		for i := range m.plan.Coverage {
			c := &m.plan.Coverage[i]
			if !sbxenv.CoverageFromFile(c.Kind) {
				continue
			}
			if f := m.filter(); f != "" && c.Origin.SourceID != f {
				continue
			}
			label := pad("-", 7) + "  " + pad("未展開", 8) + "  " + c.Ref + "（" + sbxenv.CoverageTitle(c.Kind) + "）"
			if m.matches(label) {
				rows = append(rows, row{label: label, sev: sbxenv.SeverityInfo, cover: c})
			}
		}
	}
	m.rows = rows
	m.keyW = 0
	if m.view == viewItems {
		for _, r := range rows {
			if r.key != "" && !r.header {
				m.keyW = max(m.keyW, widthOf(r.key))
			}
		}
	}
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
		return "境界: " + sbxenv.BoundaryTitle(bs[0].Kind)
	case groupKit:
		if it.Kit != "" {
			return "kit: " + it.Kit
		}
		return "kit: （環境ファイルに直接書いた設定）"
	}
	o := model.LastOrigin(it)
	if s, ok := m.idx[o.SourceID]; ok {
		return fmt.Sprintf("層 %s %s", s.ID, s.Path)
	}
	return "層 " + o.SourceID
}

func (m *Model) move(delta int) {
	// On the Overview, a move back toward the cursor first undoes a scroll
	// past the ends, so k after j past the end returns through the lines
	// it passed instead of jumping to the entry above.
	if m.view == viewOverview && (m.oExtra > 0 && delta < 0 || m.oExtra < 0 && delta > 0) {
		m.oExtra += delta
		return
	}
	before, step := m.cursor[m.view], delta
	defer func() {
		if m.view == viewOverview {
			m.scrollOverview(step, m.cursor[m.view] != before)
		}
	}()
	if len(m.rows) == 0 {
		return
	}
	// The next selectable row that way; at either end, or when only
	// headers lie that way, the cursor stays where it is (or, on a header,
	// goes to the nearest selectable row).
	c := m.cursor[m.view]
	next := -1
	for i := c + delta; i >= 0 && i < len(m.rows); i += delta {
		if !m.rows[i].header {
			next = i
			break
		}
	}
	if next < 0 && m.rows[min(max(c, 0), len(m.rows)-1)].header {
		for i := c - delta; i >= 0 && i < len(m.rows); i -= delta {
			if !m.rows[i].header {
				next = i
				break
			}
		}
	}
	if next >= 0 {
		m.cursor[m.view] = next
	}
	m.dScroll = 0
}

// selectRow puts the cursor on row i if it is selectable.
func (m *Model) selectRow(i int) bool {
	if i < 0 || i >= len(m.rows) || m.rows[i].header {
		return false
	}
	if m.cursor[m.view] != i {
		m.cursor[m.view] = i
		m.dScroll = 0
	}
	return true
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m.key(msg.String())
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			m.click(msg.X, msg.Y)
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.wheel(msg.X, msg.Y, -1)
		case tea.MouseWheelDown:
			m.wheel(msg.X, msg.Y, 1)
		}
	}
	return m, nil
}

// click acts on the zone under (x, y) in the last frame.
func (m *Model) click(x, y int) {
	// Any click closes the help, its text included, and ends typing a
	// search, so keys move the cursor again wherever the click lands.
	if m.help {
		m.help = false
		return
	}
	m.searching = false
	z, ok := m.zoneAt(x, y)
	if !ok {
		return
	}
	if m.picker && z.kind != zoneTarget && z.kind != zoneTargetRow {
		m.picker = false
		return
	}
	switch z.kind {
	case zoneTab:
		m.setView(viewID(z.value))
	case zoneGroup:
		if m.group != groupMode(z.value) {
			m.group = groupMode(z.value)
			m.cursor[viewItems] = 0
			m.relist()
		}
	case zoneFilter:
		if m.filterIdx != z.value {
			m.filterIdx = z.value
			m.relist()
		}
	case zoneRow:
		if m.view == viewOverview {
			if m.selectRow(z.value) {
				m.jump()
			}
			return
		}
		if m.selectRow(z.value) {
			m.focus = 0
			if !m.twoPane() {
				m.detail = true
			}
		}
	case zoneListPane:
		m.focus = 0
	case zoneDetailPane:
		m.focus = 1
	case zoneBack:
		m.detail = false
		m.focus = 0
	case zoneTarget, zoneTargetRow:
		m.picker = false
		if z.value != m.tIdx {
			m.setTarget(z.value)
		}
	}
}

// wheel scrolls the pane under the pointer without moving focus.
func (m *Model) wheel(x, y, delta int) {
	z, ok := m.zoneAt(x, y)
	if !ok {
		return
	}
	switch z.kind {
	case zoneRow, zoneListPane:
		m.move(delta)
	case zoneDetailPane:
		m.scrollDetail(delta * 3)
	}
}

func (m *Model) scrollDetail(delta int) {
	m.dScroll += delta
	if m.dScroll < 0 {
		m.dScroll = 0
	}
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
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.hScroll++
		case "k", "up":
			m.hScroll = max(m.hScroll-1, 0)
		default:
			m.help = false
		}
		return m, nil
	}
	if m.picker {
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.pCursor = min(m.pCursor+1, len(m.targets)-1)
		case "k", "up":
			m.pCursor = max(m.pCursor-1, 0)
		case "enter":
			m.picker = false
			if m.pCursor != m.tIdx {
				m.setTarget(m.pCursor)
			}
		case "esc", "s":
			m.picker = false
		}
		return m, nil
	}
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help, m.hScroll = true, 0
	case "s":
		if m.multi() {
			m.picker, m.pCursor = true, m.tIdx
		}
	case ">", ".":
		if m.multi() {
			m.setTarget((m.tIdx + 1) % len(m.targets))
		}
	case "<", ",":
		if m.multi() {
			m.setTarget((m.tIdx + len(m.targets) - 1) % len(m.targets))
		}
	case "1", "2", "3", "4":
		m.setView(viewID(k[0] - '1'))
	case "]", "tab":
		m.setView((m.view + 1) % viewCount)
	case "[", "shift+tab":
		m.setView((m.view + viewCount - 1) % viewCount)
	case "j", "down":
		if m.inDetail() {
			m.scrollDetail(1)
		} else {
			m.move(1)
		}
	case "k", "up":
		if m.inDetail() {
			m.scrollDetail(-1)
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
			m.relist()
		}
	case "f":
		// The Overview applies no filter and shows no switch, so f there
		// would change the next view's filter unseen.
		if m.view != viewOverview {
			m.filterIdx = (m.filterIdx + 1) % len(m.filters)
			m.relist()
		}
	case "/":
		// The Overview is a fixed manifest; its search is the settings'.
		if m.view == viewOverview {
			m.setView(viewItems)
		}
		m.searching = true
		m.query = ""
		m.relist()
	case "l", "right", "enter":
		if m.view == viewOverview {
			m.jump()
		} else if m.twoPane() {
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
		// Esc steps back one level: out of the detail first (as its back
		// link and footer say), then out of a confirmed search.
		switch {
		case m.inDetail() && m.twoPane():
			m.focus = 0
		case m.inDetail():
			m.detail = false
		case m.query != "" && m.view != viewOverview:
			m.query = ""
			m.rebuild()
		}
	}
	return m, nil
}

func (m *Model) inDetail() bool {
	if m.view == viewOverview {
		return false
	}
	if m.twoPane() {
		return m.focus == 1
	}
	return m.detail
}

func (m *Model) setView(v viewID) {
	// Typing a search ends with the view it was typed in (a tab clicked
	// mid-search), so keys go back to moving the cursor.
	m.searching = false
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
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sbxray"
	return v
}

// Run starts the TUI and returns the one-line summary to print on exit.
func Run(p *sbxenv.Plan, in io.Reader, out io.Writer) (string, error) {
	return run(New(p, os.Getenv("NO_COLOR") != ""), in, out)
}

// RunTargets starts the TUI over discovered sandboxes.
func RunTargets(targets []live.Target, note string, in io.Reader, out io.Writer) (string, error) {
	return run(NewTargets(targets, note, os.Getenv("NO_COLOR") != ""), in, out)
}

func run(m *Model, in io.Reader, out io.Writer) (string, error) {
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		return "", err
	}
	line := exitSummary(m.plan)
	if t := m.target(); t != nil {
		line = "sbxray: " + model.EscapeControl(t.Name) + ": "
		// A target without a plan was not assessed: counts of 0 from the
		// stand-in plan would read as nothing found.
		switch {
		case t.Plan == nil && t.Sandbox != nil:
			line += "宣言ファイルがないため、危険度は評価していません"
		case t.Plan == nil:
			line += "設定ファイルを解析できないため、危険度は評価していません"
		default:
			line += sbxenv.SummaryLine(m.plan.Summary)
		}
		if t.Comparison != nil {
			line += fmt.Sprintf(" | ズレ %d", len(t.Comparison.Drifts))
		}
	}
	return line, nil
}

// exitSummary is printed to stdout after the TUI closes.
func exitSummary(p *sbxenv.Plan) string {
	return "sbxray: " + sbxenv.SummaryLine(p.Summary)
}
