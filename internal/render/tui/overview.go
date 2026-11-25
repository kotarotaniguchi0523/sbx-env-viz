// overview.go is the Overview screen of Model: tui.go's rebuild builds its
// cards and Enter jumps from them, and view.go draws them, so it reads and
// calls the same Model state as the other two files.
//
//declscope:namespace tui // one Bubble Tea Model split across tui.go, view.go and overview.go

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// card is one rounded panel of the Overview screen.
type card struct {
	title string
	lines []cardLine
}

// cardLine is one entry of a card. row is its index in Model.rows, or -1
// for a line that only informs and has nowhere to jump.
type cardLine struct {
	text   string
	badges []string
	row    int
}

// overviewMinTwoColumns is the narrowest width that lays cards in two columns.
const overviewMinTwoColumns = 100

// buildOverview fills m.cards and m.rows: every entry that names an item,
// diagnostic, or coverage gap becomes a selectable row.
func (m *Model) buildOverview() {
	m.cards = nil
	var rows []row
	byID := map[string]*sbxenv.Item{}
	for i := range m.plan.Items {
		byID[m.plan.Items[i].ID] = &m.plan.Items[i]
	}
	cur := -1
	newCard := func(title string) {
		m.cards = append(m.cards, card{title: title})
		cur = len(m.cards) - 1
	}
	info := func(text string) {
		m.cards[cur].lines = append(m.cards[cur].lines, cardLine{text: text, row: -1})
	}
	add := func(text string, r row) {
		rows = append(rows, r)
		m.cards[cur].lines = append(m.cards[cur].lines, cardLine{text: text, badges: r.badges, row: len(rows) - 1})
	}
	addItem := func(label string, it *sbxenv.Item) {
		add(label+": "+it.Value, row{label: label, badges: it.Badges, sev: it.Risk, item: it})
	}
	items := func(match func(*sbxenv.Item) bool) []*sbxenv.Item {
		var out []*sbxenv.Item
		for i := range m.plan.Items {
			if it := &m.plan.Items[i]; match(it) {
				out = append(out, it)
			}
		}
		return out
	}
	pathIs := func(prefixes ...string) func(*sbxenv.Item) bool {
		return func(it *sbxenv.Item) bool {
			for _, p := range prefixes {
				if it.Path == p || (strings.HasSuffix(p, ".") && strings.HasPrefix(it.Path, p)) {
					return true
				}
			}
			return false
		}
	}

	if t := m.target(); t != nil {
		switch {
		case t.Err != "":
			newCard("設定ファイルを解析できません")
			info(t.Err)
			for _, f := range t.Files {
				info("ファイル: " + model.EscapeControl(f))
			}
			m.rows = rows
			return
		case t.Plan == nil && t.Sandbox != nil:
			m.liveCard(t.Sandbox, newCard, info)
			m.rows = rows
			return
		case t.Comparison != nil:
			newCard(fmt.Sprintf("宣言と実体のズレ（%d 件）", len(m.drifts)))
			for i := range m.drifts {
				d := &m.drifts[i]
				add(d.Kind+": "+d.Title, row{label: d.Title, badges: []string{"[DRIFT]"}, sev: d.Severity, drift: d})
			}
			if len(m.drifts) == 0 {
				info("ズレは見つかりませんでした。")
			}
			info("比較した項目: " + strings.Join(t.Comparison.Checked, "・"))
			for _, s := range t.Comparison.Skipped {
				info(model.EscapeControl(s))
			}
		case strings.HasPrefix(t.State, "宣言のみ"):
			newCard("稼働中の実体")
			info("まだ作成されていません（sbx ls に該当する sandbox がありません）。")
		}
	}

	newCard("基本構成")
	for _, f := range [][2]string{
		{"name", "名前"}, {"agent", "エージェント"}, {"sandboxOptions.template", "テンプレート"},
		{"sandboxOptions.pullPolicy", "イメージの取得"}, {"sandboxOptions.memory", "メモリ"},
		{"sandboxOptions.cpus", "CPU"}, {"sandboxOptions.skills", "共有スキル"},
		{"entrypoint", "entrypoint"}, {"cmd", "cmd"},
	} {
		if it := byID[f[0]]; it != nil {
			addItem(f[1], it)
		} else {
			info(f[1] + ": （未設定・sbx の既定値）")
		}
	}

	newCard("適用される Kits")
	kits := items(pathIs("kits[]"))
	for _, it := range kits {
		state := "中身を確認済み"
		if field(it, "status") != "expanded" {
			switch field(it, "kind") {
			case "oci", "git", "zip":
				state = "リモートの Kit（中身は 4: Sources）"
			default:
				state = "未展開（読み込めない）"
			}
		}
		add(field(it, "handle")+": "+state, row{label: it.ID, badges: it.Badges, sev: it.Risk, item: it})
	}
	if len(kits) == 0 {
		info("（Kit の指定なし）")
	}

	newCard("ワークスペース・マウント")
	n := len(rows)
	for _, it := range items(pathIs("workspace")) {
		label := "ワークスペース（読み書き）"
		if field(it, "clone") == "true" || field(it, "--clone") == "true" {
			label = "ワークスペース（コピーして使う）"
		}
		addItem(label, it)
	}
	for _, it := range items(pathIs("additionalWorkspaces[]")) {
		label := "追加フォルダ（読み書き）"
		if hasBadge(it, sbxenv.BadgeRO) {
			label = "追加フォルダ（読み取り専用）"
		}
		addItem(label, it)
	}
	for _, it := range items(pathIs("mounts.")) {
		addItem("Kit 用フォルダの割り当て", it)
	}
	for _, it := range items(pathIs("cap:host-mount@1")) {
		addItem("Kit が共有するフォルダ", it)
	}
	if len(rows) == n {
		info("（共有するフォルダなし）")
	}

	newCard("MCP & Network")
	n = len(rows)
	for _, it := range items(pathIs("mcp.servers[]")) {
		name := strings.TrimSuffix(strings.TrimPrefix(it.ID, "mcp.servers["), "]")
		where := "リモート（URL に接続）"
		if strings.HasPrefix(it.Value, "command:") {
			where = "あなたの PC 上で起動（stdio）"
		}
		add("MCP "+name+": "+where, row{label: it.ID, badges: it.Badges, sev: it.Risk, item: it})
	}
	for _, it := range items(pathIs("cap:network-policy@1", "cap:network-policy@2")) {
		label := "通信の許可"
		if strings.Contains(it.ID, ".deny/") {
			label = "通信の禁止"
		}
		phase := "実行中"
		if strings.Contains(it.ID, "/install.") {
			phase = "準備時"
		}
		host := it.Value
		if parts := strings.SplitN(it.ID, "/", 4); len(parts) == 4 {
			host = parts[3]
		}
		add(fmt.Sprintf("%s（%s）: %s", label, phase, host), row{label: it.ID, badges: it.Badges, sev: it.Risk, item: it})
	}
	if len(rows) == n {
		info("（MCP サーバー・Kit の通信設定なし）")
	}

	newCard("資格情報・SSH・ポート・ホスト実行")
	n = len(rows)
	for _, g := range []struct {
		label string
		match func(*sbxenv.Item) bool
	}{
		{"秘密", pathIs("secrets.")},
		{"使用の承認", pathIs("bindings.")},
		{"レジストリ認証", pathIs("registries.")},
		{"Kit の認証情報", pathIs("cap:credential@1")},
		{"SSH エージェント", pathIs("cap:ssh-agent@1")},
		{"Git の名前・メール", pathIs("cap:git-identity@1")},
		{"ポート公開", pathIs("ports[]")},
		{"PC 上で実行", pathIs("lifecycle.")},
		{"sandbox 内で実行", pathIs("cap:lifecycle@1")},
	} {
		for _, it := range items(g.match) {
			label := g.label
			if i := strings.Index(it.ID, "."); i >= 0 && (strings.HasPrefix(it.ID, "secrets.") || strings.HasPrefix(it.ID, "bindings.") || strings.HasPrefix(it.ID, "registries.")) {
				label += " " + it.ID[i+1:]
			}
			addItem(label, it)
		}
	}
	if len(rows) == n {
		info("（なし）")
	}

	m.rows = rows
}

