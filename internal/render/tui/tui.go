// Package tui is the interactive Bubble Tea v2 view of a Plan.
//
// Views: 1 Overview (default; cards that jump to details), 2 Risk, 3 Items
// (g cycles layer / boundary / kit grouping), 4 Sources. At 120 columns or wider the list and the detail
// pane sit side by side in rounded panels; narrower terminals show one pane
// at a time. Tabs, chips, list rows, and panes respond to the mouse.
package tui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// TwoPaneMinWidth is the narrowest width that shows list and detail together.
const TwoPaneMinWidth = 120

type viewID int

const (
	viewOverview viewID = iota
	viewRisk
	viewItems
	viewSources
	viewCount
)

var viewNames = []string{"1:Overview", "2:Risk", "3:Items", "4:Sources"}

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
	badges []string
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

	pal   palette
	rows  []row
	cards []card
	// oScroll is the first visible canvas line of the Overview.
	oScroll int
	// zones are the clickable regions of the last frame; mouse events are
	// resolved against them so drawing and hit-testing never disagree.
	zones []zone
}

// New builds a model. noColor disables all colours (NO_COLOR); structure,
// borders, bold, and reverse video remain.
func New(p *sbxenv.Plan, noColor bool) *Model {
	p = model.ForTerminal(p)
	m := &Model{plan: p, idx: model.SourceIndex(p), width: 100, height: 30, pal: newPalette(noColor)}
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
	case viewOverview:
		m.buildOverview()
		rows = m.rows
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
			label, search := r.Label, r.Label
			switch {
			case r.Diag != nil:
				label = sbxenv.ExplainDiagnostic(r.Diag.Code).Title
				search = label + " " + r.Label
			case r.Cover != nil:
				label = "未展開 " + sbxenv.CoverageTitle(r.Cover.Kind) + ": " + r.Cover.Ref
				search = label + " " + r.Label
			}
			if !m.matches(strings.Join(r.Badges, "") + " " + search) {
				continue
			}
			rows = append(rows, row{label: label, badges: r.Badges, sev: r.Severity, item: r.Item, diag: r.Diag, cover: r.Cover})
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
				rows = append(rows, row{label: it.ID + " = " + it.Value, badges: it.Badges, sev: it.Risk, item: it})
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
			label := fmt.Sprintf("%-7s %s [%s] %s", s.ID, s.Path, state, sbxenv.SourceKindLabel(s.Kind))
			if !m.matches(label) {
				continue
			}
			rows = append(rows, row{label: label, sev: sbxenv.SeverityInfo, source: s})
		}
		if len(m.plan.Coverage) > 0 {
			rows = append(rows, row{header: true, label: fmt.Sprintf("未展開・解析範囲外 (%d)", len(m.plan.Coverage))})
			for i := range m.plan.Coverage {
				c := &m.plan.Coverage[i]
				label := sbxenv.CoverageTitle(c.Kind) + ": " + c.Ref
				if f := m.filter(); f != "" && c.Origin.SourceID != f {
					continue
				}
				if m.matches(label) {
					rows = append(rows, row{label: label, badges: []string{"[COVERAGE]"}, sev: sbxenv.SeverityMedium, cover: c})
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
	z, ok := m.zoneAt(x, y)
	if !ok {
		return
	}
	if m.help {
		m.help = false
		return
	}
	switch z.kind {
	case zoneTab:
		m.setView(viewID(z.value))
	case zoneGroup:
		if m.group != groupMode(z.value) {
			m.group = groupMode(z.value)
			m.cursor[viewItems] = 0
			m.rebuild()
		}
	case zoneFilter:
		if m.filterIdx != z.value {
			m.filterIdx = z.value
			m.rebuild()
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
	if m.view == viewOverview {
		return false
	}
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
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sbx-env-viz"
	return v
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
