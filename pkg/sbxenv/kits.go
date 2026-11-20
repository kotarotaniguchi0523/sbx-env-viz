package sbxenv

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

// Kit source kinds.
const (
	kitLocal = "local"
	kitZip   = "zip"
	kitGit   = "git"
	kitOCI   = "oci"
)

const capNamespace = "com.docker.sandbox/"

type kitState struct {
	entry    *vnode
	source   string
	kind     string
	dir      string
	handle   string
	srcID    string
	expanded bool
	descKind string
	caps     []*capState
	argDecls map[string]*argDecl
	args     map[string]*ArgValue
	origin   Origin
	// raw is the parsed descriptor between loading and expansion.
	raw *parser.Node
}

type capState struct {
	kit         *kitState
	typ         string
	short       string
	optional    bool
	name        string
	description string
	config      *vnode
	origin      Origin
	index       string
}

// via returns the provenance chain for a kit-derived origin.
func (k *kitState) via() []string {
	return []string{fmt.Sprintf("%s:%d kits[%s]", k.origin.SourceID, k.origin.Line, k.source)}
}

func classifyKitSource(src string) string {
	switch {
	case strings.HasPrefix(src, "git+https://"), strings.HasPrefix(src, "git+ssh://"):
		return kitGit
	case strings.HasSuffix(strings.ToLower(src), ".zip") && !strings.Contains(src, "://"):
		return kitZip
	case src == ".", src == "..", strings.HasPrefix(src, "./"), strings.HasPrefix(src, "../"),
		strings.HasPrefix(src, ".\\"), strings.HasPrefix(src, "..\\"), filepath.IsAbs(src):
		return kitLocal
	}
	return kitOCI
}

// kitHandle derives the handle sbx uses for agent: matching.
func kitHandle(kind, src, dir string) string {
	switch kind {
	case kitLocal:
		return filepath.Base(dir)
	case kitZip:
		return strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	case kitGit:
		if i := strings.Index(src, "dir="); i >= 0 {
			d := src[i+4:]
			if j := strings.IndexAny(d, "&#"); j >= 0 {
				d = d[:j]
			}
			return path.Base(d)
		}
		u := src
		if j := strings.IndexAny(u, "#?"); j >= 0 {
			u = u[:j]
		}
		return strings.TrimSuffix(path.Base(u), ".git")
	}
	ref := src
	if i := strings.Index(ref, "@sha256:"); i >= 0 {
		ref = ref[:i]
	}
	last := path.Base(ref)
	if i := strings.LastIndex(last, ":"); i >= 0 {
		last = last[:i]
	}
	return last
}

func (rs *resolver) expandKits() {
	kits := rs.root.get("kits")
	for _, e := range kits.list() {
		src := kitSource(e)
		if src == "" {
			rs.diag("KIT_SOURCE_MISSING", LevelError, "kits", "a kits entry has no source", "", e.origin())
			continue
		}
		k := &kitState{entry: e, source: src, kind: classifyKitSource(src), origin: e.origin(), args: map[string]*ArgValue{}}
		srcNode := e
		if e.kind == parser.Map {
			srcNode = e.get("source")
		}
		if k.kind == kitLocal {
			k.dir = resolveHostPath(srcNode.fileDir, src)
		}
		k.handle = kitHandle(k.kind, src, k.dir)
		rs.kits = append(rs.kits, k)
		switch k.kind {
		case kitLocal:
			rs.loadLocalKit(k)
		case kitZip:
			rs.coverage(CoverageRemoteKit, src, "ZIP kit archives are not unpacked by this tool; its capabilities (network, credentials, host mounts, hooks) are not shown", k.origin)
		case kitGit:
			reason := "Git kits are fetched and built by sbx at create time; their capabilities are not shown"
			if !strings.Contains(src, "ref=") {
				reason += ". The reference has no ref= pin, so its content can change between creates"
			}
			rs.coverage(CoverageRemoteKit, src, reason, k.origin)
		case kitOCI:
			reason := "OCI kits are pulled by sbx at create time; their capabilities (network, credentials, host mounts, hooks) are not shown"
			if !strings.Contains(src, "@sha256:") {
				reason += ". The tag is not digest-pinned, so a republished tag changes what is installed"
			}
			rs.coverage(CoverageRemoteKit, src, reason, k.origin)
		}
	}
	rs.applyKitArgs()
	for _, k := range rs.kits {
		if k.expanded {
			rs.expandKitDescriptor(k)
		}
	}
	rs.checkAgent()
}

