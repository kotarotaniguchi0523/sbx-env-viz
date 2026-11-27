package live

import (
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// Drift is one difference between the declaration and the running sandbox.
type Drift struct {
	// Kind is the compared area in Japanese (エージェント, ポート公開, ...).
	Kind string
	// Title says what differs in plain Japanese.
	Title string
	// Declared and Actual are the two sides as a person reads them.
	Declared string
	Actual   string
	// Fix says what to do.
	Fix      string
	Severity sbxenv.Severity
	// ItemID is the declared item involved, if any.
	ItemID string
	// Source names the sbx command the actual side comes from.
	Source string
}

// Comparison is the result of comparing one plan with one sandbox.
type Comparison struct {
	Drifts []Drift
	// Checked lists the compared areas; Skipped says, in Japanese, what was
	// not compared and why.
	Checked []string
	Skipped []string
	// Compared lists the ItemIDs of the declared items that were held
	// against sbx's output, whether they matched or drifted. An item in a
	// checked area that is not listed here (a clone workspace, a Kit mount,
	// an install-time rule) was not confirmed either way, so a reader must
	// not take it as matching.
	Compared []string
}

const (
	fixRecreate = "宣言の内容を反映するには、sandbox を作り直します（sbx env rm → sbx env create）。いまの状態を正とするなら、設定ファイルの方を実体に合わせて書き換えます。"
	srcLs       = "sbx ls --json"
)

// Paths turns a host path into what a person reads (home shortened).
type Paths func(string) string

// livePath shows a path sbx reports. Under strict redaction (mode, where
// empty means strict, as in sbxenv) a workspace path is hidden like the
// declared ones, since it can carry the very argument values the plan
// redacts (a name, a project directory).
//
//declscope:shared // discover.go shows the paths of a sandbox no file declares with it
func livePath(mode sbxenv.Redaction, show Paths, path string) string {
	if mode == "" || mode == sbxenv.RedactStrict {
		return "<redacted>"
	}
	return show(path)
}

// Compare reports how sandbox sb differs from plan p. Only what sbx
// reports is compared; anything else is listed in Skipped, never guessed.
func Compare(p *sbxenv.Plan, sb *Sandbox, snap *Snapshot, show Paths) Comparison {
	var c Comparison
	d := p.Declared
	if d == nil {
		c.Skipped = append(c.Skipped, "宣言の解析結果がないため比較していません。")
		return c
	}
	if show == nil {
		show = func(s string) string { return s }
	}
	// Build the lookup on demand: a comparison may need no item labels.
	var labels map[string]string
	item := func(id string) string {
		if labels == nil {
			labels = make(map[string]string, len(p.Items))
			for i := range p.Items {
				it := &p.Items[i]
				if _, exists := labels[it.ID]; !exists {
					labels[it.ID] = it.Value
				}
			}
		}
		return labels[id]
	}
	add := func(x Drift) { c.Drifts = append(c.Drifts, x) }
	// Under strict redaction (--redact, --json) a value sbx reports is
	// shown only as <redacted>: an argument hidden in any declaration of
	// the session (this one or another target's) can come back inside a
	// live name, agent, port, host or secret name in any form, so no live
	// value is held to be safe. What differs is still said, by kind.
	strict := p.Meta.Redaction == "" || p.Meta.Redaction == sbxenv.RedactStrict
	shown := func(v string) string {
		if strict {
			return "<redacted>"
		}
		return v
	}

	// Name: a declared name is how the sandbox was found, so it is
	// confirmed when sbx has the same one.
	if d.Name != "" && d.Name == sb.Name {
		c.Compared = append(c.Compared, "name")
	}

	// Agent.
	if d.Agent != "" && sb.Agent != "" {
		c.Checked = append(c.Checked, "エージェント")
		c.Compared = append(c.Compared, "agent")
		if !strings.EqualFold(d.Agent, sb.Agent) {
			add(Drift{Kind: "エージェント", Title: "宣言と違うエージェントで動いている", Declared: item("agent"), Actual: shown(sb.Agent),
				Fix: "agent を変えた場合は、作り直し（sbx env rm → sbx env create）で反映されます。", Severity: sbxenv.SeverityMedium, ItemID: "agent", Source: srcLs})
		}
	}

	// Workspaces.
	if len(sb.Workspaces) > 0 || len(d.Workspaces) > 0 {
		c.Checked = append(c.Checked, "ワークスペース・マウント")
		used := map[int]bool{}
		readOnlyNote := false
		for _, w := range d.Workspaces {
			decl := item(w.ItemID)
			if w.Primary && w.Clone {
				c.Skipped = append(c.Skipped, "メインのワークスペースは clone モード（専用のコピー）なので、マウント先のパスは比較していません。")
				for i, m := range sb.Workspaces {
					if samePath(m.matchPath(), w.Path) {
						used[i] = true
					}
				}
				continue
			}
			c.Compared = append(c.Compared, w.ItemID)
			found := -1
			for i, m := range sb.Workspaces {
				if !used[i] && samePath(m.matchPath(), w.Path) {
					found = i
					break
				}
			}
			if found < 0 {
				for _, m := range sb.Mounts {
					if samePath(m.matchPath(), w.Path) {
						found = -2
						break
					}
				}
			}
			switch {
			case found == -1:
				add(Drift{Kind: "ワークスペース・マウント", Title: "宣言したフォルダがマウントされていない", Declared: decl, Actual: "（マウントされていない）",
					Fix: fixRecreate, Severity: sbxenv.SeverityMedium, ItemID: w.ItemID, Source: srcLs})
			case found >= 0:
				used[found] = true
			}
			if w.ReadOnly || !w.Primary {
				readOnlyNote = true
			}
		}
		if readOnlyNote {
			c.Skipped = append(c.Skipped, "sbx ls --json はマウントのパスだけを出力し、読み取り専用かどうかを出力しないため、書き込みの可否（RW / RO）は比較していません。")
		}
		for i, m := range sb.Workspaces {
			if used[i] {
				continue
			}
			add(Drift{Kind: "ワークスペース・マウント", Title: "宣言にないフォルダがマウントされている", Declared: "（宣言なし）",
				Actual:   livePath(p.Meta.Redaction, show, m.Path) + "（読み書きの区別は sbx から取得できません）",
				Fix:      "sbx の操作で後から追加された可能性があります。不要なら作り直して宣言どおりにし、必要なら設定ファイルの additionalWorkspaces に書いてください。",
				Severity: sbxenv.SeverityMedium, Source: srcLs})
		}
	}

	// Ports.
	if len(sb.Ports) > 0 || len(d.Ports) > 0 {
		c.Checked = append(c.Checked, "ポート公開")
		// Pair declarations with listeners so that as many as possible
		// pair (a declaration never takes the listener another one needs,
		// so sbx's order cannot swap two of one sandbox port), each trying
		// the closest listener first: its exact host port, then its
		// address and a row showing its family, its address, its family,
		// then any. Declarations that can claim an exact host port go
		// first, then those that fix a family, then the rest.
		compatible := func(dp sbxenv.DeclaredPort, lp Port) bool {
			return lp.SandboxPort == dp.SandboxPort && baseProto(lp.Protocol) == baseProto(dp.Protocol) &&
				sameFamily(portFamily(lp.Protocol, lp.HostIP), portFamily(dp.Protocol, dp.HostIP))
		}
		distance := func(dp sbxenv.DeclaredPort, lp Port) int {
			// A fixed host port that differs costs most (it is a drift);
			// an ephemeral one accepts any host port.
			n := 0
			if dp.HostPort != 0 && lp.HostPort != dp.HostPort {
				n += 4
			}
			f := portFamily(dp.Protocol, dp.HostIP)
			shows := f == "" || portFamily(lp.Protocol, lp.HostIP) == f
			switch same := sameIP(dp.HostIP, lp.HostIP); {
			case same && shows:
			case same:
				n++
			case shows:
				n += 2
			default:
				n += 3
			}
			return n
		}
		rank := func(dp sbxenv.DeclaredPort) int {
			if dp.HostPort != 0 && slices.ContainsFunc(sb.Ports, func(lp Port) bool { return compatible(dp, lp) && lp.HostPort == dp.HostPort }) {
				return 0
			}
			if portFamily(dp.Protocol, dp.HostIP) != "" {
				return 1
			}
			return 2
		}
		pair := make([]int, len(d.Ports))   // declaration -> listener, or -1
		owner := make([]int, len(sb.Ports)) // listener -> declaration, or -1
		for j := range pair {
			pair[j] = -1
		}
		for i := range owner {
			owner[i] = -1
		}
		var claim func(j int, seen []bool) bool
		claim = func(j int, seen []bool) bool {
			dp := d.Ports[j]
			var cands []int
			for i, lp := range sb.Ports {
				if !seen[i] && compatible(dp, lp) {
					cands = append(cands, i)
				}
			}
			slices.SortStableFunc(cands, func(a, b int) int { return distance(dp, sb.Ports[a]) - distance(dp, sb.Ports[b]) })
			// A free listener is taken before one is pried from another
			// declaration, which moves only when this one has no other.
			for _, i := range cands {
				if owner[i] < 0 {
					seen[i] = true
					owner[i], pair[j] = j, i
					return true
				}
			}
			for _, i := range cands {
				seen[i] = true
				if claim(owner[i], seen) {
					owner[i], pair[j] = j, i
					return true
				}
			}
			return false
		}
		order := make([]int, len(d.Ports))
		for j := range order {
			order[j] = j
		}
		slices.SortStableFunc(order, func(a, b int) int { return rank(d.Ports[a]) - rank(d.Ports[b]) })
		for _, j := range order {
			claim(j, make([]bool, len(sb.Ports)))
		}
		// The matching pairs as many as it can, but its first choices can
		// still cost an exact pair (one declaration on a listener another
		// needed exactly). A sandbox port with a few declarations, as real
		// ones have, is settled by trying every assignment: the most pairs,
		// then the least total distance.
		type group struct{ decls, lists []int }
		groups := map[string]*group{}
		key := func(port int, proto string) string { return fmt.Sprint(port, "/", baseProto(proto)) }
		for j, dp := range d.Ports {
			k := key(dp.SandboxPort, dp.Protocol)
			if groups[k] == nil {
				groups[k] = &group{}
			}
			groups[k].decls = append(groups[k].decls, j)
		}
		for i, lp := range sb.Ports {
			if g := groups[key(lp.SandboxPort, lp.Protocol)]; g != nil {
				g.lists = append(g.lists, i)
			}
		}
		for _, g := range groups {
			if len(g.decls) < 2 || len(g.decls) > 5 || len(g.lists) > 6 {
				continue
			}
			best, bestN, bestCost := []int(nil), -1, 0
			cur := make([]int, len(g.decls))
			taken := map[int]bool{}
			var try func(k, n, cost int)
			try = func(k, n, cost int) {
				if k == len(g.decls) {
					if n > bestN || n == bestN && cost < bestCost {
						best, bestN, bestCost = slices.Clone(cur), n, cost
					}
					return
				}
				dp := d.Ports[g.decls[k]]
				for _, i := range g.lists {
					if !taken[i] && compatible(dp, sb.Ports[i]) {
						taken[i], cur[k] = true, i
						try(k+1, n+1, cost+distance(dp, sb.Ports[i]))
						taken[i] = false
					}
				}
				cur[k] = -1
				try(k+1, n, cost)
			}
			try(0, 0, 0)
			for _, i := range g.lists {
				owner[i] = -1
			}
			for k, j := range g.decls {
				pair[j] = best[k]
				if best[k] >= 0 {
					owner[best[k]] = j
				}
			}
		}
		used := map[int]bool{}
		for i, j := range owner {
			used[i] = j >= 0
		}
		for j, dp := range d.Ports {
			decl := item(dp.ItemID)
			found := pair[j]
			if found < 0 {
				c.Compared = append(c.Compared, dp.ItemID)
				add(Drift{Kind: "ポート公開", Title: "宣言したポートが公開されていない", Declared: decl, Actual: "（公開されていない）",
					Fix: fixRecreate, Severity: sbxenv.SeverityMedium, ItemID: dp.ItemID, Source: srcLs})
				continue
			}
			lp := sb.Ports[found]
			// A fixed host port is confirmed only when sbx reports one, and
			// a fixed family (tcp6, ::1) only when sbx's row shows one.
			if (dp.HostPort == 0 || lp.HostPort != 0) &&
				(portFamily(dp.Protocol, dp.HostIP) == "" || portFamily(lp.Protocol, lp.HostIP) != "") {
				c.Compared = append(c.Compared, dp.ItemID)
			}
			hide := dp.Redacted || strict
			var diffs []string
			if dp.HostPort != 0 && lp.HostPort != 0 && lp.HostPort != dp.HostPort {
				if hide {
					diffs = append(diffs, "PC 側のポート番号が異なる")
				} else {
					diffs = append(diffs, fmt.Sprintf("PC 側のポート番号が %d ではなく %d", dp.HostPort, lp.HostPort))
				}
			}
			wider := false
			if !sameIP(dp.HostIP, lp.HostIP) {
				if hide {
					diffs = append(diffs, "公開先のアドレスが異なる")
				} else {
					diffs = append(diffs, "公開先のアドレスが "+ipLabel(dp.HostIP)+" ではなく "+ipLabel(lp.HostIP))
				}
				wider = isAll(lp.HostIP) && !isAll(dp.HostIP)
			}
			if len(diffs) > 0 {
				sev := sbxenv.SeverityMedium
				if wider {
					sev = sbxenv.SeverityHigh
				}
				actual := portLabel(lp)
				if hide {
					actual = "<redacted>"
				}
				add(Drift{Kind: "ポート公開", Title: "ポートの公開のしかたが宣言と違う（" + strings.Join(diffs, "、") + "）", Declared: decl,
					Actual: actual, Fix: fixRecreate, Severity: sev, ItemID: dp.ItemID, Source: srcLs})
			}
		}
		for i, lp := range sb.Ports {
			if used[i] {
				continue
			}
			sev := sbxenv.SeverityLow
			if isAll(lp.HostIP) {
				sev = sbxenv.SeverityHigh
			}
			actual := portLabel(lp)
			if strict {
				actual = "<redacted>"
			}
			add(Drift{Kind: "ポート公開", Title: "宣言にないポートが公開されている", Declared: "（宣言なし）", Actual: actual,
				Fix:      "Kit が公開したポート（空きポートが自動で割り当てられます）か、sbx ports で手動公開したものです。不要なら sbx ports --unpublish で外せます。",
				Severity: sev, Source: srcLs})
		}
	}

	// Network rules.
	if !sb.RulesKnown {
		c.Skipped = append(c.Skipped, "sbx policy ls の結果を取得できなかったため、通信ルールは比較していません。")
	} else {
		c.Checked = append(c.Checked, "通信の許可・拒否")
		// A bounded rule does not declare its whole host, so a manual
		// rule for that host is still reported.
		declared := map[string]bool{}
		for _, r := range d.Network {
			if r.Phase == "runtime" && !r.Bounded {
				declared[r.Decision+" "+r.Host] = true
			}
		}
		kitVisible := false
		live := map[string]bool{}
		for _, r := range sb.Rules {
			live[r.Decision+" "+r.Resource] = true
			if strings.Contains(r.Origin, "kit") || strings.Contains(r.Layer, "kit") {
				kitVisible = true
			}
		}
		for _, r := range sb.Rules {
			manual := r.CreatedVia == "added" || r.CreatedVia == "approval"
			if !manual || declared[r.Decision+" "+r.Resource] {
				continue
			}
			resource := shown(r.Resource)
			how := "sbx policy allow などで手動追加"
			if r.CreatedVia == "approval" {
				how = "通信の承認プロンプトで許可"
			}
			if r.Decision == "allow" {
				add(Drift{Kind: "通信の許可・拒否", Title: "宣言にない通信許可がある（" + how + "）", Declared: "（宣言なし）", Actual: "許可: " + resource,
					Fix:      "不要なら sbx policy rm network で削除します。必要なら Kit の network-policy に書いて宣言に含めると、どこで許可したかが分かります。",
					Severity: sbxenv.SeverityMedium, Source: "sbx policy ls <name> --json"})
			} else {
				add(Drift{Kind: "通信の許可・拒否", Title: "宣言にない通信の拒否がある", Declared: "（宣言なし）", Actual: "拒否: " + resource,
					Fix: "手動で追加した拒否ルールです。意図したものか確認してください。", Severity: sbxenv.SeverityLow, Source: "sbx policy ls <name> --json"})
			}
		}
		if !kitVisible {
			if len(d.Network) > 0 {
				c.Skipped = append(c.Skipped, "sbx policy ls の結果に Kit 由来のルールが含まれていないため、宣言した通信ルールが反映済みかは比較していません。")
			}
		} else {
			for _, r := range d.Network {
				// sbx policy ls lists the rules in force now, so only
				// runtime rules can be held against it.
				if r.Phase != "runtime" {
					continue
				}
				// A rule bounded to methods or paths is listed by host
				// alone, so its host being there confirms no more than that.
				if !r.Bounded {
					c.Compared = append(c.Compared, r.ItemID)
				}
				if live[r.Decision+" "+r.Host] {
					continue
				}
				label := map[string]string{"allow": "許可", "deny": "拒否"}[r.Decision]
				add(Drift{Kind: "通信の許可・拒否", Title: "宣言した通信の" + label + "が反映されていない", Declared: item(r.ItemID), Actual: "（該当するルールなし）",
					Fix: fixRecreate, Severity: sbxenv.SeverityMedium, ItemID: r.ItemID, Source: "sbx policy ls <name> --json"})
			}
		}
	}

	// Secrets.
	if !sb.SecretsKnown {
		c.Skipped = append(c.Skipped, "sbx secret ls の結果を取得できなかったため、シークレットは比較していません。")
	} else {
		c.Checked = append(c.Checked, "シークレット（名前のみ）")
		live := map[string]bool{}
		for _, s := range sb.Secrets {
			live[s] = true
		}
		declared := map[string]bool{}
		for _, s := range d.Secrets {
			declared[s.Name] = true
			c.Compared = append(c.Compared, s.ItemID)
			if !live[s.Name] {
				add(Drift{Kind: "シークレット", Title: "宣言したシークレットが登録されていない", Declared: s.Name + "（" + item(s.ItemID) + "）", Actual: "（登録なし）",
					Fix:      "シークレットは sandbox を作成したときに登録されます。作り直す（sbx env rm → sbx env create）か、sbx secret set で登録してください。",
					Severity: sbxenv.SeverityMedium, ItemID: s.ItemID, Source: "sbx secret ls --sandbox <name> --json"})
			}
		}
		for _, s := range sb.Secrets {
			if !declared[s] {
				add(Drift{Kind: "シークレット", Title: "宣言にないシークレットが登録されている", Declared: "（宣言なし）", Actual: shown(s) + "（値は表示しません）",
					Fix:      "sbx secret set で手動登録した可能性があります。不要なら sbx secret rm で削除してください。",
					Severity: sbxenv.SeverityLow, Source: "sbx secret ls --sandbox <name> --json"})
			}
		}
	}

	// MCP servers (registered gateway-wide, so only declared ones are checked).
	switch {
	case len(d.MCP) == 0:
	case snap == nil || !snap.MCPKnown:
		c.Skipped = append(c.Skipped, "sbx mcp ls の結果を取得できなかったため、MCP サーバーは比較していません。")
	default:
		c.Checked = append(c.Checked, "MCP サーバー")
		for _, m := range d.MCP {
			var row *MCPServer
			for i := range snap.MCP {
				if snap.MCP[i].Name == m.Name {
					row = &snap.MCP[i]
					break
				}
			}
			if row == nil {
				c.Compared = append(c.Compared, m.ItemID)
				add(Drift{Kind: "MCP サーバー", Title: "宣言した MCP サーバーが登録されていない", Declared: item(m.ItemID), Actual: "（登録なし）",
					Fix: fixRecreate, Severity: sbxenv.SeverityMedium, ItemID: m.ItemID, Source: "sbx mcp ls --json"})
				continue
			}
			// The kind sbx reports ("remote", or another for a command
			// server) is held against url versus command; without it only
			// the registration is known, which does not confirm the row.
			if row.Type != "" {
				c.Compared = append(c.Compared, m.ItemID)
				if remote := row.Type == "remote"; remote != m.Remote {
					add(Drift{Kind: "MCP サーバー", Title: "同じ名前の MCP サーバーが、宣言と違う種類で登録されている", Declared: item(m.ItemID),
						Actual: "種類: " + row.Type + "（" + orDefault(row.Transport, "接続方式不明") + "）",
						Fix:    "別の設定で登録された可能性があります。sbx mcp inspect " + m.Shown + " で確認してください。", Severity: sbxenv.SeverityMedium,
						ItemID: m.ItemID, Source: "sbx mcp ls --json"})
				}
			}
			if st := row.Status; needsAuth(st) {
				add(Drift{Kind: "MCP サーバー", Title: "MCP サーバーは登録済みだが使える状態ではない", Declared: item(m.ItemID), Actual: "状態: " + st,
					Fix: "認証が必要な場合は sbx mcp auth " + m.Shown + " を実行してください。詳しくは sbx mcp inspect " + m.Shown + " で確認できます。", Severity: sbxenv.SeverityLow,
					ItemID: m.ItemID, Source: "sbx mcp ls --json"})
			}
		}
	}
	c.Skipped = append(c.Skipped, "メモリ・CPU・lifecycle の実行履歴などは sbx の一覧から取得できないため比較していません。")
	sort.SliceStable(c.Drifts, func(i, j int) bool { return c.Drifts[i].Severity.Rank() > c.Drifts[j].Severity.Rank() })
	return c
}

func baseProto(p string) string {
	p = strings.ToLower(p)
	if strings.HasPrefix(p, "udp") {
		return "udp"
	}
	return "tcp"
}

func isLoopback(ip string) bool {
	ip = strings.Trim(ip, "[]")
	return ip == "" || ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}

func isAll(ip string) bool {
	ip = strings.Trim(ip, "[]")
	return ip == "0.0.0.0" || ip == "::"
}

func sameIP(a, b string) bool {
	if isLoopback(a) && isLoopback(b) {
		return sameFamily(ipFamily(a), ipFamily(b))
	}
	a, b = strings.Trim(a, "[]"), strings.Trim(b, "[]")
	// Two spellings of one address (2001:0db8::1, 2001:db8::1) are one.
	if x, err := netip.ParseAddr(a); err == nil {
		if y, err := netip.ParseAddr(b); err == nil {
			return x == y
		}
	}
	return a == b
}

// ipFamily is "4" or "6" for an address literal, and "" for one that
// does not fix the family (empty, localhost).
func ipFamily(ip string) string {
	switch ip = strings.Trim(ip, "[]"); {
	case ip == "" || ip == "localhost":
		return ""
	case strings.Contains(ip, ":"):
		return "6"
	}
	return "4"
}

// protoFamily is "4" or "6" for tcp4/udp6 and the like, and "" for a
// protocol bound to both (tcp, udp).
func protoFamily(p string) string {
	p = strings.ToLower(p)
	if strings.HasSuffix(p, "4") || strings.HasSuffix(p, "6") {
		return p[len(p)-1:]
	}
	return ""
}

// portFamily is the address family a port is bound to: its protocol's
// (tcp4, udp6), else its address literal's, else "" for both.
func portFamily(proto, ip string) string {
	if f := protoFamily(proto); f != "" {
		return f
	}
	return ipFamily(ip)
}

// sameFamily tells whether two families can meet: an unfixed one meets
// either.
func sameFamily(a, b string) bool { return a == "" || b == "" || a == b }

func ipLabel(ip string) string {
	switch {
	case isLoopback(ip):
		return "あなたの PC の中だけ（" + orDefault(ip, "ループバック") + "）"
	case isAll(ip):
		return "すべてのネットワーク（" + ip + "）"
	}
	return ip
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func portLabel(p Port) string {
	ip := p.HostIP
	if ip == "" {
		ip = "loopback"
	}
	if strings.Contains(ip, ":") {
		ip = "[" + ip + "]"
	}
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}
	return fmt.Sprintf("%s:%d -> sandbox:%d/%s", ip, p.HostPort, p.SandboxPort, proto)
}

func needsAuth(status string) bool {
	s := strings.ToLower(status)
	for _, w := range []string{"unauth", "needs", "required", "expired", "missing", "unsupported", "error", "failed"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