// liveCard shows a sandbox that no environment file declares.
func (m *Model) liveCard(sb *live.Sandbox, newCard func(string), info func(string)) {
	newCard("稼働中の実体（宣言ファイル未検出）")
	info("この sandbox を宣言した sbxenv*.yaml が、カレントディレクトリとワークスペースの近くに見つかりませんでした。")
	info("エージェント: " + orNone(sb.Agent))
	info("状態: " + live.StateLabel(sb.Status))
	for _, w := range sb.Workspaces {
		info("ワークスペース: " + w.Path)
	}
	for _, w := range sb.Mounts {
		info("マウント: " + w.Path)
	}
	for _, p := range sb.Ports {
		info(fmt.Sprintf("ポート公開: %s:%d -> sandbox:%d/%s", orNone(p.HostIP), p.HostPort, p.SandboxPort, orNone(p.Protocol)))
	}
	if sb.RulesKnown {
		info(fmt.Sprintf("通信ルール: %d 件", len(sb.Rules)))
	}
	if sb.SecretsKnown {
		info("シークレット（名前のみ）: " + orNone(strings.Join(sb.Secrets, ", ")))
	}
}

func orNone(s string) string {
	if s == "" {
		return "（なし）"
	}
	return s
}

func field(it *sbxenv.Item, key string) string {
	for _, f := range it.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func hasBadge(it *sbxenv.Item, b string) bool {
	for _, x := range it.Badges {
		if x == b {
			return true
		}
	}
	return false
}

// jump opens the Items or Risk detail of the selected Overview entry.
func (m *Model) jump() {
	r := m.current()
	if r == nil || m.view != viewOverview {
		return
	}
	target := viewRisk
	if r.item != nil {
		target = viewItems
	}
	m.query, m.filterIdx = "", 0
	m.setView(target)
	for i := range m.rows {
		x := m.rows[i]
		if (r.item != nil && x.item == r.item) || (r.diag != nil && x.diag == r.diag) || (r.cover != nil && x.cover == r.cover) || (r.drift != nil && x.drift == r.drift) {
			m.selectRow(i)
			break
		}
	}
	if !m.twoPane() {
		m.detail = true
	}
}

// overviewPanes lays the cards out in one or two columns of rounded panels,
// scrolled so the selected entry is visible, and records entry zones.
func (m *Model) overviewPanes(w, top, ph int) string {
	cols := 1
	if w >= overviewMinTwoColumns {
		cols = 2
	}
	cw := w / cols
	type placed struct {
		lines []string
		col   int
		y     int
	}
	colH := make([]int, cols)
	var cards []placed
	type ovZone struct{ x0, y, x1, row int }
	var zs []ovZone
	cursorY := 0
	cur := m.cursor[m.view]
	for _, c := range m.cards {
		col := 0
		for i := 1; i < cols; i++ {
			if colH[i] < colH[col] {
				col = i
			}
		}
		width := cw
		if col == cols-1 {
			width = w - cw*(cols-1)
		}
		iw := width - 2
		active := false
		inner := []string{m.pal.heading.Render(trunc("▍"+c.title, iw))}
		for _, l := range c.lines {
			y := colH[col] + 1 + len(inner)
			if l.row < 0 {
				// Informational lines wrap; only entries are one row.
				for _, w := range wrap(l.text, iw-2) {
					inner = append(inner, m.pal.dim.Render(" "+w))
				}
				continue
			}
			zs = append(zs, ovZone{col*cw + 1, y, col*cw + 1 + iw, l.row})
			if l.row == cur {
				active = true
				cursorY = y
				plain := " ▶ " + strings.TrimSpace(strings.Join(l.badges, "")+" "+l.text)
				inner = append(inner, m.pal.selected.Render(pad(trunc(plain, iw), iw)))
				continue
			}
			text := " • "
			if len(l.badges) > 0 {
				text += m.pal.badges(l.badges) + " "
			}
			inner = append(inner, trunc(text+l.text, iw))
		}
		box := strings.Split(m.box(width, len(inner)+2, inner, active), "\n")
		cards = append(cards, placed{box, col, colH[col]})
		colH[col] += len(box)
	}
	height := 0
	for _, h := range colH {
		height = max(height, h)
	}
	canvas := make([][]string, height)
	for i := range canvas {
		canvas[i] = make([]string, cols)
	}
	for _, c := range cards {
		for i, l := range c.lines {
			canvas[c.y+i][c.col] = l
		}
	}
	// Keep the selected entry (and its card title) on screen.
	if cursorY < m.oScroll+2 {
		m.oScroll = cursorY - 2
	}
	if cursorY >= m.oScroll+ph-1 {
		m.oScroll = cursorY - ph + 2
	}
	m.oScroll = min(max(m.oScroll, 0), max(height-ph, 0))

	m.addZone(0, top, w, top+ph, zoneListPane, 0)
	for _, z := range zs {
		if y := z.y - m.oScroll; y >= 0 && y < ph {
			m.addZone(z.x0, top+y, z.x1, top+y+1, zoneRow, z.row)
		}
	}
	out := make([]string, 0, ph)
	for i := m.oScroll; i < m.oScroll+ph; i++ {
		var b strings.Builder
		for col := 0; col < cols; col++ {
			width := cw
			if col == cols-1 {
				width = w - cw*(cols-1)
			}
			l := ""
			if i < height {
				l = canvas[i][col]
			}
			b.WriteString(pad(l, width))
		}
		out = append(out, b.String())
	}
	return lipgloss.JoinVertical(lipgloss.Left, out...)
}