func (rs *resolver) loadLocalKit(k *kitState) {
	if rs.opts.Kits == nil {
		rs.coverage(CoverageNoKitResolver, k.source, "no kit loader was configured, so this local kit is not expanded", k.origin)
		return
	}
	in, err := rs.opts.Kits.LoadKit(k.dir)
	if err != nil {
		rs.coverage(CoverageKitLoadFailed, k.source, "the kit directory could not be read: "+rs.r.text(err.Error()), k.origin)
		rs.diag("KIT_UNREADABLE", LevelError, "kits", fmt.Sprintf("local kit %s could not be read; sbx would fail to build it", k.source), "kits["+rs.r.path(k.source)+"]", k.origin)
		return
	}
	k.srcID = rs.addSource(Source{Kind: SourceKit, Path: rs.r.path(in.DescriptorPath), SHA256: digest(in.Content), Expanded: true})
	res, err := parser.Parse(in.Content)
	if err != nil {
		rs.markSource(k.srcID, false, "parse error")
		rs.diag("KIT_PARSE", LevelError, "kits", fmt.Sprintf("kit descriptor for %s: %s", k.source, err.Error()), "", Origin{SourceID: k.srcID})
		return
	}
	for _, d := range res.Duplicates {
		rs.diag("DUPLICATE_KEY", LevelError, "parse",
			fmt.Sprintf("key %q is defined twice in the same mapping (first at line %d, again at line %d); kit decoding rejects duplicate keys", d.Path, d.First.Line, d.Again.Line),
			"", Origin{SourceID: k.srcID, Line: d.First.Line, Col: d.First.Col, Via: k.via()}, Origin{SourceID: k.srcID, Line: d.Again.Line, Col: d.Again.Col, Via: k.via()})
	}
	root := res.Root
	if root.Kind != parser.Map {
		rs.markSource(k.srcID, false, "not a mapping")
		return
	}
	if sv := root.Get("schemaVersion"); sv == nil || sv.Value != "3" {
		v := ""
		if sv != nil {
			v = sv.Value
		}
		rs.markSource(k.srcID, false, "legacy or unknown kit schema")
		rs.coverage(CoverageLegacyKit, k.source, fmt.Sprintf("kit descriptor schemaVersion %q is not the v3 grammar this tool reads; it is not expanded", v), Origin{SourceID: k.srcID})
		return
	}
	var issues []schemaIssue
	checkShape(root, kitSchema, "", &issues)
	for _, is := range issues {
		code := "KIT_TYPE_MISMATCH"
		if is.unknown {
			code = "KIT_UNKNOWN_FIELD"
		}
		rs.diag(code, LevelError, "kits", is.message(), "", Origin{SourceID: k.srcID, Line: is.pos.Line, Col: is.pos.Col, Via: k.via()})
	}
	if kind := root.Get("kind"); kind != nil {
		k.descKind = kind.Value
	}
	if k.descKind == "set" || root.Get("kits") != nil {
		rs.coverage(CoverageRemoteKit, k.source, "kit sets are composed from other kits at build time; members are not expanded", Origin{SourceID: k.srcID})
	}
	k.argDecls = map[string]*argDecl{}
	if args := root.Get("args"); args != nil {
		for _, kv := range args.Keys {
			d := &argDecl{name: kv.Key, origin: Origin{SourceID: k.srcID, Line: kv.KeyPos.Line, Via: k.via()}}
			if v := kv.Value; v != nil && v.Kind == parser.Map {
				if x := v.Get("default"); x != nil {
					s := x.Value
					d.def = &s
					d.defOrigin = Origin{SourceID: k.srcID, Line: x.Pos.Line, Via: k.via()}
				}
				d.required = v.Get("required") != nil && v.Get("required").Value == "true"
				if x := v.Get("enum"); x != nil {
					for _, it := range x.Items {
						d.enum = append(d.enum, it.Value)
					}
				}
				if x := v.Get("pattern"); x != nil {
					d.pattern = x.Value
				}
			}
			k.argDecls[kv.Key] = d
		}
	}
	k.expanded = true
	k.raw = root
}

