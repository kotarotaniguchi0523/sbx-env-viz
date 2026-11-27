package sbxenv

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

// Badges shown in renderers. They are ASCII so they survive NO_COLOR and
// copy/paste.
const (
	BadgeHost    = "[HOST]"
	BadgeOpaque  = "[OPAQUE]"
	BadgeRW      = "[RW]"
	BadgeRO      = "[RO]"
	BadgePersist = "[PERSIST]"
	BadgeNet     = "[NET]"
	BadgeCred    = "[CRED]"
	BadgeSSH     = "[SSH]"
	BadgeSkip    = "[SKIP]"
	BadgeWarn    = "[!]"
	BadgeUnknown = "[UNKNOWN]"
	BadgeLiteral = "[LITERAL]"
)

type fmtFn func(*vnode) string

//declscope:shared // analyze.go adds the items its checks report on
func (rs *resolver) addItem(it Item, v *vnode, f fmtFn) *Item {
	base := it.ID
	for n := 2; ; n++ {
		if _, dup := rs.itemIndex[it.ID]; !dup {
			break
		}
		it.ID = fmt.Sprintf("%s#%d", base, n)
	}
	if v != nil {
		if it.Value == "" && f != nil {
			it.Value = f(v)
		}
		for _, h := range v.hist {
			if h.node == nil {
				continue
			}
			it.History = append(it.History, HistoryEntry{Op: h.op, Value: f(h.node), Origin: h.node.origin()})
		}
		if len(it.History) == 0 {
			it.History = []HistoryEntry{{Op: OpSet, Value: it.Value, Origin: v.origin()}}
		}
		seen := map[string]bool{}
		for i := len(it.History) - 1; i >= 0; i-- {
			h := it.History[i]
			if h.Op == OpShadow {
				continue
			}
			key := fmt.Sprintf("%s:%d", h.Origin.SourceID, h.Origin.Line)
			if !seen[key] {
				seen[key] = true
				it.Origins = append([]Origin{h.Origin}, it.Origins...)
			}
		}
	}
	if it.Risk == "" {
		it.Risk = SeverityInfo
	}
	if it.RiskReason == "" && it.Risk == SeverityInfo {
		it.RiskReason = "一般的な設定値で、sandbox の外には影響しません。"
	}
	rs.items = append(rs.items, it)
	rs.itemIndex[it.ID] = len(rs.items) - 1
	return &rs.items[len(rs.items)-1]
}

// bound records a boundary unless its item belongs to a capability the
// host would skip, which grants nothing.
func (rs *resolver) bound(b Boundary) {
	if rs.skippedItems[b.ItemID] {
		return
	}
	rs.bounds = append(rs.bounds, b)
}

// formatters

