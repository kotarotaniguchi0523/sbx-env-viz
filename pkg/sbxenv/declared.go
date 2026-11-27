package sbxenv

import (
	"strconv"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

// Declared is the subset of the merged declaration that a running sandbox
// can be compared with. Values are resolved (expressions expanded, relative
// paths made absolute) and NOT redacted, so Declared never leaves the
// process: it is excluded from JSON and only used for matching. Anything a
// person reads comes from the redacted Items, found by ItemID.
type Declared struct {
	Name       string
	Agent      string
	Workspaces []DeclaredMount
	Ports      []DeclaredPort
	Secrets    []DeclaredName
	MCP        []DeclaredMCP
	Network    []DeclaredRule
}

// DeclaredMount is the primary workspace or one additional workspace.
type DeclaredMount struct {
	ItemID   string
	Path     string
	Target   string
	ReadOnly bool
	Clone    bool
	Primary  bool
}

// DeclaredPort is one published port. HostPort 0 means an ephemeral port
// and an empty HostIP the loopback default.
type DeclaredPort struct {
	ItemID      string
	HostIP      string
	HostPort    int
	SandboxPort int
	// Redacted marks ports whose resolved fields are hidden in display output.
	Redacted bool
	Protocol string
}

// DeclaredName is a named entry such as a secret service.
type DeclaredName struct {
	ItemID string
	Name   string
}

// DeclaredMCP is one MCP server; Remote is true for url servers. Shown is
// the name as the Items show it (redacted like them), for text a person
// reads; Name is only for matching.
type DeclaredMCP struct {
	ItemID string
	Name   string
	Shown  string
	Remote bool
	// Skipped marks a command server --skip-host-commands keeps from
	// running, so its registration says nothing about the declaration.
	Skipped bool
}

// DeclaredRule is one network-policy entry of an expanded, selected kit.
// Bounded marks an @2 entry limited to some methods or paths.
type DeclaredRule struct {
	ItemID   string
	Phase    string
	Decision string
	Host     string
	Bounded  bool
}

//declscope:shared // finish in analyze.go attaches it to the Plan
func (rs *resolver) declared() *Declared {
	d := &Declared{Name: rs.root.str("name"), Agent: rs.root.str("agent")}
	// addItem numbers a repeated ID (#2, #3, ...); entries here take the
	// same numbers, in the same order, so each finds its own item.
	seen := map[string]int{}
	itemID := func(id string) string {
		seen[id]++
		if n := seen[id]; n > 1 {
			return id + "#" + strconv.Itoa(n)
		}
		return id
	}
	if rs.opts.Name != "" {
		d.Name = rs.opts.Name
	}
	if ws := rs.root.get("workspace"); ws != nil && ws.kind != parser.Null {
		p, clone := ws, false
		if ws.kind == parser.Map {
			p, clone = ws.get("path"), ws.str("clone") == "true"
		}
		if rs.opts.Clone != nil {
			clone = *rs.opts.Clone
		}
		if p != nil && p.kind == parser.Scalar {
			d.Workspaces = append(d.Workspaces, DeclaredMount{ItemID: "workspace", Path: resolveHostPath(p.fileDir, p.val), Clone: clone, Primary: true})
		}
	}
	for _, e := range rs.root.get("additionalWorkspaces").list() {
		p := workspacePath(e)
		target := rs.r.path(e.dstr("target"))
		if target == "" && p != nil {
			target = rs.fmtPath(p)
		}
		id := itemID("additionalWorkspaces[" + target + "]")
		if p == nil || p.kind != parser.Scalar {
			continue
		}
		d.Workspaces = append(d.Workspaces, DeclaredMount{ItemID: id,
			Path: resolveHostPath(p.fileDir, p.val), Target: e.str("target"), ReadOnly: e.str("readOnly") == "true"})
	}
	for _, e := range rs.root.get("ports").list() {
		redacted := false
		for _, key := range []string{"host", "hostIP", "sandbox", "protocol"} {
			if e.dstr(key) != e.str(key) {
				redacted = true
				break
			}
		}
		host, _ := strconv.Atoi(e.str("host"))
		sb, _ := strconv.Atoi(e.str("sandbox"))
		d.Ports = append(d.Ports, DeclaredPort{Redacted: redacted, ItemID: itemID("ports[" + rs.portKey(e) + "]"), HostIP: e.str("hostIP"),
			HostPort: host, SandboxPort: sb, Protocol: effectiveProtocol(e)})
	}
	for _, k := range rs.root.get("secrets").mapKeys() {
		d.Secrets = append(d.Secrets, DeclaredName{ItemID: "secrets." + k, Name: k})
	}
	for _, e := range rs.root.get("mcp").get("servers").list() {
		shown := rs.r.text(e.dstr("name"))
		id := itemID("mcp.servers[" + shown + "]")
		name := e.str("name")
		if name == "" {
			continue
		}
		d.MCP = append(d.MCP, DeclaredMCP{ItemID: id, Name: name, Shown: shown, Remote: e.has("url"),
			Skipped: e.has("command") && rs.opts.SkipHostCommands})
	}
	for _, e := range rs.net.entries {
		if e.cap.skipped || e.host == nil || e.itemID == "" {
			continue
		}
		d.Network = append(d.Network, DeclaredRule{ItemID: e.itemID, Phase: e.phase, Decision: e.list, Host: strings.ToLower(e.host.val), Bounded: e.bounded()})
	}
	return d
}