func (rs *resolver) markSource(id string, expanded bool, status string) {
	for i := range rs.sources {
		if rs.sources[i].ID == id {
			rs.sources[i].Expanded = expanded
			rs.sources[i].Status = status
		}
	}
}

func (rs *resolver) applyKitArgs() {
	type supplied struct {
		handle, name, value string
		origin              Origin
		from                string
	}
	var sup []supplied
	parse := func(s string, o Origin, from string) (supplied, bool) {
		lhs, value, ok := strings.Cut(s, "=")
		if !ok || lhs == "" {
			return supplied{}, false
		}
		handle, name := "", lhs
		if i := strings.LastIndex(lhs, "."); i >= 0 {
			handle, name = lhs[:i], lhs[i+1:]
		}
		return supplied{handle: handle, name: name, value: value, origin: o, from: from}, true
	}
	for _, f := range rs.opts.KitArgsFiles {
		id := rs.addSource(Source{Kind: SourceKitArgsFile, Path: rs.r.path(f.Path), SHA256: digest(f.Content), Expanded: true})
		lines, bad := parseArgFile(f.Content, id, "kit-args-file")
		for _, b := range bad {
			rs.diag("ARGS_FILE_SYNTAX", LevelError, "args", "each non-empty, non-comment line of a kit args file must be NAME=VALUE", "", b)
		}
		for _, l := range lines {
			if s, ok := parse(l.name+"="+l.value, l.origin, "kit-args-file"); ok {
				sup = append(sup, s)
			}
		}
	}
	for i, a := range rs.opts.KitArgs {
		o := rs.cliOrigin("--kit-arg", i)
		s, ok := parse(a, o, "flag")
		if !ok {
			rs.diag("KIT_ARG_SYNTAX", LevelError, "args", fmt.Sprintf("--kit-arg #%d must have the form NAME=VALUE or KIT.NAME=VALUE", i+1), "", o)
			continue
		}
		sup = append(sup, s)
	}
	remote := false
	for _, k := range rs.kits {
		if !k.expanded {
			remote = true
		}
	}
	for _, k := range rs.kits {
		// Defaults, then the env file entry, then files and flags.
		if k.expanded {
			for name, d := range k.argDecls {
				if d.def != nil {
					k.args[name] = &ArgValue{Kit: k.handle, Name: name, Value: *d.def, From: "default", Origin: d.defOrigin}
				}
			}
		}
		if args := k.entry.get("args"); args != nil {
			for _, key := range args.keys {
				v := args.kv[key]
				if k.expanded {
					if _, ok := k.argDecls[key]; !ok {
						rs.diag("KIT_ARG_UNDECLARED", LevelError, "args", fmt.Sprintf("kit %s does not declare argument %q", k.handle, key), "kits["+rs.r.path(k.source)+"]", v.origin())
						continue
					}
				}
				k.args[key] = &ArgValue{Kit: k.handle, Name: key, Value: v.val, From: "kit-entry", Origin: v.origin()}
			}
		}
	}
	for _, s := range sup {
		matched := false
		for _, k := range rs.kits {
			if s.handle != "" && s.handle != k.handle {
				continue
			}
			if k.expanded {
				if _, ok := k.argDecls[s.name]; !ok {
					continue
				}
			} else if s.handle == "" {
				continue
			}
			matched = true
			k.args[s.name] = &ArgValue{Kit: k.handle, Name: s.name, Value: s.value, From: s.from, Origin: s.origin}
		}
		if !matched {
			level := LevelWarning
			msg := fmt.Sprintf("kit argument %q is not declared by any expanded kit, so it has no visible effect", s.name)
			if remote {
				level = LevelInfo
				msg += " (it may apply to a remote kit this tool does not expand)"
			}
			rs.diag("KIT_ARG_UNUSED", level, "args", msg, "", s.origin)
		}
	}
	for _, k := range rs.kits {
		if !k.expanded {
			for _, name := range sortedKeys(k.args) {
				av := *k.args[name]
				av.Value = rs.r.value(av.Value)
				rs.kitArgs = append(rs.kitArgs, av)
			}
			continue
		}
		for _, name := range sortedKeys(k.argDecls) {
			d := k.argDecls[name]
			v, ok := k.args[name]
			if !ok {
				if d.required {
					rs.diag("KIT_ARG_REQUIRED", LevelError, "args", fmt.Sprintf("kit %s requires argument %q", k.handle, name), "kits["+rs.r.path(k.source)+"]", d.origin)
				}
				continue
			}
			if len(d.enum) > 0 && !contains(d.enum, v.Value) {
				rs.diag("KIT_ARG_INVALID", LevelError, "args", fmt.Sprintf("kit %s argument %q is not one of its enum values", k.handle, name), "", v.Origin, d.origin)
			}
			if d.pattern != "" {
				if re, err := regexp.Compile("^(?:" + d.pattern + ")$"); err == nil && !re.MatchString(v.Value) {
					rs.diag("KIT_ARG_INVALID", LevelError, "args", fmt.Sprintf("kit %s argument %q does not match its pattern", k.handle, name), "", v.Origin, d.origin)
				}
			}
			av := *v
			av.Value = rs.r.value(v.Value)
			rs.kitArgs = append(rs.kitArgs, av)
		}
	}
}