func (rs *resolver) fmtPlain(v *vnode) string {
	switch v.kind {
	case parser.Scalar:
		return rs.r.text(v.disp)
	case parser.Seq:
		var parts []string
		for _, it := range v.items {
			parts = append(parts, rs.fmtPlain(it))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case parser.Map:
		var parts []string
		for _, k := range v.keys {
			parts = append(parts, k+": "+rs.fmtPlain(v.kv[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "null"
}

func (rs *resolver) fmtValue(v *vnode) string {
	switch v.kind {
	case parser.Scalar:
		return rs.r.value(v.disp)
	case parser.Seq:
		var parts []string
		for _, it := range v.items {
			parts = append(parts, rs.fmtValue(it))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case parser.Map:
		var parts []string
		for _, k := range v.keys {
			parts = append(parts, k+": "+rs.fmtValue(v.kv[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "null"
}

//declscope:shared // declared.go shows mount targets the way their items do
func (rs *resolver) fmtPath(v *vnode) string {
	if v.kind != parser.Scalar {
		return rs.fmtPlain(v)
	}
	return rs.r.path(resolveHostPath(v.fileDir, v.disp))
}

func (rs *resolver) url(s string) string {
	if !rs.r.strict() {
		return rs.r.kitRef(s)
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "<redacted url>"
	}
	out := u.Scheme + "://" + u.Host
	if u.Path != "" || u.RawQuery != "" {
		out += "/…"
	}
	return out
}

func (rs *resolver) fmtSecretSource(v *vnode) string {
	if v == nil || v.kind != parser.Map {
		// A scalar written where a source map belongs is still a secret.
		return rs.r.literalSecret()
	}
	var parts []string
	if v.has("value") {
		parts = append(parts, "value: "+rs.r.literalSecret())
	}
	if v.has("ref") {
		parts = append(parts, "ref: "+rs.r.secretRef(v.dstr("ref")))
	}
	if v.has("command") {
		parts = append(parts, "command: "+rs.r.command(v.dstr("command")))
	}
	for _, k := range []string{"snapshot", "refresh", "backend", "noVerify"} {
		if v.has(k) {
			parts = append(parts, k+": "+rs.r.text(v.dstr(k)))
		}
	}
	return strings.Join(parts, ", ")
}

func (rs *resolver) fmtPort(v *vnode) string {
	if v.kind != parser.Map {
		return rs.fmtPlain(v)
	}
	host := v.dstr("host")
	if host == "" {
		host = "ephemeral"
	}
	ip := v.dstr("hostIP")
	switch {
	case ip == "":
		ip = "loopback"
	case strings.Contains(ip, ":") && !strings.HasPrefix(ip, "["):
		ip = "[" + ip + "]"
	}
	return fmt.Sprintf("%s:%s -> sandbox:%s", ip, host, rs.portKey(v))
}

func (rs *resolver) fmtMCP(v *vnode) string {
	if v.kind != parser.Map {
		return rs.fmtPlain(v)
	}
	if v.has("url") {
		return "url: " + rs.url(v.dstr("url"))
	}
	cmd := v.dstr("command")
	if args := v.get("args"); args != nil {
		cmd += " " + strings.Join(args.dscalars(), " ")
	}
	return "command: " + rs.r.command(strings.TrimSpace(cmd))
}

func (rs *resolver) fmtLifecycle(v *vnode) string {
	if v.kind != parser.Map {
		return rs.r.command(rs.fmtPlain(v))
	}
	return rs.r.command(v.dstr("command"))
}

// workspacePath is an additionalWorkspaces entry's path: the entry itself
// when it is a bare path, else its path key (nil when it has none).
//
//declscope:shared // declared.go names a mount by the path its item shows
func workspacePath(e *vnode) *vnode {
	if e.kind == parser.Map {
		return e.get("path")
	}
	return e
}

func (rs *resolver) fmtWorkspaceEntry(v *vnode) string {
	if v.kind == parser.Scalar {
		return rs.fmtPath(v)
	}
	p := v.get("path")
	out := ""
	if p != nil {
		out = rs.fmtPath(p)
	}
	if t := v.dstr("target"); t != "" {
		out += " -> " + rs.r.path(t)
	}
	if v.str("readOnly") == "true" {
		out += " (ro)"
	}
	if v.str("clone") == "true" {
		out += " (clone)"
	}
	return out
}

func (rs *resolver) fmtKitEntry(v *vnode) string {
	out := rs.r.kitRef(kitSourceDisplay(v))
	if args := v.get("args"); args != nil && len(args.keys) > 0 {
		var parts []string
		for _, k := range args.keys {
			parts = append(parts, k+"="+rs.r.value(args.kv[k].disp))
		}
		out += " {" + strings.Join(parts, ", ") + "}"
	}
	return out
}

//declscope:shared // Resolve runs the build phase
func (rs *resolver) buildItems() {
	root := rs.root
	top := func(key string, sec Section, f fmtFn) *Item {
		v := root.get(key)
		if v == nil {
			return nil
		}
		return rs.addItem(Item{ID: key, Path: key, Section: sec}, v, f)
	}
	top("schemaVersion", SectionTop, rs.fmtPlain)
	if it := top("name", SectionTop, rs.fmtPlain); it != nil && rs.opts.Name != "" {
		it.History = append(shadowHistory(it.History), HistoryEntry{Op: OpReplace, Value: rs.r.text(rs.opts.Name), Origin: Origin{SourceID: "cli", Via: []string{"--name"}}})
		it.Value = rs.r.text(rs.opts.Name)
		it.Origins = []Origin{{SourceID: "cli", Via: []string{"--name"}}}
	} else if it == nil && rs.opts.Name != "" {
		rs.addItem(Item{ID: "name", Path: "name", Section: SectionTop, Value: rs.r.text(rs.opts.Name),
			Origins: []Origin{{SourceID: "cli", Via: []string{"--name"}}},
			History: []HistoryEntry{{Op: OpSet, Value: rs.r.text(rs.opts.Name), Origin: Origin{SourceID: "cli", Via: []string{"--name"}}}}}, nil, nil)
	}
	top("agent", SectionTop, rs.fmtPlain)
	top("entrypoint", SectionTop, rs.fmtValue)
	top("cmd", SectionTop, rs.fmtValue)

	rs.buildArgs()
	rs.buildWorkspace()
	rs.buildMounts()
	rs.buildMapScalars("env", SectionEnv, rs.fmtValue, SeverityInfo)
	rs.buildSandboxOptions()
	rs.buildSecrets()
	rs.buildBindings()
	rs.buildRegistries()
	rs.buildMCP()
	rs.buildPorts()
	rs.buildLifecycle()
	rs.buildKitEntries()
	for _, k := range rs.kits {
		for _, c := range k.caps {
			rs.buildCapability(c)
		}
	}
}

func shadowHistory(h []HistoryEntry) []HistoryEntry {
	out := make([]HistoryEntry, len(h))
	for i, e := range h {
		e.Op = OpShadow
		out[i] = e
	}
	return out
}

func (rs *resolver) buildArgs() {
	for _, name := range rs.argOrder {
		d := rs.argDecls[name]
		it := Item{ID: "args." + name, Path: "args." + name, Section: SectionArgs, Risk: SeverityInfo}
		var hist []HistoryEntry
		if d.defNode != nil {
			for _, h := range d.defNode.hist {
				op := h.op
				if op == OpSet {
					op = OpDefault
				}
				hist = append(hist, HistoryEntry{Op: op, Value: rs.r.value(h.node.val), Origin: h.node.origin()})
			}
		}
		val := ""
		if v, ok := rs.argValues[name]; ok {
			val = rs.r.value(v.Value)
			if v.From != "default" {
				hist = append(shadowHistory(hist), HistoryEntry{Op: OpReplace, Value: val, Origin: v.Origin})
			}
			it.Origins = []Origin{v.Origin}
		} else {
			val = "<unset>"
			it.Origins = []Origin{d.origin}
		}
		it.Value = val
		it.History = hist
		if d.required {
			it.Fields = append(it.Fields, Field{"required", "true"})
		}
		if len(d.enum) > 0 {
			it.Fields = append(it.Fields, Field{"enum", strings.Join(d.enum, " | ")})
		}
		if d.pattern != "" {
			it.Fields = append(it.Fields, Field{"pattern", d.pattern})
		}
		if d.description != "" {
			it.Fields = append(it.Fields, Field{"description", rs.r.text(d.description)})
		}
		rs.addItem(it, nil, nil)
	}
}

func (rs *resolver) buildWorkspace() {
	ws := rs.root.get("workspace")
	if ws != nil && ws.kind != parser.Null {
		pathNode := ws
		clone := false
		if ws.kind == parser.Map {
			pathNode = ws.get("path")
			clone = ws.str("clone") == "true"
		}
		if rs.opts.Clone != nil {
			clone = *rs.opts.Clone
		}
		it := rs.addItem(Item{ID: "workspace", Path: "workspace", Section: SectionWorkspace}, ws, rs.fmtWorkspaceEntry)
		if pathNode != nil {
			it.Value = rs.fmtPath(pathNode)
		}
		access := "rw"
		reason := "the primary workspace is bind-mounted read/write; the agent's edits land on the host directly"
		it.Risk, it.Badges = SeverityMedium, []string{BadgeRW}
		it.RiskReason = "ワークスペースのフォルダを sandbox が直接読み書きします。sandbox 内での変更があなたの PC のファイルにそのまま反映されます。"
		if clone {
			access = "clone"
			reason = "clone mode gives the sandbox a private Git clone; host files are not written until you fetch from it"
			it.Risk, it.Badges = SeverityLow, nil
			it.RiskReason = "clone モードなので sandbox は専用のコピーを編集し、あなたの PC のファイルは直接書き換わりません。"
			it.Fields = append(it.Fields, Field{"clone", "true"})
		}
		if rs.opts.Clone != nil {
			it.Fields = append(it.Fields, Field{"--clone", fmt.Sprint(*rs.opts.Clone)})
			it.History = append(it.History, HistoryEntry{Op: OpReplace, Value: "clone=" + fmt.Sprint(*rs.opts.Clone), Origin: Origin{SourceID: "cli", Via: []string{"--clone"}}})
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: access, Reason: reason})
	}
	for _, e := range rs.root.get("additionalWorkspaces").list() {
		target := rs.r.path(e.dstr("target"))
		if p := workspacePath(e); target == "" && p != nil {
			target = rs.fmtPath(p)
		}
		id := "additionalWorkspaces[" + target + "]"
		it := rs.addItem(Item{ID: id, Path: "additionalWorkspaces[]", Section: SectionWorkspace}, e, rs.fmtWorkspaceEntry)
		access := "rw"
		it.Risk, it.Badges = SeverityMedium, []string{BadgeRW}
		it.RiskReason = "追加のフォルダを sandbox が直接読み書きします。"
		if e.str("readOnly") == "true" {
			access = "ro"
			it.Risk, it.Badges = SeverityLow, []string{BadgeRO}
			it.RiskReason = "読み取り専用のマウントなので、sandbox からは書き換えられません。"
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: access,
			Reason: "an additional host directory is mounted directly, even when the primary workspace uses clone mode"})
	}
}

func (rs *resolver) buildMounts() {
	m := rs.root.get("mounts")
	if m == nil {
		return
	}
	for _, k := range m.keys {
		v := m.kv[k]
		it := rs.addItem(Item{ID: "mounts." + k, Path: "mounts." + k, Section: SectionMounts, Risk: SeverityHigh, Badges: []string{BadgeRW, BadgePersist},
			RiskReason: "あなたの PC のフォルダを sandbox と読み書き可能で共有し、sandbox を削除してもデータが残ります。"}, v, rs.fmtPath)
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
			Reason: "binds a kit-declared host-mount@1 path to this host directory; sandbox writes reach it and outlive the sandbox"})
	}
}

func (rs *resolver) buildMapScalars(key string, sec Section, f fmtFn, risk Severity) {
	m := rs.root.get(key)
	if m == nil {
		return
	}
	for _, k := range m.keys {
		rs.addItem(Item{ID: key + "." + k, Path: key + "." + k, Section: sec, Risk: risk}, m.kv[k], f)
	}
}

func (rs *resolver) buildSandboxOptions() {
	so := rs.root.get("sandboxOptions")
	if so == nil {
		return
	}
	for _, k := range so.keys {
		v := so.kv[k]
		f := rs.fmtPlain
		if k == "template" {
			f = func(n *vnode) string { return rs.r.kitRef(n.disp) }
		}
		it := rs.addItem(Item{ID: "sandboxOptions." + k, Path: "sandboxOptions." + k, Section: SectionSandbox}, v, f)
		val := v.val
		switch k {
		case "writableEnvFiles":
			if val == "true" {
				it.Risk, it.Badges = SeverityHigh, []string{BadgeRW, BadgeHost}
				it.RiskReason = "sandbox から環境ファイルを書き換えられるため、次回以降にあなたの PC で実行されるコマンドを変えられてしまいます。"
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
					Reason: "the sandbox can rewrite environment files, and with them the host commands later invocations run"})
			}
		case "skills":
			if val == "readwrite" {
				it.Risk, it.Badges = SeverityHigh, []string{BadgeRW, BadgePersist}
				it.RiskReason = "あなたの PC の共有スキル置き場を sandbox から書き換えられ、他の sandbox にも影響します。"
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
					Reason: "the shared agent skills store on the host becomes writable from this sandbox"})
			}
		case "gpu", "display", "fuse":
			if val == "true" {
				it.Risk = SeverityMedium
				it.RiskReason = "あなたの PC の装置（GPU・画面・FUSE）を sandbox から使えるようにします。"
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryDevice, Where: "host", Lifetime: "sandbox-scoped", Reason: "passes a host device or socket into the sandbox"})
			}
		case "usb":
			if len(v.list()) > 0 {
				it.Risk = SeverityMedium
				it.RiskReason = "あなたの PC の USB 機器を sandbox から使えるようにします。"
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryDevice, Where: "host", Lifetime: "sandbox-scoped", Reason: "passes host USB devices into the sandbox"})
			}
		case "template":
			it.Risk = SeverityLow
			it.RiskReason = "sandbox の土台になるイメージの指定です。イメージの中身はこのツールでは確認できません。"
		}
	}
}

