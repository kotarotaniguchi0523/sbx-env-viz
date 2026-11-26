package sbxenv

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

// capShape is a well-known capability type's arity (SPEC-v3 §7.2).
type capShape int

const (
	shapeInstance capShape = iota
	shapeSingleton
)

// knownCaps lists the com.docker.sandbox types of SPEC-v3 §7.2.
var knownCaps = map[string]capShape{
	"network-policy@1": shapeSingleton, "network-policy@2": shapeSingleton,
	"docker-auth@1": shapeInstance, "credential@1": shapeInstance, "ssh-agent@1": shapeInstance,
	"volume@1": shapeInstance, "host-mount@1": shapeInstance, "port@1": shapeInstance,
	"host-port@1": shapeInstance, "usb-device@1": shapeInstance, "resources@1": shapeSingleton,
	"privileged@1": shapeSingleton, "dockerd@1": shapeSingleton, "open-url@1": shapeSingleton,
	"long-running@1": shapeSingleton, "auto-restart@1": shapeSingleton, "lifecycle@1": shapeSingleton,
	"agent-context@1": shapeInstance, "agent-context@2": shapeInstance, "acp@1": shapeInstance,
	"agent-sessions@1": shapeSingleton, "agent-interactive-sessions@1": shapeSingleton,
	"agent-skills@1": shapeInstance, "agent-skill@1": shapeInstance, "git-identity@1": shapeSingleton,
	"kit-registry@1": shapeSingleton, "sbx@1": shapeSingleton,
}

var (
	capTypeRe   = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)*/[a-z0-9]+(-[a-z0-9]+)*@[0-9]+$`)
	serviceRe   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	sshServerRe = regexp.MustCompile(`^([A-Za-z0-9._-]+@)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
)

// known reports whether the capability is a well-known type.
func (c *capState) known() bool {
	if !strings.HasPrefix(c.typ, capNamespace) {
		return false
	}
	_, ok := knownCaps[c.short]
	return ok
}

// checkCapGrammar validates type names, singletons, and @1/@2 exclusivity
// per declaration block (top-level entries, or one group's members).
//
//declscope:shared // analyze.go applies the capability rules to each expanded kit
func (rs *resolver) checkCapGrammar(k *kitState) {
	type blockKey struct {
		block int
		short string
	}
	seen := map[blockKey]*capState{}
	for _, c := range k.caps {
		if !capTypeRe.MatchString(c.typ) {
			rs.diag("KIT_CAP_GRAMMAR", LevelError, "kits", fmt.Sprintf("kit %s: capability type %q is not <namespace>/<name>@<version>", k.handleShown, c.typ), c.itemID, c.origin)
		}
		if !c.known() {
			if !c.optional {
				rs.diag("UNKNOWN_CAPABILITY_REQUIRED", LevelError, "kits", fmt.Sprintf("kit %s requires capability %s, which is not a well-known type; a host that does not recognise it refuses the kit", k.handleShown, c.typ), c.itemID, c.origin)
			}
			continue
		}
		if knownCaps[c.short] != shapeSingleton {
			continue
		}
		key := c.short
		if strings.HasPrefix(key, "network-policy@") {
			key = "network-policy"
		}
		if prev, ok := seen[blockKey{c.block, key}]; ok {
			rs.diag("KIT_SINGLETON", LevelError, "kits", fmt.Sprintf("kit %s declares %s and %s in one declaration block; at most one is allowed", k.handleShown, prev.short, c.short), c.itemID, prev.origin, c.origin)
			continue
		}
		seen[blockKey{c.block, key}] = c
	}
}

// markSkippedGroups applies optional-group selection: a group is taken
// whole or not at all, so one member the host cannot satisfy (an unbound
// credential) skips every member. A standalone optional credential with no
// binding is skipped on its own.
//
//declscope:shared // kits.go marks skipped groups once a kit's capabilities are read
func (rs *resolver) markSkippedGroups(k *kitState) {
	bindings := rs.root.get("bindings")
	unbound := func(c *capState) string {
		if c.short != "credential@1" {
			return ""
		}
		b := bindings.get(c.config.str("service"))
		for _, mech := range []string{"apiKey", "oauth"} {
			if c.config.get(mech) != nil && b.get(mech) == nil {
				return fmt.Sprintf("認証情報 %s の使用が承認されていない（bindings.%s.%s がない）ため、この任意の要求は省略されます", rs.r.text(c.config.dstr("service")), rs.r.text(c.config.dstr("service")), mech)
			}
		}
		return ""
	}
	groups := map[int][]*capState{}
	for _, c := range k.caps {
		if c.block > 0 {
			groups[c.block] = append(groups[c.block], c)
		} else if c.optional {
			if why := unbound(c); why != "" {
				c.skipped, c.skipReason = true, why
			}
		}
	}
	for _, members := range groups {
		if !members[0].optional {
			continue
		}
		for _, m := range members {
			if why := unbound(m); why != "" {
				for _, x := range members {
					x.skipped, x.skipReason = true, "同じグループの要求を満たせないため、グループごと省略されます: "+why
				}
				break
			}
		}
	}
}

