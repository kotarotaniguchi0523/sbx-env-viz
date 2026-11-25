package sbxenv

import (
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
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
	order   int
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

// networkPolicy is compiled after all kits have selected their active groups.
type networkPolicy struct {
	entries      []*netEntry
	byCapability map[*capState][]*netEntry
	denies       map[string]map[string]*netEntry // phase, normalized pattern -> first declaration
	allows       map[kitPhase]map[string]bool
}

type kitPhase struct {
	kit   *kitState
	phase string
}

func compileNetworkPolicy(kits []*kitState) networkPolicy {
	p := networkPolicy{
		byCapability: make(map[*capState][]*netEntry),
		denies:       make(map[string]map[string]*netEntry),
		allows:       make(map[kitPhase]map[string]bool),
	}
	for _, k := range kits {
		for _, c := range k.caps {
			if c.short != "network-policy@1" && c.short != "network-policy@2" {
				continue
			}
			entries := parseNetPolicy(c)
			p.byCapability[c] = entries
			for _, e := range entries {
				e.order = len(p.entries)
				p.entries = append(p.entries, e)
				host := strings.ToLower(stripPort(e.host.val))
				if e.list == "deny" {
					if c.skipped || e.bounded() {
						continue
					}
					if p.denies[e.phase] == nil {
						p.denies[e.phase] = make(map[string]*netEntry)
					}
					if p.denies[e.phase][host] == nil {
						p.denies[e.phase][host] = e
					}
				} else {
					key := kitPhase{k, e.phase}
					if p.allows[key] == nil {
						p.allows[key] = make(map[string]bool)
					}
					p.allows[key][host] = true
				}
			}
		}
	}
	return p
}

// deniedBy returns the first unbounded deny covering the entire requested
// pattern. Enumerate DNS suffixes, preserving declaration order across matches.
func (p *networkPolicy) deniedBy(phase, host string) *netEntry {
	index := p.denies[phase]
	if len(index) == 0 {
		return nil
	}
	h := strings.ToLower(stripPort(host))
	var first *netEntry
	consider := func(pattern string) {
		if e := index[pattern]; e != nil && (first == nil || e.order < first.order) {
			first = e
		}
	}
	consider("*")
	consider("**")
	consider(h)
	if h == "*" || h == "**" {
		return first
	}
	suffix := h
	for {
		_, rest, ok := strings.Cut(suffix, ".")
		if !ok {
			break
		}
		if suffix == h && !strings.HasPrefix(h, "**.") && !strings.HasPrefix(h, ".") {
			consider("*." + rest)
		}
		consider("**." + rest)
		suffix = rest
	}
	return first
}

func (p *networkPolicy) kitAllows(k *kitState, phase, domain string) bool {
	hosts := p.allows[kitPhase{k, phase}]
	return hosts["*"] || hosts["**"] || hosts[strings.ToLower(stripPort(domain))]
}