func (rs *resolver) buildSecrets() {
	m := rs.root.get("secrets")
	if m == nil {
		return
	}
	for _, svc := range m.keys {
		v := m.kv[svc]
		it := rs.addItem(Item{ID: "secrets." + svc, Path: "secrets." + svc, Section: SectionSecrets, Risk: SeverityMedium, Badges: []string{BadgeCred},
			RiskReason: "サービスの認証情報（API キーなど）を sandbox からの通信に使います。"}, v, rs.fmtSecretSource)
		for _, k := range []string{"snapshot", "refresh", "backend", "noVerify"} {
			if v.has(k) {
				it.Fields = append(it.Fields, Field{k, rs.r.text(v.dstr(k))})
			}
		}
		switch {
		case v.has("command"):
			it.Risk = SeverityHigh
			it.RiskReason = "秘密を取り出すためにあなたの PC でコマンドを実行します（--skip-host-commands でも止まりません）。"
			it.Badges = append(it.Badges, BadgeHost, BadgeOpaque)
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "invocation", Opaque: true,
				Reason: "the command runs on the host with your privileges to produce the secret; --skip-host-commands does not skip it"})
		case v.has("value"):
			it.Risk = SeverityHigh
			it.RiskReason = "秘密が平文でファイルに直接書かれています。ファイルを読める人なら誰でも見られます。"
			it.Badges = append(it.Badges, BadgeLiteral)
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryProxyRoute, Where: "host", Lifetime: "sandbox-scoped",
			Reason: "stored at sandbox scope on the host; the proxy presents it on matching outbound requests"})
	}
}

