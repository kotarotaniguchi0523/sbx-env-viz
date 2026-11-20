package sbxenv

import (
	"path/filepath"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

// vnode is a merged value with provenance. It never leaves this package.
type vnode struct {
	kind parser.Kind
	raw  string // scalar text before expansion
	val  string // scalar text after expansion, used for logic
	// disp is val as it may be shown: in strict mode every substituted
	// argument value is replaced by <redacted>.
	disp   string
	quoted bool
	pos    parser.Pos // key position for map values, item position for list entries
	src    string     // source ID
	layer  int
	// fileDir is the directory of the declaring file.
	fileDir string
	// argRefs are env.args names referenced in this scalar.
	argRefs []string
	keys    []string
	kv      map[string]*vnode
	items   []*vnode
	hist    []hent
}

type hent struct {
	op   HistoryOp
	node *vnode
}

func (v *vnode) get(key string) *vnode {
	if v == nil || v.kind != parser.Map {
		return nil
	}
	return v.kv[key]
}

func (v *vnode) str(key string) string {
	c := v.get(key)
	if c == nil || c.kind != parser.Scalar {
		return ""
	}
	return c.val
}

// show returns the display text of a scalar.
func (v *vnode) show() string {
	if v == nil || v.kind != parser.Scalar {
		return ""
	}
	return v.disp
}

// dstr returns the display text of a scalar child.
func (v *vnode) dstr(key string) string { return v.get(key).show() }

// dscalars returns the display texts of a scalar list.
func (v *vnode) dscalars() []string {
	var out []string
	for _, it := range v.list() {
		if it.kind == parser.Scalar {
			out = append(out, it.disp)
		}
	}
	return out
}

func (v *vnode) has(key string) bool { return v.get(key) != nil && v.get(key).kind != parser.Null }

func (v *vnode) origin() Origin {
	return Origin{SourceID: v.src, Line: v.pos.Line, Col: v.pos.Col}
}

// list returns the items of a sequence, or nil.
func (v *vnode) list() []*vnode {
	if v == nil || v.kind != parser.Seq {
		return nil
	}
	return v.items
}

// scalars returns the string values of a scalar list.
func (v *vnode) scalars() []string {
	var out []string
	for _, it := range v.list() {
		if it.kind == parser.Scalar {
			out = append(out, it.val)
		}
	}
	return out
}

// clone deep-copies a node (used to snapshot history).
func (v *vnode) clone() *vnode {
	if v == nil {
		return nil
	}
	c := *v
	if v.kv != nil {
		c.kv = make(map[string]*vnode, len(v.kv))
		for k, x := range v.kv {
			c.kv[k] = x.clone()
		}
		c.keys = append([]string(nil), v.keys...)
	}
	if v.items != nil {
		c.items = make([]*vnode, len(v.items))
		for i, x := range v.items {
			c.items[i] = x.clone()
		}
	}
	c.hist = append([]hent(nil), v.hist...)
	return &c
}

// collectArgRefs returns every env.args name referenced in the subtree.
func (v *vnode) collectArgRefs(out map[string]bool) {
	if v == nil {
		return
	}
	for _, r := range v.argRefs {
		out[r] = true
	}
	for _, k := range v.keys {
		v.kv[k].collectArgRefs(out)
	}
	for _, it := range v.items {
		it.collectArgRefs(out)
	}
}

// lostRef records that a layer overrode a value that referenced an argument.
type lostRef struct {
	arg        string
	path       string
	shadowed   Origin
	overridden Origin
}

type merger struct {
	lost []lostRef
}

// listStrategy names how a list at path merges.
func listStrategy(path string) string {
	switch path {
	case "entrypoint", "cmd":
		return "replace"
	case "ports", "mcp.servers", "additionalWorkspaces":
		return "keyed"
	case "kits":
		return "kits"
	}
	return "append"
}

// listKey returns the identity of a keyed list entry, or "" when it has none.
func listKey(path string, e *vnode) string {
	switch path {
	case "ports":
		if e.kind != parser.Map || !e.has("sandbox") {
			return ""
		}
		return e.str("sandbox") + "/" + effectiveProtocol(e)
	case "mcp.servers":
		return e.str("name")
	case "additionalWorkspaces":
		if t := e.str("target"); t != "" {
			return "target:" + t
		}
		if p := e.str("path"); p != "" {
			return "target:" + filepath.ToSlash(resolveHostPath(e.fileDir, p))
		}
	case "kits":
		return kitSource(e)
	}
	return ""
}

func effectiveProtocol(port *vnode) string {
	p := strings.ToLower(port.str("protocol"))
	if p != "" {
		return p
	}
	if strings.Contains(port.str("hostIP"), ":") {
		return "tcp6"
	}
	return "tcp4"
}

func kitSource(e *vnode) string {
	if e.kind == parser.Scalar {
		return e.val
	}
	return e.str("source")
}

// mergeInto merges src (one layer) into dst at path. It returns the new node.
func (m *merger) mergeInto(dst, src *vnode, path string) *vnode {
	if src == nil {
		return dst
	}
	if src.kind == parser.Null {
		// An omitted or null value inherits the earlier value.
		if dst == nil {
			src.hist = []hent{{op: OpSet, node: src.clone()}}
			return src
		}
		return dst
	}
	if dst == nil || dst.kind == parser.Null {
		switch src.kind {
		case parser.Map:
			fresh := *src
			fresh.kv = make(map[string]*vnode, len(src.kv))
			fresh.keys = nil
			fresh.hist = []hent{{op: OpSet, node: shallow(src)}}
			for _, k := range src.keys {
				fresh.keys = append(fresh.keys, k)
				fresh.kv[k] = m.mergeInto(nil, src.kv[k], joinKey(path, k))
			}
			return &fresh
		case parser.Seq:
			return m.mergeList(nil, src, path)
		}
		m.stampSet(src)
		return src
	}
	switch {
	case dst.kind == parser.Map && src.kind == parser.Map:
		for _, k := range src.keys {
			child := src.kv[k]
			cp := joinKey(path, k)
			if old, ok := dst.kv[k]; ok {
				dst.kv[k] = m.mergeInto(old, child, cp)
			} else {
				dst.keys = append(dst.keys, k)
				dst.kv[k] = m.mergeInto(nil, child, cp)
			}
		}
		dst.hist = append(dst.hist, hent{op: OpMerge, node: shallow(src)})
		return dst
	case dst.kind == parser.Seq && src.kind == parser.Seq && listStrategy(path) != "replace":
		return m.mergeList(dst, src, path)
	}
	// Scalar override, kind change, or a replace-strategy list. The new
	// value goes through the same path as a first appearance, so keyed
	// lists inside it are deduplicated exactly as in a single file.
	m.recordLost(dst, src, path)
	hist := shadowAll(dst.hist)
	snapshot := shallow(src)
	norm := m.mergeInto(nil, src, path)
	norm.hist = append(hist, hent{op: OpReplace, node: snapshot})
	return norm
}

func (m *merger) mergeList(dst, src *vnode, path string) *vnode {
	if dst == nil {
		dst = &vnode{kind: parser.Seq, pos: src.pos, src: src.src, layer: src.layer, fileDir: src.fileDir}
		dst.hist = []hent{{op: OpSet, node: shallow(src)}}
	} else {
		dst.hist = append(dst.hist, hent{op: OpAppend, node: shallow(src)})
	}
	strategy := listStrategy(path)
	for _, e := range src.items {
		key := ""
		if strategy == "keyed" || strategy == "kits" {
			key = listKey(path, e)
		}
		idx := -1
		if key != "" {
			for i, old := range dst.items {
				if listKey(path, old) == key {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			m.stampSet(e)
			if e.layer != dst.layer {
				e.hist[len(e.hist)-1].op = OpAppend
			}
			dst.items = append(dst.items, e)
			continue
		}
		old := dst.items[idx]
		if strategy == "kits" {
			dst.items[idx] = m.mergeKitEntry(old, e, path)
			continue
		}
		// The last entry replaces the whole earlier one, keeping its position.
		m.recordLost(old, e, path)
		e.hist = append(shadowAll(old.hist), hent{op: OpReplace, node: e.clone()})
		dst.items[idx] = e
	}
	return dst
}

// mergeKitEntry folds two kit entries with the same source: arguments merge
// key by key and later values win.
func (m *merger) mergeKitEntry(old, e *vnode, path string) *vnode {
	oldArgs := old.get("args")
	newArgs := e.get("args")
	merged := old
	if old.kind == parser.Scalar {
		merged = &vnode{kind: parser.Map, pos: old.pos, src: old.src, layer: old.layer, fileDir: old.fileDir,
			kv: map[string]*vnode{"source": old}, keys: []string{"source"}, hist: old.hist}
	}
	if newArgs != nil {
		merged.kv["args"] = m.mergeInto(oldArgs, newArgs, path+".args")
		if oldArgs == nil {
			merged.keys = append(merged.keys, "args")
		}
	}
	merged.hist = append(merged.hist, hent{op: OpMerge, node: e.clone()})
	return merged
}

func (m *merger) stampSet(v *vnode) {
	if v == nil {
		return
	}
	if len(v.hist) == 0 {
		v.hist = []hent{{op: OpSet, node: shallow(v)}}
	}
	for _, k := range v.keys {
		m.stampSet(v.kv[k])
	}
	for _, it := range v.items {
		m.stampSet(it)
	}
}

func (m *merger) recordLost(old, repl *vnode, path string) {
	before := map[string]bool{}
	old.collectArgRefs(before)
	if len(before) == 0 {
		return
	}
	after := map[string]bool{}
	repl.collectArgRefs(after)
	for _, a := range sortedKeys(before) {
		if !after[a] {
			m.lost = append(m.lost, lostRef{arg: a, path: path, shadowed: old.origin(), overridden: repl.origin()})
		}
	}
}

func shadowAll(h []hent) []hent {
	out := make([]hent, len(h))
	for i, e := range h {
		e.op = OpShadow
		out[i] = e
	}
	return out
}

// shallow snapshots a node without its history (to keep history acyclic).
func shallow(v *vnode) *vnode {
	c := v.clone()
	c.hist = nil
	return c
}

// resolveHostPath resolves p against dir unless it is absolute.
func resolveHostPath(dir, p string) string {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || hasExpr(p) {
		return p
	}
	if dir == "" {
		return p
	}
	return filepath.Join(dir, p)
}

// mapKeys returns the keys of a mapping in order; nil-safe.
func (v *vnode) mapKeys() []string {
	if v == nil || v.kind != parser.Map {
		return nil
	}
	return v.keys
}
