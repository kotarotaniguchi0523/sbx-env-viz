package sbxenv

import (
	"strings"
	"unicode"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

// netEntry is one allow or deny entry of network-policy@1 or @2, with @1's
// plain host strings read as unbounded entries.
//
//declscope:shared // build.go and analyze.go read the compiled entries
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
	// order is the entry's position in the policy; the first deny wins.
	//
	//declscope:private // only deniedBy compares positions
	order int
}

//declscope:shared // build.go and analyze.go tell bounded entries apart
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
	h, _ = splitPort(h)
	return h
}

// splitPort splits "host:port" into host and port; port is "" when none is
// stated, which in a network-policy pattern means any port.
func splitPort(h string) (host, port string) {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i], h[i+1:]
	}
	return h, ""
}

// foldHost maps h to a key that is equal for two hosts exactly when
// strings.EqualFold reports them equal: each rune becomes the smallest rune
// of its simple case-folding orbit. Unlike strings.ToLower, this keeps "ς"
// and "σ" the same, as EqualFold does.
func foldHost(h string) string {
	return strings.Map(func(r rune) rune {
		m := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			m = min(m, f)
		}
		return m
	}, h)
}

// networkPolicy is compiled after all kits have selected their active groups.
// entries and byCapability keep every entry for display; the allow and deny
// indexes hold only entries of capabilities the host selects.
//
//declscope:shared // the resolver holds the compiled policy; build.go, analyze.go and declared.go read it
type networkPolicy struct {
	entries      []*netEntry
	byCapability map[*capState][]*netEntry
	// denies and allows are the lookup tables behind deniedBy and kitAllows.
	//
	//declscope:private // other files ask through deniedBy and kitAllows
	denies map[denyKey]*netEntry // first unbounded deny per folded pattern and port
	//declscope:private // other files ask through deniedBy and kitAllows
	allows map[kitPhaseHost]bool // folded pattern without its port
}

type phaseHost struct{ phase, host string }

type denyKey struct {
	phaseHost
	port string // "" covers every port
}

type kitPhaseHost struct {
	kit *kitState
	phaseHost
}

//declscope:shared // Resolve compiles the policy once kits are expanded
func compileNetworkPolicy(kits []*kitState) networkPolicy {
	p := networkPolicy{
		byCapability: map[*capState][]*netEntry{},
		denies:       map[denyKey]*netEntry{},
		allows:       map[kitPhaseHost]bool{},
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
				if c.skipped {
					continue
				}
				host, port := splitPort(foldHost(e.host.val))
				switch {
				case e.list == "allow":
					p.allows[kitPhaseHost{k, phaseHost{e.phase, host}}] = true
				case !e.bounded():
					key := denyKey{phaseHost{e.phase, host}, port}
					if p.denies[key] == nil {
						p.denies[key] = e
					}
				}
			}
		}
	}
	return p
}

// deniedBy returns the earliest-declared unbounded deny, from any kit, whose
// pattern covers all of host in phase. Deny wins over every kit's allow.
// Patterns: "*" or "**" cover everything, "*.x" exactly one more label,
// "**.x" any depth; host may itself be such a pattern. A deny without a port
// covers every port; one with a port covers only a host stating that port, so
// a host without a port (any port) is covered only by a portless deny.
//
//declscope:shared // build.go and analyze.go look up deny rules
func (p *networkPolicy) deniedBy(phase, host string) *netEntry {
	h, port := splitPort(foldHost(host))
	var first *netEntry
	consider := func(pattern string) {
		for _, pt := range []string{"", port} {
			if e := p.denies[denyKey{phaseHost{phase, pattern}, pt}]; e != nil && (first == nil || e.order < first.order) {
				first = e
			}
			if port == "" {
				break
			}
		}
	}
	consider("*")
	consider("**")
	consider(h)
	if label, rest, ok := strings.Cut(h, "."); ok && label != "" && label != "**" {
		consider("*." + rest)
	}
	for s := h; ; {
		_, rest, ok := strings.Cut(s, ".")
		if !ok {
			return first
		}
		consider("**." + rest)
		s = rest
	}
}

// kitAllows reports whether the kit's own policy lists domain for phase,
// exactly (a wildcard does not cover a specific subdomain) or via * / **.
//
//declscope:shared // analyze.go checks credential domains against it
func (p *networkPolicy) kitAllows(k *kitState, phase, domain string) bool {
	allowed := func(h string) bool { return p.allows[kitPhaseHost{k, phaseHost{phase, h}}] }
	return allowed("*") || allowed("**") || allowed(foldHost(stripPort(domain)))
}