func (rs *resolver) buildBindings() {
	m := rs.root.get("bindings")
	if m == nil {
		return
	}
	for _, svc := range m.keys {
		b := m.kv[svc]
		for _, mech := range []string{"apiKey", "oauth"} {
			mv := b.get(mech)
			if mv == nil {
				continue
			}
			dom := mv.get("domains")
			id := "bindings." + svc + "." + mech
			f := func(n *vnode) string {
				if d := n.get("domains"); d != nil {
					return strings.Join(d.dscalars(), ", ")
				}
				return rs.fmtPlain(n)
			}
			it := rs.addItem(Item{ID: id, Path: id, Section: SectionBindings, Risk: SeverityMedium, Badges: []string{BadgeCred, BadgePersist},
				RiskReason: "認証情報を差し込んでよい通信先の承認で、あなたの PC 全体の設定（credentials.yaml）に残ります。"}, mv, f)
			if dom != nil {
				it.Fields = append(it.Fields, Field{"domains", strings.Join(dom.dscalars(), ", ")})
			}
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostGlobal, Where: "host", Lifetime: "host-persistent",
				Reason: "merged into the global credentials.yaml; it survives sbx env rm unless --prune-bindings is given and is shared with other sandboxes"})
		}
	}
}

func (rs *resolver) buildRegistries() {
	m := rs.root.get("registries")
	if m == nil {
		return
	}
	for _, host := range m.keys {
		v := m.kv[host]
		f := func(n *vnode) string {
			var parts []string
			if u := n.get("username"); u != nil {
				parts = append(parts, "username: "+rs.fmtSecretSource(u))
			}
			if s := n.get("secret"); s != nil {
				parts = append(parts, "secret: "+rs.fmtSecretSource(s))
			}
			return strings.Join(parts, "; ")
		}
		it := rs.addItem(Item{ID: "registries." + host, Path: "registries." + host, Section: SectionRegistries, Risk: SeverityMedium, Badges: []string{BadgeCred},
			RiskReason: "イメージを取得するための認証情報を使います。"}, v, f)
		for _, sub := range []string{"username", "secret"} {
			if s := v.get(sub); s != nil && s.has("command") {
				it.Risk = SeverityHigh
				it.RiskReason = "認証情報を取り出すためにあなたの PC でコマンドを実行します。"
				it.Badges = append(it.Badges, BadgeHost, BadgeOpaque)
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "invocation", Opaque: true,
					Reason: "the registry credential command runs on the host"})
				break
			}
		}
	}
}