// kitVnode converts a kit descriptor subtree, expanding kit expressions.
func (rs *resolver) kitVnode(n *parser.Node, k *kitState, p string) *vnode {
	v := &vnode{kind: n.Kind, pos: n.Pos, src: k.srcID, fileDir: k.dir, quoted: n.Quoted}
	switch n.Kind {
	case parser.Scalar:
		v.raw, v.val = n.Value, n.Value
		if hasExpr(n.Value) {
			e := expandString(n.Value, func(body string) exprResult {
				if name, ok := kitArgRef(body); ok {
					if av, ok := k.args[name]; ok {
						return exprResult{value: av.Value, ok: true}
					}
					if _, declared := k.argDecls[name]; declared {
						return exprResult{}
					}
					return exprResult{}
				}
				if strings.HasPrefix(body, "kit.env.") {
					return exprResult{keep: true}
				}
				return exprResult{}
			})
			v.val = e.out
			for _, kept := range e.kept {
				rs.coverage(CoverageKitEnvRef, k.handle+": ${{ "+rs.r.expr(kept)+" }}", "final container environment references are resolved by sbx at create time; the value is shown unexpanded", Origin{SourceID: k.srcID, Line: n.Pos.Line, Via: k.via()})
			}
			for _, u := range e.unresolved {
				rs.diag("KIT_UNRESOLVED_EXPR", LevelError, "expression", fmt.Sprintf("kit %s: expression ${{ %s }} at %s cannot be resolved (only declared kit.args and kit.env references expand)", k.handle, rs.r.expr(u), p), "", Origin{SourceID: k.srcID, Line: n.Pos.Line, Col: n.Pos.Col, Via: k.via()})
			}
		}
	case parser.Map:
		v.kv = map[string]*vnode{}
		for _, kv := range n.Keys {
			child := rs.kitVnode(kv.Value, k, joinKey(p, kv.Key))
			child.pos = kv.KeyPos
			if _, dup := v.kv[kv.Key]; !dup {
				v.keys = append(v.keys, kv.Key)
			}
			v.kv[kv.Key] = child
		}
	case parser.Seq:
		for i, it := range n.Items {
			v.items = append(v.items, rs.kitVnode(it, k, fmt.Sprintf("%s[%d]", p, i)))
		}
	}
	return v
}

