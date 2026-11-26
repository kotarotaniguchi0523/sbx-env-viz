// overview.go is the Overview screen of Model: tui.go's rebuild builds its
// manifest and Enter jumps from it, and view.go draws it, so it reads and
// calls the same Model state as the other two files.
//
//declscope:namespace tui // one Bubble Tea Model split across tui.go, view.go and overview.go

package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// lineKind is the role of one manifest line.
type lineKind int

const (
	lineEntry   lineKind = iota // a declared setting, selectable
	lineSection                 // a section heading
	lineInfo                    // an explanatory sentence, not selectable
)

// checkState is what the live comparison says about one entry.
type checkState int

const (
	checkNone  checkState = iota // no live data: the column is not drawn
	checkSkip                    // this area is not compared
	checkMatch                   // compared and the same
	checkDrift                   // compared and different
)

// manifestLine is one line of the Overview's declaration manifest.
type manifestLine struct {
	kind     lineKind
	sev      sbxenv.Severity
	label    string
	declared string
	check    checkState
	actual   string
	// row is the line's index in Model.rows, or -1.
	row int
}

// buildOverview fills m.manifest and m.rows: every entry that names an
// item or a drift becomes a selectable row.
func (m *Model) buildOverview() {
	m.manifest = nil
	var rows []row
	t := m.target()
	var cmp *live.Comparison
	if t != nil {
		cmp = t.Comparison
	}
	used := map[*live.Drift]bool{}
	byID := map[string]*sbxenv.Item{}
	for i := range m.plan.Items {
		byID[m.plan.Items[i].ID] = &m.plan.Items[i]
	}

	section := func(title string) {
		m.manifest = append(m.manifest, manifestLine{kind: lineSection, label: title, row: -1})
	}
	info := func(text string) {
		m.manifest = append(m.manifest, manifestLine{kind: lineInfo, label: text, row: -1})
	}
	// entry adds a setting that stands for items, compared under area.
	entry := func(label, declared, area string, items ...*sbxenv.Item) {
		l := manifestLine{kind: lineEntry, label: label, declared: declared, sev: sbxenv.SeverityInfo}
		for _, it := range items {
			if it.Risk.Rank() > l.sev.Rank() {
				l.sev = it.Risk
			}
		}
		r := row{label: label, sev: l.sev}
		if len(items) > 0 {
			r.item = items[0]
		}
		if cmp != nil {
			l.check = checkSkip
			compared := slices.ContainsFunc(items, func(it *sbxenv.Item) bool { return !slices.Contains(cmp.NotCompared, it.ID) })
			if slices.Contains(cmp.Checked, area) && compared {
				l.check = checkMatch
			}
			for i := range m.drifts {
				d := &m.drifts[i]
				if d.ItemID == "" || used[d] || !slices.ContainsFunc(items, func(it *sbxenv.Item) bool { return it.ID == d.ItemID }) {
					continue
				}
				used[d] = true
				if l.check != checkDrift {
					l.check, l.actual = checkDrift, d.Actual
					r.drift = d
					if d.Severity.Rank() > l.sev.Rank() {
						l.sev = d.Severity
					}
				}
			}
		}
		rows = append(rows, r)
		l.row = len(rows) - 1
		m.manifest = append(m.manifest, l)
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

	if t != nil {
		switch {
		case t.Err != "" && t.Plan == nil:
			section("設定ファイルを解析できません")
			info(t.Err)
			for _, f := range t.Files {
				info("ファイル: " + model.EscapeControl(f))
			}
			m.rows = rows
			return
		case t.Plan == nil && t.Sandbox != nil:
			m.liveOnly(t.Sandbox, section, info)
			m.rows = rows
			return
		case strings.HasPrefix(t.State, "宣言のみ"):
			section("稼働中の実体")
			info("まだ作成されていません（sbx ls に該当する sandbox がありません）。")
		}
	}

	section("基本")
	for _, f := range [][2]string{
		{"name", "名前"}, {"agent", "エージェント"}, {"sandboxOptions.template", "テンプレート"},
		{"sandboxOptions.pullPolicy", "イメージの取得"}, {"sandboxOptions.memory", "リソース"},
		{"sandboxOptions.skills", "共有スキル"}, {"entrypoint", "entrypoint"}, {"cmd", "cmd"},
	} {
		if f[0] == "sandboxOptions.memory" {
			// Memory and CPUs read as one line: "メモリ 12g、CPU 4".
			var parts []string
			var its []*sbxenv.Item
			if it := byID["sandboxOptions.memory"]; it != nil {
				parts, its = append(parts, "メモリ "+it.Value), append(its, it)
			}
			if it := byID["sandboxOptions.cpus"]; it != nil {
				parts, its = append(parts, "CPU "+it.Value), append(its, it)
			}
			if len(its) > 0 {
				entry(f[1], strings.Join(parts, "、"), "", its...)
			}
			continue
		}
		if it := byID[f[0]]; it != nil {
			area := ""
			if f[0] == "agent" {
				area = "エージェント"
			}
			entry(f[1], it.Value, area, it)
		}
	}

	if kits := items(pathIs("kits[]")); len(kits) > 0 {
		section("Kit")
		for _, it := range kits {
			state := "展開済み"
			if field(it, "status") != "expanded" {
				switch field(it, "kind") {
				case "oci", "git", "zip":
					state = "リモート（中身は「4 ファイル」で確認）"
				default:
					state = "未展開（読み込めない）"
				}
			}
			entry(field(it, "handle"), state, "", it)
		}
	}

	if ws, extra, kitMounts, caps := items(pathIs("workspace")), items(pathIs("additionalWorkspaces[]")), items(pathIs("mounts.")), items(pathIs("cap:host-mount@1")); len(ws)+len(extra)+len(kitMounts)+len(caps) > 0 {
		section("フォルダの共有")
		const area = "ワークスペース・マウント"
		for _, it := range ws {
			mode := "読み書き"
			if field(it, "clone") == "true" || field(it, "--clone") == "true" {
				mode = "コピーして使う"
			}
			entry("ワークスペース", it.Value+"（"+mode+"）", area, it)
		}
		for _, it := range extra {
			mode := "読み書き"
			if hasBadge(it, sbxenv.BadgeRO) {
				mode = "読み取り専用"
			}
			entry("追加フォルダ", it.Value+"（"+mode+"）", area, it)
		}
		for _, it := range kitMounts {
			entry("Kit 用フォルダ", it.Value, area, it)
		}
		for _, it := range caps {
			v := it.Value
			if hasBadge(it, sbxenv.BadgePersist) {
				v += "（削除後も残る）"
			}
			entry("Kit の共有", v, area, it)
		}
	}

	if ports := items(pathIs("ports[]")); len(ports) > 0 {
		section("ポート公開")
		for _, it := range ports {
			entry("ポート", it.Value, "ポート公開", it)
		}
	}

	mcp, net := items(pathIs("mcp.servers[]")), items(pathIs("cap:network-policy@1", "cap:network-policy@2"))
	if len(mcp)+len(net) > 0 {
		section("通信")
		for _, it := range mcp {
			name := strings.TrimSuffix(strings.TrimPrefix(it.ID, "mcp.servers["), "]")
			where := "リモート（URL に接続）"
			if strings.HasPrefix(it.Value, "command:") {
				where = "あなたの PC 上で起動（stdio）"
			}
			entry("MCP "+name, where, "MCP サーバー", it)
		}
		// Rules group by decision and phase, one line each.
		type group struct {
			label string
			hosts []string
			items []*sbxenv.Item
		}
		var groups []*group
		find := map[string]*group{}
		for _, it := range net {
			label := "許可"
			if strings.Contains(it.ID, ".deny/") {
				label = "禁止"
			}
			if strings.Contains(it.ID, "/install.") {
				label += "（準備時）"
			} else {
				label += "（実行中）"
			}
			g := find[label]
			if g == nil {
				g = &group{label: label}
				find[label] = g
				groups = append(groups, g)
			}
			host := it.Value
			if parts := strings.SplitN(it.ID, "/", 4); len(parts) == 4 {
				host = parts[3]
			}
			g.hosts = append(g.hosts, host)
			g.items = append(g.items, it)
		}
		for _, g := range groups {
			entry(g.label, fmt.Sprintf("%d 件  %s", len(g.hosts), strings.Join(g.hosts, "、")), "通信の許可・拒否", g.items...)
		}
	}

	var creds []func()
	for _, g := range []struct {
		label, area string
		match       func(*sbxenv.Item) bool
	}{
		{"秘密", "シークレット（名前のみ）", pathIs("secrets.")},
		{"使用の承認", "", pathIs("bindings.")},
		{"レジストリ認証", "", pathIs("registries.")},
		{"Kit の認証情報", "", pathIs("cap:credential@1")},
		{"SSH エージェント", "", pathIs("cap:ssh-agent@1")},
		{"Git の名前・メール", "", pathIs("cap:git-identity@1")},
	} {
		for _, it := range items(g.match) {
			label := g.label
			if i := strings.Index(it.ID, "."); i >= 0 && (strings.HasPrefix(it.ID, "secrets.") || strings.HasPrefix(it.ID, "bindings.") || strings.HasPrefix(it.ID, "registries.")) {
				label += " " + it.ID[i+1:]
			}
			creds = append(creds, func() { entry(label, it.Value, g.area, it) })
		}
	}
	if len(creds) > 0 {
		section("資格情報")
		for _, f := range creds {
			f()
		}
	}

	host, inside := items(pathIs("lifecycle.")), items(pathIs("cap:lifecycle@1"))
	if len(host)+len(inside) > 0 {
		section("コマンドの実行")
		for _, it := range host {
			entry("PC 上で実行", it.Value, "", it)
		}
		for _, it := range inside {
			entry("sandbox 内で実行", it.Value, "", it)
		}
	}

	// Drifts no declared line stands for: something sbx has that the files
	// do not declare, or a declaration the manifest groups elsewhere.
	var rest []*live.Drift
	for i := range m.drifts {
		if d := &m.drifts[i]; !used[d] {
			rest = append(rest, d)
		}
	}
	if len(rest) > 0 {
		section("宣言にない実体・その他のズレ")
		for _, d := range rest {
			rows = append(rows, row{label: d.Title, sev: d.Severity, drift: d})
			m.manifest = append(m.manifest, manifestLine{kind: lineEntry, label: d.Kind, declared: d.Declared, sev: d.Severity, check: checkDrift, actual: d.Actual, row: len(rows) - 1})
		}
	}
	if cmp != nil {
		for _, s := range cmp.Skipped {
			info(model.EscapeControl(s))
		}
	}
	m.rows = rows
}

// liveOnly shows a sandbox that no environment file declares.
func (m *Model) liveOnly(sb *live.Sandbox, section func(string), info func(string)) {
	section("稼働中の実体（宣言ファイル未検出）")
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
	return slices.Contains(it.Badges, b)
}

// jump opens the Items or Risk detail of the selected Overview entry.
func (m *Model) jump() {
	r := m.current()
	if r == nil || m.view != viewOverview {
		return
	}
	// A drifted entry opens the drift, whose detail sets the declaration
	// beside what sbx has; any other entry opens its setting.
	target := viewItems
	if r.drift != nil || r.item == nil {
		target = viewRisk
	}
	m.query, m.filterIdx = "", 0
	m.setView(target)
	for i := range m.rows {
		x := m.rows[i]
		if (target == viewItems && x.item == r.item) || (target == viewRisk && r.drift != nil && x.drift == r.drift) {
			m.selectRow(i)
			break
		}
	}
	if !m.twoPane() {
		m.detail = true
	}
}

// riskCounts counts the findings this tool rates high or above, and medium.
func riskCounts(p *sbxenv.Plan) (high, medium int) {
	for _, r := range model.RiskList(p) {
		if r.Cover != nil {
			continue
		}
		switch {
		case r.Severity.Rank() >= sbxenv.SeverityHigh.Rank():
			high++
		case r.Severity == sbxenv.SeverityMedium:
			medium++
		}
	}
	return high, medium
}

// overviewPanes draws the summary line and the manifest table, scrolled so
// the selected entry stays visible, and records the row zones.
func (m *Model) overviewPanes(w, top, ph int) string {
	p := m.pal
	var head []string
	high, medium := riskCounts(m.plan)
	summary := p.stamp.Render("✗") + " 最重要・高 " + p.strong.Render(fmt.Sprint(high)) + "    " +
		p.caution.Render("!") + " 中 " + p.strong.Render(fmt.Sprint(medium))
	if t := m.target(); t != nil && t.Comparison != nil {
		n := p.strong.Render(fmt.Sprint(len(m.drifts)))
		if len(m.drifts) > 0 {
			summary += "    " + p.stamp.Render("ズレ") + " " + n
		} else {
			summary += "    ズレ " + n
		}
	}
	summary += p.muted.Render("    印なしでも問題がないとは限りません")
	head = append(head, " "+summary, "")

	live := false
	labelW, actualW := 4, 0
	for _, l := range m.manifest {
		if l.kind != lineEntry {
			continue
		}
		labelW = max(labelW, min(widthOf(l.label), 18))
		if l.check != checkNone {
			live = true
			actualW = max(actualW, min(widthOf(actualText(l)), w*30/100))
		}
	}
	if live {
		actualW = max(actualW, widthOf("実体（sbx）"))
	}
	const indent = 4 // " ✗  " margin before the label
	declW := w - indent - labelW - 2
	if live {
		declW -= actualW + 2
	}
	declW = max(declW, 8)
	cols := func(mark, label, declared, actual string) string {
		s := " " + mark + "  " + pad(label, labelW) + "  "
		if !live {
			return s + trunc(declared, declW)
		}
		return s + pad(declared, declW) + "  " + trunc(actual, actualW)
	}
	header := pad(cols(" ", "項目", "宣言（sbxenv.yaml）", "実体（sbx）"), w-1)
	head = append(head, p.colHead.Render(header))

	var body []string
	type rowZone struct{ y, row int }
	var zs []rowZone
	cursorY := 0
	cur := m.cursor[m.view]
	for _, l := range m.manifest {
		switch l.kind {
		case lineSection:
			if len(body) > 0 {
				body = append(body, "")
			}
			body = append(body, " "+p.strong.Render(l.label))
		case lineInfo:
			for _, s := range wrap(l.label, w-indent-1) {
				body = append(body, strings.Repeat(" ", indent)+p.muted.Render(s))
			}
		case lineEntry:
			zs = append(zs, rowZone{len(body), l.row})
			if l.row == cur {
				cursorY = len(body)
				body = append(body, p.selected.Render(pad(cols(markText(l.sev), l.label, l.declared, actualText(l)), w-1)))
				continue
			}
			actual := actualText(l)
			switch l.check {
			case checkDrift:
				actual = p.drift.Render(trunc(actual, actualW))
			case checkMatch, checkSkip:
				actual = p.muted.Render(actual)
			}
			s := " " + p.mark(l.sev) + "  " + pad(l.label, labelW) + "  "
			if live {
				s += pad(l.declared, declW) + "  " + actual
			} else {
				s += trunc(l.declared, declW)
			}
			body = append(body, s)
		}
	}
	if len(body) == 0 {
		body = append(body, strings.Repeat(" ", indent)+p.muted.Render("（表示できる設定がありません）"))
	}

	bh := max(ph-len(head), 1)
	if cursorY < m.oScroll+1 {
		m.oScroll = cursorY - 1
	}
	if cursorY >= m.oScroll+bh-1 {
		m.oScroll = cursorY - bh + 2
	}
	m.oScroll = min(max(m.oScroll, 0), max(len(body)-bh, 0))

	m.addZone(0, top, w, top+ph, zoneListPane, 0)
	for _, z := range zs {
		if y := z.y - m.oScroll; y >= 0 && y < bh {
			m.addZone(0, top+len(head)+y, w, top+len(head)+y+1, zoneRow, z.row)
		}
	}
	out := append([]string{}, head...)
	end := min(m.oScroll+bh, len(body))
	for i := m.oScroll; i < end; i++ {
		out = append(out, body[i])
	}
	if rest := len(body) - end; rest > 0 {
		// The last visible line says how much follows, so the summary's
		// counts never point at rows the user cannot see.
		out[len(out)-1] = strings.Repeat(" ", indent) + p.muted.Render(fmt.Sprintf("ほか %d 行（j で下へ）", rest+1))
	}
	for len(out) < ph {
		out = append(out, "")
	}
	for i := range out {
		out[i] = pad(out[i], w)
	}
	return strings.Join(out[:ph], "\n")
}

// actualText is the 実体 column of an entry.
func actualText(l manifestLine) string {
	switch l.check {
	case checkMatch:
		return "一致"
	case checkDrift:
		return l.actual
	case checkSkip:
		return "照合なし"
	}
	return ""
}