func (rs *resolver) buildMCP() {
	for _, e := range rs.root.get("mcp").get("servers").list() {
		name := rs.r.text(e.dstr("name"))
		it := rs.addItem(Item{ID: "mcp.servers[" + name + "]", Path: "mcp.servers[]", Section: SectionMCP}, e, rs.fmtMCP)
		if e.has("command") {
			it.Risk = SeverityHigh
			it.RiskReason = "MCP サーバーをあなたの PC の上で、あなたの権限で動かします。"
			it.Badges = []string{BadgeHost, BadgeOpaque}
			b := Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "sandbox-scoped", Opaque: true,
				Reason: "a local stdio MCP server runs on the host with your privileges whenever the sandbox's MCP gateway starts"}
			if rs.opts.SkipHostCommands {
				b.Skipped = true
				it.Risk = SeverityLow
				it.RiskReason = "--skip-host-commands を付けているため、このコマンドは実行されません。"
				it.Badges = append(it.Badges, BadgeSkip)
				b.Reason += " (disabled by --skip-host-commands)"
			}
			rs.bound(b)
		} else {
			it.Risk = SeverityMedium
			it.RiskReason = "リモートの MCP サーバーをあなたの PC 全体に登録します（sandbox を削除しても残ります）。"
			it.Badges = []string{BadgePersist}
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostGlobal, Where: "unknown", Lifetime: "host-persistent",
				Reason: "a remote MCP server registered with url stays host-global and survives sbx env rm; traffic goes through the MCP gateway"})
		}
	}
}

func (rs *resolver) buildPorts() {
	for _, e := range rs.root.get("ports").list() {
		it := rs.addItem(Item{ID: "ports[" + rs.portKey(e) + "]", Path: "ports[]", Section: SectionPorts, Risk: SeverityLow,
			RiskReason: "あなたの PC の中からだけ接続できるポートです。"}, e, rs.fmtPort)
		ip := e.str("hostIP")
		reason := "publishes a sandbox port on the host loopback interface"
		if ip == "0.0.0.0" || ip == "::" || ip == "[::]" {
			it.Risk = SeverityHigh
			it.RiskReason = "LAN など外部のネットワークから、このポートに接続できます。"
			reason = "publishes a sandbox port on every host interface, reachable from the network"
		} else if a, err := netip.ParseAddr(strings.Trim(ip, "[]")); ip != "" && ip != "localhost" && (err != nil || !a.IsLoopback()) {
			it.Risk = SeverityMedium
			it.RiskReason = "特定のネットワークアドレスに公開されるため、そのネットワーク上の他の機械から接続できる場合があります。"
			reason = "publishes a sandbox port on a non-loopback host address"
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostPort, Where: "host", Lifetime: "sandbox-scoped", Reason: reason})
	}
}

func (rs *resolver) buildLifecycle() {
	lc := rs.root.get("lifecycle")
	for _, phase := range []string{"initialize", "postCreate", "preRemove"} {
		for i, e := range lc.get(phase).list() {
			id := fmt.Sprintf("lifecycle.%s[%d]", phase, i)
			it := rs.addItem(Item{ID: id, Path: "lifecycle." + phase + "[]", Section: SectionLifecycle, Risk: SeverityHigh, Badges: []string{BadgeHost, BadgeOpaque},
				RiskReason: "sandbox の作成・削除のタイミングで、あなたの PC の上でコマンドをあなたの権限で実行します。コマンドの中身はこのツールでは確認しません。"}, e, rs.fmtLifecycle)
			if n := e.dstr("name"); n != "" {
				it.Fields = append(it.Fields, Field{"name", rs.r.text(n)})
			}
			if w := e.get("workdir"); w != nil {
				it.Fields = append(it.Fields, Field{"workdir", rs.fmtPath(w)})
			}
			if t := e.dstr("timeout"); t != "" {
				it.Fields = append(it.Fields, Field{"timeout", t})
			}
			b := Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "invocation", Opaque: true,
				Reason: "a lifecycle " + phase + " command runs on the host with your user privileges; its effect is not analysed"}
			if rs.opts.SkipHostCommands {
				b.Skipped = true
				b.Reason += " (skipped by --skip-host-commands)"
				it.Risk = SeverityLow
				it.RiskReason = "--skip-host-commands を付けているため、このコマンドは実行されません。"
				it.Badges = append(it.Badges, BadgeSkip)
			}
			rs.bound(b)
		}
	}
}

