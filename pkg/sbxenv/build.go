package sbxenv

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
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
	rs.items = append(rs.items, it)
	rs.itemIndex[it.ID] = len(rs.items) - 1
	return &rs.items[len(rs.items)-1]
}

func (rs *resolver) bound(b Boundary) { rs.bounds = append(rs.bounds, b) }

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

func (rs *resolver) fmtPath(v *vnode) string {
	if v.kind != parser.Scalar {
		return rs.fmtPlain(v)
	}
	return rs.r.path(resolveHostPath(v.fileDir, v.disp))
}

func (rs *resolver) url(s string) string {
	if !rs.r.strict() {
		return rs.kitRef(s)
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
	if ip == "" {
		ip = "loopback"
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
	out := rs.kitRef(kitSourceDisplay(v))
	if args := v.get("args"); args != nil && len(args.keys) > 0 {
		var parts []string
		for _, k := range args.keys {
			parts = append(parts, k+"="+rs.r.value(args.kv[k].disp))
		}
		out += " {" + strings.Join(parts, ", ") + "}"
	}
	return out
}

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
		if d.def != nil {
			hist = append(hist, HistoryEntry{Op: OpDefault, Value: rs.r.value(*d.def), Origin: d.defOrigin})
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
		if clone {
			access = "clone"
			reason = "clone mode gives the sandbox a private Git clone; host files are not written until you fetch from it"
			it.Risk, it.Badges = SeverityLow, nil
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
		if target == "" {
			target = rs.fmtPath(e.get("path"))
		}
		id := "additionalWorkspaces[" + target + "]"
		it := rs.addItem(Item{ID: id, Path: "additionalWorkspaces[]", Section: SectionWorkspace}, e, rs.fmtWorkspaceEntry)
		access := "rw"
		it.Risk, it.Badges = SeverityMedium, []string{BadgeRW}
		if e.str("readOnly") == "true" {
			access = "ro"
			it.Risk, it.Badges = SeverityLow, []string{BadgeRO}
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
		it := rs.addItem(Item{ID: "mounts." + k, Path: "mounts." + k, Section: SectionMounts, Risk: SeverityHigh, Badges: []string{BadgeRW, BadgePersist}}, v, rs.fmtPath)
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
			f = func(n *vnode) string { return rs.kitRef(n.disp) }
		}
		it := rs.addItem(Item{ID: "sandboxOptions." + k, Path: "sandboxOptions." + k, Section: SectionSandbox}, v, f)
		val := v.val
		switch k {
		case "writableEnvFiles":
			if val == "true" {
				it.Risk, it.Badges = SeverityHigh, []string{BadgeRW, BadgeHost}
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
					Reason: "the sandbox can rewrite environment files, and with them the host commands later invocations run"})
			}
		case "skills":
			if val == "readwrite" {
				it.Risk, it.Badges = SeverityHigh, []string{BadgeRW, BadgePersist}
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw",
					Reason: "the shared agent skills store on the host becomes writable from this sandbox"})
			}
		case "gpu", "display", "fuse":
			if val == "true" {
				it.Risk = SeverityMedium
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryDevice, Where: "host", Lifetime: "sandbox-scoped", Reason: "passes a host device or socket into the sandbox"})
			}
		case "usb":
			if len(v.list()) > 0 {
				it.Risk = SeverityMedium
				rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryDevice, Where: "host", Lifetime: "sandbox-scoped", Reason: "passes host USB devices into the sandbox"})
			}
		case "template":
			it.Risk = SeverityLow
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
		it := rs.addItem(Item{ID: "secrets." + svc, Path: "secrets." + svc, Section: SectionSecrets, Risk: SeverityMedium, Badges: []string{BadgeCred}}, v, rs.fmtSecretSource)
		for _, k := range []string{"snapshot", "refresh", "backend", "noVerify"} {
			if v.has(k) {
				it.Fields = append(it.Fields, Field{k, rs.r.text(v.dstr(k))})
			}
		}
		switch {
		case v.has("command"):
			it.Risk = SeverityHigh
			it.Badges = append(it.Badges, BadgeHost, BadgeOpaque)
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "invocation", Opaque: true,
				Reason: "the command runs on the host with your privileges to produce the secret; --skip-host-commands does not skip it"})
		case v.has("value"):
			it.Risk = SeverityHigh
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
			it := rs.addItem(Item{ID: id, Path: id, Section: SectionBindings, Risk: SeverityMedium, Badges: []string{BadgeCred, BadgePersist}}, mv, f)
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
		it := rs.addItem(Item{ID: "registries." + host, Path: "registries." + host, Section: SectionRegistries, Risk: SeverityMedium, Badges: []string{BadgeCred}}, v, f)
		for _, sub := range []string{"username", "secret"} {
			if s := v.get(sub); s != nil && s.has("command") {
				it.Risk = SeverityHigh
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
			it.Badges = []string{BadgeHost, BadgeOpaque}
			b := Boundary{ItemID: it.ID, Kind: BoundaryHostExec, Where: "host", Lifetime: "sandbox-scoped", Opaque: true,
				Reason: "a local stdio MCP server runs on the host with your privileges whenever the sandbox's MCP gateway starts"}
			if rs.opts.SkipHostCommands {
				b.Skipped = true
				it.Risk = SeverityLow
				it.Badges = append(it.Badges, BadgeSkip)
				b.Reason += " (disabled by --skip-host-commands)"
			}
			rs.bound(b)
		} else {
			it.Risk = SeverityMedium
			it.Badges = []string{BadgePersist}
			rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostGlobal, Where: "unknown", Lifetime: "host-persistent",
				Reason: "a remote MCP server registered with url stays host-global and survives sbx env rm; traffic goes through the MCP gateway"})
		}
	}
}