func (rs *resolver) expandKitDescriptor(k *kitState) {
	root := k.raw
	k.raw = nil
	caps := root.Get("capabilities")
	if caps == nil {
		return
	}
	add := func(entry *parser.Node, idx string, groupOptional *bool) {
		if entry.Kind != parser.Map {
			return
		}
		c := &capState{kit: k, index: idx, origin: Origin{SourceID: k.srcID, Line: entry.Pos.Line, Col: entry.Pos.Col, Via: k.via()}}
		if t := entry.Get("type"); t != nil {
			c.typ = t.Value
		}
		c.short = strings.TrimPrefix(c.typ, capNamespace)
		if groupOptional != nil {
			c.optional = *groupOptional
			if entry.Get("optional") != nil {
				rs.diag("KIT_GROUP_MEMBER_OPTIONAL", LevelError, "kits", fmt.Sprintf("kit %s: group members cannot set optional; put it on the group", k.handle), "", c.origin)
			}
		} else if o := entry.Get("optional"); o != nil {
			c.optional = o.Value == "true"
		}
		if x := entry.Get("name"); x != nil {
			c.name = x.Value
		}
		if x := entry.Get("description"); x != nil {
			c.description = x.Value
		}
		if cfg := entry.Get("config"); cfg != nil {
			c.config = rs.kitVnode(cfg, k, idx+".config")
			if sch, ok := capConfigSchemas[c.short]; ok && strings.HasPrefix(c.typ, capNamespace) {
				var issues []schemaIssue
				checkShape(cfg, sch, idx+".config", &issues)
				for _, is := range issues {
					code := "KIT_TYPE_MISMATCH"
					if is.unknown {
						code = "KIT_UNKNOWN_FIELD"
					}
					rs.diag(code, LevelError, "kits", fmt.Sprintf("kit %s (%s): %s", k.handle, c.short, is.message()), "", Origin{SourceID: k.srcID, Line: is.pos.Line, Col: is.pos.Col, Via: k.via()})
				}
			}
		}
		if c.short == "git-identity@1" && entry.Get("config") != nil {
			rs.diag("KIT_CONFIG_NOT_ALLOWED", LevelError, "kits", fmt.Sprintf("kit %s: git-identity@1 must not carry config, not even an empty one", k.handle), "", c.origin)
		}
		if c.typ == "" {
			rs.diag("KIT_CAP_TYPE_MISSING", LevelError, "kits", fmt.Sprintf("kit %s: a capability entry has no type", k.handle), "", c.origin)
			return
		}
		k.caps = append(k.caps, c)
	}
	for i, entry := range caps.Items {
		idx := fmt.Sprintf("capabilities[%d]", i)
		if g := entry.Get("group"); g != nil {
			opt := g.Get("optional") != nil && g.Get("optional").Value == "true"
			members := g.Get("capabilities")
			if members == nil || len(members.Items) == 0 {
				rs.diag("KIT_GROUP_EMPTY", LevelError, "kits", fmt.Sprintf("kit %s: capability groups must not be empty", k.handle), "", Origin{SourceID: k.srcID, Line: g.Pos.Line, Via: k.via()})
				continue
			}
			for j, m := range members.Items {
				add(m, fmt.Sprintf("%s.group.capabilities[%d]", idx, j), &opt)
			}
			continue
		}
		add(entry, idx, nil)
	}
}

var builtinAgents = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "copilot": true, "opencode": true,
	"shell": true, "cursor": true, "kiro": true, "docker-agent": true, "goose": true, "amp": true,
}

func (rs *resolver) checkAgent() {
	agent := rs.root.get("agent")
	if agent == nil || agent.kind != parser.Scalar || agent.val == "" {
		rs.diag("AGENT_MISSING", LevelError, "schema", "agent is required in the merged environment (a built-in agent or the handle of a workload kit)", "agent")
		return
	}
	for _, k := range rs.kits {
		if k.handle == agent.val {
			if k.expanded && k.descKind != "" && k.descKind != "workload" && k.descKind != "set" {
				rs.diag("AGENT_NOT_WORKLOAD", LevelError, "kits", fmt.Sprintf("agent %q matches kit %s, but that kit is a %s, not a workload", agent.val, k.source, k.descKind), "agent", agent.origin())
			}
			return
		}
	}
	if builtinAgents[agent.val] || !strings.ContainsAny(agent.val, "/:") {
		rs.coverage(CoverageBuiltinAgent, agent.val, "the built-in agent workload is supplied by sbx; its own network policy, credentials, and hooks are not shown", agent.origin())
	}
}