func (rs *resolver) buildKitEntries() {
	for _, k := range rs.kits {
		id := "kits[" + k.shown + "]"
		it := rs.addItem(Item{ID: id, Path: "kits[]", Section: SectionKits, Kit: k.handleShown, Risk: SeverityLow,
			RiskReason: "ローカルの Kit で、要求している内容をこのツールで確認済みです。"}, k.entry, rs.fmtKitEntry)
		status := "expanded"
		switch {
		case k.expanded:
		case k.kind == kitLocal:
			// A local kit the user pointed at but that could not be read
			// is a real problem worth surfacing.
			status = "not expanded"
			it.Risk = SeverityMedium
			it.RiskReason = "ローカルの Kit を読み込めませんでした。通信・認証情報・フォルダ共有など何を要求するかは、作成時まで分かりません。"
			it.Badges = append(it.Badges, BadgeUnknown)
		default:
			// Remote kits are never fetched: that is the premise of a
			// static analysis, not a finding, so it stays informational.
			status = "not expanded"
			it.Risk = SeverityInfo
			it.RiskReason = "インターネット上の Kit です。このツールは静的解析のため中身を取得しません。作成前に sbx env plan で実行計画を確認してください。"
		}
		it.Fields = append(it.Fields, Field{"kind", k.kind}, Field{"handle", k.handleShown}, Field{"status", status})
		if k.descKind != "" {
			it.Fields = append(it.Fields, Field{"descriptor kind", k.descKind})
		}
		if k.srcID != "" {
			it.Fields = append(it.Fields, Field{"descriptor", k.srcID})
		}
		for _, name := range sortedKeys(k.args) {
			a := k.args[name]
			it.Fields = append(it.Fields, Field{"arg " + name, rs.r.value(a.Value) + " (" + a.From + ")"})
		}
		for _, name := range sortedKeys(k.argDecls) {
			if d := k.argDecls[name]; d.export != "" {
				it.Fields = append(it.Fields, Field{"exports " + d.export, name})
			}
		}
	}
}

func (rs *resolver) capItem(c *capState, suffix string, v *vnode, value string, risk Severity, reason string, badges ...string) *Item {
	id := "kit:" + c.kit.handleShown + "/" + c.short
	if suffix != "" {
		id += "/" + suffix
	}
	it := Item{ID: id, Path: "cap:" + c.short, Section: SectionCapabilities, Kit: c.kit.handleShown, Value: value, Risk: risk, RiskReason: reason, Badges: badges}
	o := c.origin
	if v != nil {
		o = Origin{SourceID: v.src, Line: v.pos.Line, Col: v.pos.Col, Via: c.kit.via()}
	}
	it.Origins = []Origin{o}
	it.History = []HistoryEntry{{Op: OpSet, Value: value, Origin: o}}
	if c.optional {
		it.Fields = append(it.Fields, Field{"optional", "true"})
	}
	if c.name != "" {
		it.Fields = append(it.Fields, Field{"name", rs.r.text(c.name)})
	}
	if c.description != "" {
		it.Fields = append(it.Fields, Field{"description", rs.r.text(c.description)})
	}
	if c.skipped {
		it.Risk = SeverityInfo
		it.RiskReason = "条件を満たさず省略されるため、この要求は何も許可しません。"
		it.Badges = []string{BadgeSkip}
		it.Fields = append(it.Fields, Field{"skipped", c.skipReason})
	}
	added := rs.addItem(it, nil, nil)
	if c.itemID == "" {
		c.itemID = added.ID
	}
	if c.skipped {
		rs.skippedItems[added.ID] = true
	}
	return added
}