func (rs *resolver) buildPorts() {
	for _, e := range rs.root.get("ports").list() {
		it := rs.addItem(Item{ID: "ports[" + rs.portKey(e) + "]", Path: "ports[]", Section: SectionPorts, Risk: SeverityLow}, e, rs.fmtPort)
		ip := e.str("hostIP")
		reason := "publishes a sandbox port on the host loopback interface"
		if ip == "0.0.0.0" || ip == "::" || ip == "[::]" {
			it.Risk = SeverityHigh
			reason = "publishes a sandbox port on every host interface, reachable from the network"
		} else if ip != "" && ip != "127.0.0.1" && ip != "::1" {
			it.Risk = SeverityMedium
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
			it := rs.addItem(Item{ID: id, Path: "lifecycle." + phase + "[]", Section: SectionLifecycle, Risk: SeverityHigh, Badges: []string{BadgeHost, BadgeOpaque}}, e, rs.fmtLifecycle)
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
				it.Badges = append(it.Badges, BadgeSkip)
			}
			rs.bound(b)
		}
	}
}

func (rs *resolver) buildKitEntries() {
	for _, k := range rs.kits {
		id := "kits[" + k.shown + "]"
		it := rs.addItem(Item{ID: id, Path: "kits[]", Section: SectionKits, Kit: k.handleShown, Risk: SeverityLow}, k.entry, rs.fmtKitEntry)
		status := "expanded"
		if !k.expanded {
			status = "not expanded"
			it.Risk = SeverityMedium
			it.Badges = append(it.Badges, BadgeUnknown)
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
	}
}

func phases(v *vnode) []string {
	if v == nil {
		return nil
	}
	if v.kind == parser.Scalar {
		return []string{v.val}
	}
	return v.scalars()
}

func (rs *resolver) capItem(c *capState, suffix string, v *vnode, value string, risk Severity, badges ...string) *Item {
	id := "kit:" + c.kit.handleShown + "/" + c.short
	if suffix != "" {
		id += "/" + suffix
	}
	it := Item{ID: id, Path: "cap:" + c.short, Section: SectionCapabilities, Kit: c.kit.handleShown, Value: value, Risk: risk, Badges: badges}
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
	return rs.addItem(it, nil, nil)
}

func (rs *resolver) buildCapability(c *capState) {
	cfg := c.config
	if !strings.HasPrefix(c.typ, capNamespace) {
		rs.buildOpaqueCap(c, true)
		return
	}
	switch c.short {
	case "network-policy@1":
		for _, ph := range []string{"install", "runtime"} {
			for _, list := range []string{"allow", "deny"} {
				for _, e := range cfg.get(ph).get(list).list() {
					pat := e.val
					shown := rs.r.text(e.disp)
					risk, badges := SeverityLow, []string{BadgeNet}
					if list == "deny" {
						risk, badges = SeverityInfo, nil
					} else if pat == "*" || pat == "**" {
						risk = SeverityHigh
					} else if strings.Contains(pat, "*") && ph == "runtime" {
						risk = SeverityMedium
					}
					it := rs.capItem(c, ph+"."+list+"/"+shown, e, ph+" "+list+" "+shown, risk, badges...)
					if list == "allow" {
						rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryNetwork, Where: "sandbox", Lifetime: lifetimeForPhase(ph),
							Reason: phaseReason(ph) + " egress to " + shown + " is allowed through the host proxy"})
					}
				}
			}
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
		badges := []string{BadgeCred}
		if oa.str("passthrough") == "true" {
			risk = SeverityHigh
		}
		it := rs.capItem(c, svc+"/"+strings.Join(ph, ","), cfg, strings.Join(parts, " "), risk, badges...)
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
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryProxyRoute, Where: where, Lifetime: "sandbox-scoped", Reason: reason})
	case "host-mount@1":
		p := rs.r.text(cfg.dstr("path"))
		it := rs.capItem(c, p, cfg.get("path"), p, SeverityHigh, BadgeRW, BadgePersist)
		if m := cfg.str("mode"); m != "" {
			it.Fields = append(it.Fields, Field{"mode", m})
		}
		bound := rs.root.get("mounts").get(cfg.str("path"))
		reason := "a runtime-owned host directory shared by every sandbox that composes this kit; it survives sandbox removal"
		if bound != nil {
			reason = "bound to " + rs.fmtPath(bound) + " by mounts:; data survives sandbox removal"
			it.Fields = append(it.Fields, Field{"host directory", rs.fmtPath(bound)})
		}
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundaryHostMount, Where: "host", Lifetime: "host-persistent", Access: "rw", Reason: reason})
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
		it := rs.capItem(c, strings.Join(ph, ","), cfg, value, risk, BadgeSSH, BadgeHost)
		rs.bound(Boundary{ItemID: it.ID, Kind: BoundarySSHAgent, Where: "host", Lifetime: "sandbox-scoped",
			Reason: "relays signing requests to the host SSH agent; private keys stay on the host but the sandbox can use them"})
	case "git-identity@1":
		it := rs.capItem(c, "", nil, "user.name / user.email from the host", SeverityLow)
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
				it := rs.capItem(c, fmt.Sprintf("%s[%d]", ph, i), h, rs.r.command(text), SeverityLow, BadgeOpaque)
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
			it := rs.capItem(c, fmt.Sprintf("files[%d]", i), f, rs.r.text(f.dstr("path")), SeverityInfo)
			if m := f.str("mode"); m != "" {
				it.Fields = append(it.Fields, Field{"mode", m})
			}
		}
		if inter := cfg.get("interactive"); inter != nil {
			rs.capItem(c, "interactive", inter, rs.r.command(strings.Join(inter.dscalars(), " ")), SeverityInfo)
		}
	default:
		rs.buildOpaqueCap(c, false)
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
	it := rs.capItem(c, "", c.config, c.typ, risk, BadgeUnknown)
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
func (rs *resolver) portKey(v *vnode) string {
	proto := strings.ToLower(v.dstr("protocol"))
	if proto == "" {
		proto = effectiveProtocol(v)
	}
	return rs.r.text(v.dstr("sandbox")) + "/" + rs.r.text(proto)
}

// kitSourceDisplay returns a kit entry's source as it may be shown.
func kitSourceDisplay(e *vnode) string {
	if e.kind == parser.Scalar {
		return e.disp
	}
	return e.dstr("source")
}

// kitRef formats a kit or image reference: $HOME shortened and any
// credentials in a URL authority removed, in every mode.
func (rs *resolver) kitRef(s string) string { return rs.r.path(stripUserinfo(s)) }

// sortItems orders items by section, then ID.
func sortItems(items []Item) {
	order := map[Section]int{}
	for i, s := range []Section{SectionTop, SectionArgs, SectionWorkspace, SectionMounts, SectionEnv, SectionSandbox,
		SectionSecrets, SectionBindings, SectionRegistries, SectionMCP, SectionPorts, SectionLifecycle, SectionKits, SectionCapabilities} {
		order[s] = i
	}
	sort.SliceStable(items, func(i, j int) bool {
		if order[items[i].Section] != order[items[j].Section] {
			return order[items[i].Section] < order[items[j].Section]
		}
		return false
	})
}
