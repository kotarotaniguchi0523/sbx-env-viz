package sbxenv

import (
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

// netEntry is one allow or deny entry of network-policy@1 or @2, with @1's
// plain host strings read as unbounded entries.
type netEntry struct {
	cap   *capState
	phase string
	list  string // "allow" or "deny"
	// host is the pattern as written (one entry per host of an @2 object).
	host *vnode
	// methods and paths bound an @2 entry; both empty means unbounded.
	methods []string
	paths   []string
	itemID  string
}

func (e *netEntry) bounded() bool { return len(e.methods) > 0 || len(e.paths) > 0 }

// parseNetPolicy flattens a network-policy capability into entries.
func parseNetPolicy(c *capState) []*netEntry {
	var out []*netEntry
	for _, ph := range []string{"install", "runtime"} {
		for _, list := range []string{"allow", "deny"} {
			for _, e := range c.config.get(ph).get(list).list() {
				switch e.kind {
				case parser.Scalar:
					out = append(out, &netEntry{cap: c, phase: ph, list: list, host: e})
				case parser.Map:
					for _, h := range e.get("hosts").list() {
						out = append(out, &netEntry{cap: c, phase: ph, list: list, host: h,
							methods: e.get("methods").scalars(), paths: e.get("paths").scalars()})
					}
				}
			}
		}
	}
	return out
}

// stripPort drops a :port suffix; ports are ignored for allow-list membership.
func stripPort(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}

// hostMatches reports whether pattern covers host: exact match, "*" or
// "**" for everything, "*.x" for exactly one more label, "**.x" for any
// depth. The precise matcher is runtime-owned; this is the portable core.
func hostMatches(pattern, host string) bool {
	p, h := strings.ToLower(stripPort(pattern)), strings.ToLower(stripPort(host))
	switch {
	case p == "*" || p == "**":
		return true
	case strings.HasPrefix(p, "**."):
		return strings.HasSuffix(h, p[2:])
	case strings.HasPrefix(p, "*."):
		rest, ok := strings.CutSuffix(h, p[1:])
		return ok && rest != "" && !strings.Contains(rest, ".")
	}
	return p == h
}

// deniedBy returns the unbounded deny entry, from any kit, that refuses
// host in phase. Deny wins and one kit's allow cannot defeat another's deny.
func (rs *resolver) deniedBy(phase, host string) *netEntry {
	for _, d := range rs.net {
		if d.list == "deny" && d.phase == phase && !d.bounded() && !d.cap.skipped && hostMatches(d.host.val, host) {
			return d
		}
	}
	return nil
}

// kitAllows reports whether the kit's own policy lists domain for phase,
// exactly (a wildcard does not cover a specific subdomain) or via * / **.
func (rs *resolver) kitAllows(k *kitState, phase, domain string) bool {
	for _, a := range rs.net {
		if a.cap.kit != k || a.list != "allow" || a.phase != phase {
			continue
		}
		p := stripPort(a.host.val)
		if p == "*" || p == "**" || strings.EqualFold(p, stripPort(domain)) {
			return true
		}
	}
	return false
}