func (rs *resolver) buildCapability(c *capState) {
	cfg := c.config
	if !strings.HasPrefix(c.typ, capNamespace) {
		rs.buildOpaqueCap(c, true)
		return
	}
	switch c.short {
	case "network-policy@1", "network-policy@2":
		for _, e := range rs.net.byCapability[c] {
			rs.buildNetEntry(c, e)
		}
	case "credential@1":
		svc := rs.r.text(cfg.dstr("service"))
		ph := phases(cfg.get("phase"))
		var parts []string
		parts = append(parts, "service="+svc, "phase="+strings.Join(ph, ","))
		ak := cfg.get("apiKey")
		if ak != nil {
			if n := rs.r.text(ak.dstr("name")); n != "" {
				parts = append(parts, "env="+n)
			}
			var doms []string
			for _, inj := range ak.get("inject").list() {
				doms = append(doms, rs.r.text(inj.dstr("domain")))
			}
			if len(doms) > 0 {
				parts = append(parts, "inject="+strings.Join(doms, ","))
			}
		}
		oa := cfg.get("oauth")
		if oa != nil {
			parts = append(parts, "oauth")
		}
		risk := SeverityMedium
		why := "サービスの認証情報を、指定した通信先への接続に自動で差し込みます。本物の値はあなたの PC に残ります。"
		badges := []string{BadgeCred}
		if oa.str("passthrough") == "true" {
			risk = SeverityHigh
			why = "本物のトークンがそのまま sandbox に渡ります。"
		}
		it := rs.capItem(c, svc+"/"+strings.Join(ph, ","), cfg, strings.Join(parts, " "), risk, why, badges...)
		if ak != nil {
			it.Fields = append(it.Fields, Field{"proxyManaged", orDefault(ak.str("proxyManaged"), "false")})
		}
		if oa != nil {
			it.Fields = append(it.Fields, Field{"oauth.resourceHosts", strings.Join(oa.get("resourceHosts").dscalars(), ", ")})
		}
		where := "host"
		reason := "the host proxy injects the " + svc + " credential on matching requests; the real value stays on the host"
		if ak != nil && ak.str("proxyManaged") != "true" && ak.str("name") != "" {
			reason = "the " + svc + " credential is not proxy-managed, so the sandbox environment variable may carry the real value"
			it.Risk = SeverityHigh
			it.RiskReason = "本物の認証情報が sandbox の環境変数に入る可能性があります。"
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryProxyRoute, Where: where, Lifetime: "sandbox-scoped", Reason: reason})
	case "host-mount@1":
		p := rs.r.text(cfg.dstr("path"))
		it := rs.capItem(c, p, cfg.get("path"), p, SeverityHigh, "あなたの PC のフォルダを sandbox と読み書き可能で共有し、sandbox を削除してもデータが残ります。", BadgeRW, BadgePersist)
		if m := cfg.str("mode"); m != "" {
			it.Fields = append(it.Fields, Field{"mode", m})
		}
		if bound := rs.root.get("mounts").get(cfg.str("path")); bound != nil {
			// The mounts: item already carries this directory's boundary.
			it.Fields = append(it.Fields, Field{"host directory", rs.fmtPath(bound)})
		} else {
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
				Reason: "a runtime-owned host directory shared by every sandbox that composes this kit; it survives sandbox removal"})
		}
	case "ssh-agent@1":
		ph := phases(cfg.get("phase"))
		unrestricted := cfg == nil || cfg.str("unrestricted") != "false"
		value := "phase=" + strings.Join(ph, ",")
		risk := SeverityMedium
		if unrestricted {
			value += " unrestricted"
			risk = SeverityHigh
		} else {
			if s := cfg.get("sign").dscalars(); len(s) > 0 {
				value += " sign=" + strings.Join(s, ",")
			}
			if a := cfg.get("authenticate").dscalars(); len(a) > 0 {
				value += " authenticate=" + strings.Join(a, ",")
			}
		}
		why := "あなたの SSH 鍵を、許可した用途（署名・ログイン先）に限って sandbox から使えます。"
		if unrestricted {
			why = "あなたの SSH 鍵を使って、sandbox から何にでも署名できてしまいます。"
		}
		it := rs.capItem(c, strings.Join(ph, ","), cfg, value, risk, why, BadgeSSH, BadgeHost)
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundarySSHAgent, Where: "host", Lifetime: "sandbox-scoped",
			Reason: "relays signing requests to the host SSH agent; private keys stay on the host but the sandbox can use them"})
	case "git-identity@1":
		it := rs.capItem(c, "", nil, "user.name / user.email from the host", SeverityLow, "あなたの Git の名前とメールアドレスを sandbox に伝えます（認証や署名の権限は含みません）。")
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryIdentity, Where: "host", Lifetime: "sandbox-scoped",
			Reason: "discloses your selected Git name and email to the sandbox (attribution only, not authentication)"})
	case "lifecycle@1":
		for _, ph := range []string{"install", "startup"} {
			for i, h := range cfg.get(ph).list() {
				cmd := h.get("command")
				text := ""
				if cmd != nil {
					if cmd.kind == parser.Seq {
						text = strings.Join(cmd.dscalars(), " ")
					} else {
						text = cmd.disp
					}
				}
				it := rs.capItem(c, fmt.Sprintf("%s[%d]", ph, i), h, rs.r.command(text), SeverityLow, "sandbox の中でコマンドを実行します。あなたの PC には直接影響しません。", BadgeOpaque)
				if u := h.dstr("user"); u != "" {
					it.Fields = append(it.Fields, Field{"user", u})
				}
				if env := h.get("env").dscalars(); len(env) > 0 {
					for i, e := range env {
						env[i], _, _ = strings.Cut(e, "=")
					}
					it.Fields = append(it.Fields, Field{"env", strings.Join(env, ", ")})
				}
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundarySandboxExec, Where: "sandbox", Lifetime: "sandbox-scoped", Opaque: true,
					Reason: "a kit " + ph + " hook runs inside the sandbox; what it can reach is bounded by the kit's other capabilities"})
			}
		}
		for i, f := range cfg.get("files").list() {
			it := rs.capItem(c, fmt.Sprintf("files[%d]", i), f, rs.r.text(f.dstr("path")), SeverityInfo, "sandbox の中にファイルを書き込む設定です。")
			if m := f.str("mode"); m != "" {
				it.Fields = append(it.Fields, Field{"mode", m})
			}
		}
		if inter := cfg.get("interactive"); inter != nil {
			rs.capItem(c, "interactive", inter, rs.r.command(strings.Join(inter.dscalars(), " ")), SeverityInfo, "対話モードで起動するときの引数です。")
		}
	default:
		rs.buildOpaqueCap(c, false)
	}
}