// checkHostMounts validates host-mount@1 paths and storage-key conflicts.
//
//declscope:shared // analyze.go applies the capability rules
func (rs *resolver) checkHostMountRules() {
	type claim struct {
		c    *capState
		kind string
	}
	byPath := map[string][]claim{}
	var order []string
	for _, k := range rs.kits {
		for _, c := range k.caps {
			if c.skipped || (c.short != "host-mount@1" && c.short != "volume@1") {
				continue
			}
			p := c.config.str("path")
			if c.short == "host-mount@1" && !hasExpr(p) && (p == "/" || !strings.HasPrefix(p, "/") || path.Clean(p) != p) {
				rs.diag("HOST_MOUNT_PATH_INVALID", LevelError, "boundary", fmt.Sprintf("kit %s host-mount@1 path %s must be absolute and canonical (no ., .., doubled or trailing slash, not /)", k.handleShown, rs.r.text(c.config.dstr("path"))), c.itemID, c.origin)
			}
			clean := path.Clean(p)
			if _, ok := byPath[clean]; !ok {
				order = append(order, clean)
			}
			byPath[clean] = append(byPath[clean], claim{c, c.short})
		}
	}
	for _, p := range order {
		cl := byPath[p]
		hostMounts, volumes := 0, 0
		for _, x := range cl {
			if x.kind == "host-mount@1" {
				hostMounts++
			} else {
				volumes++
			}
		}
		if hostMounts == 0 || len(cl) < 2 {
			continue
		}
		var origins []Origin
		var who []string
		for _, x := range cl {
			origins = append(origins, x.c.origin)
			who = append(who, x.c.kit.handleShown+" ("+x.kind+")")
		}
		rs.diag("HOST_MOUNT_CONFLICT", LevelError, "boundary",
			fmt.Sprintf("path %s is claimed by %s; shared storage has one declaring kit and sbx does not merge them", rs.r.text(p), strings.Join(who, ", ")), cl[0].c.itemID, origins...)
	}
}

// checkSSHAgent validates ssh-agent@1 config (phase, bounded/unbounded
// lists, server names) and overlapping phases within a block.
//
//declscope:shared // analyze.go applies the capability rules to each expanded kit
func (rs *resolver) checkSSHAgent(k *kitState) {
	type blockPhase struct {
		block int
		phase string
	}
	seen := map[blockPhase]bool{}
	for _, c := range k.caps {
		if c.short != "ssh-agent@1" {
			continue
		}
		cfg := c.config
		var problems []string
		ph := phases(cfg.get("phase"))
		if len(ph) == 0 {
			problems = append(problems, "phase is required")
		}
		for _, p := range ph {
			if p != "install" && p != "runtime" {
				problems = append(problems, fmt.Sprintf("phase %q is not install or runtime", p))
			}
			if seen[blockPhase{c.block, p}] {
				problems = append(problems, fmt.Sprintf("phase %s is already granted by another ssh-agent@1 entry", p))
			}
			seen[blockPhase{c.block, p}] = true
		}
		unrestricted := cfg.str("unrestricted") != "false"
		sign, auth := cfg.get("sign"), cfg.get("authenticate")
		if unrestricted && (sign != nil || auth != nil) {
			problems = append(problems, "an unrestricted entry cannot state sign or authenticate")
		}
		if !unrestricted && len(sign.list()) == 0 && len(auth.list()) == 0 {
			problems = append(problems, "unrestricted: false needs sign or authenticate")
		}
		for _, l := range []struct {
			name string
			v    *vnode
		}{{"sign", sign}, {"authenticate", auth}} {
			if l.v != nil && len(l.v.list()) == 0 {
				problems = append(problems, l.name+" must not be empty")
			}
			dup := map[string]bool{}
			for _, s := range l.v.scalars() {
				if dup[s] {
					problems = append(problems, l.name+" lists a value twice")
				}
				dup[s] = true
				if l.name == "authenticate" && !sshServerRe.MatchString(s) {
					problems = append(problems, "authenticate entries are [user@]host with a literal lowercase host, no wildcard or port")
				}
				if l.name == "sign" && strings.ContainsAny(s, " \t") {
					problems = append(problems, "sign namespaces contain no spaces")
				}
			}
		}
		for _, pr := range dedupe(problems) {
			rs.diag("SSH_AGENT_INVALID", LevelError, "kits", fmt.Sprintf("kit %s ssh-agent@1: %s", k.handleShown, pr), c.itemID, c.origin)
		}
	}
}

// credentialProblems validates credential@1 service and phase.
//
//declscope:shared // analyze.go validates credential@1 configs before pairing them with network rules
func credentialProblems(cfg *vnode) []string {
	var out []string
	svc := cfg.str("service")
	if svc == "" {
		out = append(out, "service is required")
	} else if !serviceRe.MatchString(svc) {
		out = append(out, "service must be lowercase-kebab")
	}
	ph := cfg.get("phase")
	list := phases(ph)
	if ph == nil || len(list) == 0 {
		out = append(out, "phase is required and must not be empty")
	}
	dup := map[string]bool{}
	for _, p := range list {
		if p != "install" && p != "runtime" {
			out = append(out, fmt.Sprintf("phase %q is not install or runtime", p))
		}
		if dup[p] {
			out = append(out, "phase lists "+p+" twice")
		}
		dup[p] = true
	}
	return out
}

// phases reads a capability config's phase field, a scalar or a list.
//
//declscope:shared // analyze.go and build.go read credential and network phases with it
func phases(v *vnode) []string {
	if v == nil {
		return nil
	}
	if v.kind == parser.Scalar {
		return []string{v.val}
	}
	return v.scalars()
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
