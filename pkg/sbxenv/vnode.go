//declscope:shared // vnode.go is the merged value model: every phase reads values and their provenance through it
package sbxenv

import (
	"path/filepath"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
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
//
//declscope:private // only dstr shows a single scalar
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

// mapKeys returns the keys of a mapping in order; nil-safe.
func (v *vnode) mapKeys() []string {
	if v == nil || v.kind != parser.Map {
		return nil
	}
	return v.keys
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