// buildNetEntry renders one network-policy entry. An allowed host that an
// unbounded deny covers, in any kit, grants nothing: deny wins.
func (rs *resolver) buildNetEntry(c *capState, e *netEntry) {
	shown := rs.r.text(e.host.disp)
	value := e.phase + " " + e.list + " " + shown
	if e.bounded() {
		value += " [" + rs.r.text(strings.Join(e.methods, ",")) + "]"
		if len(e.paths) > 0 {
			value += " " + rs.r.text(strings.Join(e.paths, " "))
		}
	}
	risk, badges := SeverityLow, []string{BadgeNet}
	why := "sandbox からこの通信先への接続を許可します。"
	pat := e.host.val
	switch {
	case e.list == "deny":
		risk, badges = SeverityInfo, nil
		why = "この通信先への接続を禁止します（安全側の設定です）。"
	case (pat == "*" || pat == "**") && !e.bounded():
		risk = SeverityHigh
		why = "インターネット上のすべての通信先への接続を許可します。"
	case strings.Contains(pat, "*") && e.phase == "runtime":
		risk = SeverityMedium
		why = "ワイルドカード（*）で、複数の通信先をまとめて許可します。"
	}
	if e.list == "allow" && e.phase == "install" && risk == SeverityLow {
		why = "インストール処理の間だけ、この通信先への接続を許可します。"
	}
	var deny *netEntry
	if e.list == "allow" {
		deny = rs.net.deniedBy(e.phase, pat)
		if deny != nil {
			risk, badges = SeverityInfo, nil
			why = "許可されていますが、別の禁止設定が優先されるため、実際には接続できません。"
		}
	}
	it := rs.capItem(c, e.phase+"."+e.list+"/"+shown, e.host, value, risk, why, badges...)
	e.itemID = it.ID
	if deny != nil {
		it.Fields = append(it.Fields, Field{"denied by", "Kit " + deny.cap.kit.handleShown + " の" + phaseJA(deny.phase) + "の通信禁止 " + rs.r.text(deny.host.disp)})
		return
	}
	if e.list == "allow" {
		reason := phaseReason(e.phase) + " egress to " + shown + " is allowed through the host proxy"
		if e.bounded() {
			reason += ", limited to the listed methods and paths"
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryNetwork, Where: "sandbox", Lifetime: lifetimeForPhase(e.phase), Reason: reason})
	}
}

// buildOpaqueCap keeps a capability this tool does not model, verbatim.
func (rs *resolver) buildOpaqueCap(c *capState, unknownNS bool) {
	var fields []Field
	flattenFields(c.config, "", rs, &fields)
	risk := SeverityMedium
	switch c.short {
	case "privileged@1", "dockerd@1":
		risk = SeverityHigh
	case "host-port@1":
		risk = SeverityHigh
	}
	why := "このツールが意味を解釈していない設定です。内容はそのまま表示していますが、影響は確認できません。"
	switch c.short {
	case "privileged@1", "dockerd@1":
		why = "sandbox の中で最も強い権限（特権モード）を要求します。"
	case "host-port@1":
		why = "あなたの PC のサービスに sandbox から直接接続できるようにします。"
	}
	it := rs.capItem(c, "", c.config, c.typ, risk, why, BadgeUnknown)
	it.Fields = append(it.Fields, fields...)
	kind := CoverageUnmodeledCap
	reason := "this capability type is preserved as-is but its effect is not modelled"
	if unknownNS {
		kind = CoverageUnknownCap
		reason = "capability type outside the com.docker.sandbox namespace; sbx refuses it if required and skips it if optional"
	}
	rs.coverage(kind, c.kit.handleShown+": "+c.typ, reason, c.origin)
	switch c.short {
	case "privileged@1", "dockerd@1":
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryDevice, Where: "sandbox", Lifetime: "sandbox-scoped", Reason: "requests elevated privileges inside the sandbox VM"})
	case "host-port@1":
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostPort, Where: "host", Lifetime: "sandbox-scoped", Reason: "grants the sandbox raw access to a host loopback service"})
	case "volume@1":
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "sandbox", Lifetime: "sandbox-scoped", Access: "rw", Reason: "private block storage owned by the sandbox instance; deleted with the sandbox"})
	}
}

func flattenFields(v *vnode, prefix string, rs *resolver, out *[]Field) {
	if v == nil {
		return
	}
	switch v.kind {
	case parser.Scalar:
		*out = append(*out, Field{prefix, rs.r.value(v.disp)})
	case parser.Map:
		for _, k := range v.keys {
			flattenFields(v.kv[k], joinKey(prefix, k), rs, out)
		}
	case parser.Seq:
		for i, it := range v.items {
			flattenFields(it, fmt.Sprintf("%s[%d]", prefix, i), rs, out)
		}
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func lifetimeForPhase(ph string) string {
	if ph == "install" {
		return "invocation"
	}
	return "sandbox-scoped"
}

func phaseReason(ph string) string {
	if ph == "install" {
		return "during install hooks only,"
	}
	return "while the agent runs,"
}

// portKey is the display form of a port's identity: sandbox port and
// effective protocol.
//
//declscope:shared // declared.go names a port by the same key as its item
func (rs *resolver) portKey(v *vnode) string {
	proto := strings.ToLower(v.dstr("protocol"))
	if proto == "" {
		proto = effectiveProtocol(v)
	}
	return rs.r.text(v.dstr("sandbox")) + "/" + rs.r.text(proto)
}
