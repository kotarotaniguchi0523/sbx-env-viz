package sbxenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

// ErrNoLayers is returned when Resolve receives no environment files.
var ErrNoLayers = errors.New("no environment files to resolve")

// ParseError reports a file that could not be parsed at all.
type ParseError struct {
	// Path is already redacted ($HOME shortened).
	Path string
	Err  error
}

func (e *ParseError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *ParseError) Unwrap() error { return e.Err }

// resolver carries state for one Resolve call.
type resolver struct {
	opts    ResolveOptions
	r       *redactor
	sources []Source
	diags   []Diagnostic
	cover   []Coverage
	items   []Item
	bounds  []Boundary

	layers []*layerState
	root   *vnode

	argDecls  map[string]*argDecl
	argOrder  []string
	argValues map[string]*ArgValue
	envArgs   []ArgValue

	kits    []*kitState
	kitArgs []ArgValue
	// net holds every network-policy entry of every expanded kit.
	net networkPolicy
	// skippedItems are items of capabilities the host would not select;
	// they get no boundaries.
	skippedItems map[string]bool
	lostByArg    map[string][]lostRef

	itemIndex map[string]int
}

type layerState struct {
	in   LayerInput
	id   string
	dir  string
	tree *parser.Node
}

type argDecl struct {
	name        string
	def         *string
	defOrigin   Origin
	required    bool
	enum        []string
	pattern     string
	description string
	origin      Origin
	// defNode carries the default's merge history across layers.
	defNode *vnode
	// export, buildArg, and buildSecret are kit-arg delivery mechanisms.
	export, buildArg, buildSecret string
}

// Resolve merges layers in order and analyses the result.
func Resolve(layers []LayerInput, opts ResolveOptions) (*Plan, error) {
	if len(layers) == 0 {
		return nil, ErrNoLayers
	}
	rs := &resolver{
		opts:         opts,
		r:            newRedactor(opts.Redaction, opts.HomeDir, opts.HomeAliases),
		argDecls:     map[string]*argDecl{},
		argValues:    map[string]*ArgValue{},
		itemIndex:    map[string]int{},
		skippedItems: map[string]bool{},
	}
	if err := rs.parseLayers(layers); err != nil {
		return nil, err
	}
	rs.collectArgDecls()
	rs.resolveEnvArgs()
	rs.mergeLayers()
	rs.expandKits()
	rs.net = compileNetworkPolicy(rs.kits)
	rs.buildItems()
	rs.analyze()
	return rs.finish(), nil
}

func (rs *resolver) addSource(s Source) string {
	if s.ID == "" {
		prefix := map[SourceKind]string{SourceKit: "K", SourceEnvArgsFile: "A", SourceKitArgsFile: "B"}[s.Kind]
		n := 1
		for _, x := range rs.sources {
			if x.Kind == s.Kind {
				n++
			}
		}
		s.ID = fmt.Sprintf("%s%d", prefix, n)
	}
	rs.sources = append(rs.sources, s)
	return s.ID
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (rs *resolver) diag(code string, level DiagLevel, cat, msg, itemID string, origins ...Origin) {
	rs.diags = append(rs.diags, Diagnostic{
		Code: code, Level: level, Message: rs.r.text(msg), ItemID: itemID, Origins: origins, Category: cat,
	})
}

func (rs *resolver) coverage(kind CoverageKind, ref, reason string, o Origin) {
	for _, c := range rs.cover {
		if c.Kind == kind && c.Ref == rs.r.text(ref) {
			return
		}
	}
	rs.cover = append(rs.cover, Coverage{Kind: kind, Ref: rs.r.text(ref), Reason: reason, Origin: o})
}

func (rs *resolver) parseLayers(layers []LayerInput) error {
	for i, in := range layers {
		kind := SourceEnv
		if in.Kind == LayerUserDefaults {
			kind = SourceUserDefaults
		}
		id := fmt.Sprintf("L%d", i+1)
		rs.addSource(Source{ID: id, Kind: kind, Path: rs.r.path(in.Path), SHA256: digest(in.Content), Layer: i + 1, Expanded: true})
		res, err := parser.Parse(in.Content)
		if err != nil {
			return &ParseError{Path: rs.r.path(in.Path), Err: err}
		}
		ls := &layerState{in: in, id: id, dir: filepath.Dir(in.Path), tree: res.Root}
		rs.layers = append(rs.layers, ls)
		for _, d := range res.Duplicates {
			rs.diag("DUPLICATE_KEY", LevelError, "parse",
				fmt.Sprintf("key %q is defined twice in the same mapping (first at line %d, again at line %d); sbx rejects duplicate keys", d.Path, d.First.Line, d.Again.Line),
				"", Origin{SourceID: id, Line: d.First.Line, Col: d.First.Col}, Origin{SourceID: id, Line: d.Again.Line, Col: d.Again.Col})
		}
		if res.Root.Kind != parser.Map {
			if res.Root.Kind != parser.Null {
				rs.diag("NOT_A_MAPPING", LevelError, "schema", "the environment file must be a YAML mapping", "", Origin{SourceID: id, Line: res.Root.Pos.Line})
			}
			continue
		}
		var issues []schemaIssue
		checkShape(res.Root, envFileSchema, "", &issues)
		for _, is := range issues {
			code := "TYPE_MISMATCH"
			if is.unknown {
				code = "UNKNOWN_FIELD"
			}
			rs.diag(code, LevelError, "schema", is.message(), "", Origin{SourceID: id, Line: is.pos.Line, Col: is.pos.Col})
		}
		rs.checkLayerScalars(ls)
	}
	return nil
}

// checkLayerScalars reports per-file rules that hold before merging.
func (rs *resolver) checkLayerScalars(ls *layerState) {
	root := ls.tree
	o := func(n *parser.Node) Origin { return Origin{SourceID: ls.id, Line: n.Pos.Line, Col: n.Pos.Col} }
	if sv := root.Get("schemaVersion"); sv != nil && sv.Kind == parser.Scalar && sv.Value != "1" {
		rs.diag("SCHEMA_VERSION", LevelError, "schema", fmt.Sprintf("schemaVersion must be \"1\" (found %q)", sv.Value), "schemaVersion", o(sv))
	}
	if ls.in.Kind == LayerUserDefaults {
		if n := root.Get("name"); n != nil {
			rs.diag("USER_DEFAULTS_NAME", LevelError, "schema", "~/.sbxenv.yaml cannot set name; set it in a project file or with --name", "name", o(n))
		}
		if ws := root.Get("workspace"); ws != nil {
			p := ws.Value
			if ws.Kind == parser.Map {
				if pp := ws.Get("path"); pp != nil {
					p = pp.Value
				}
			}
			if !strings.HasPrefix(strings.TrimSpace(p), "${{ env.projectDir }}") && !regexp.MustCompile(`^\$\{\{\s*env\.projectDir\s*\}\}`).MatchString(strings.TrimSpace(p)) {
				rs.diag("USER_DEFAULTS_WORKSPACE", LevelError, "schema", "~/.sbxenv.yaml may only set workspace to ${{ env.projectDir }} or a subdirectory of it", "workspace", o(ws))
			}
		}
	}
	rs.walkKeys(root, ls.id, "")
}

// walkKeys reports expressions written in field names.
func (rs *resolver) walkKeys(n *parser.Node, src, path string) {
	if n == nil {
		return
	}
	for _, kv := range n.Keys {
		p := joinKey(path, kv.Key)
		if hasExpr(kv.Key) {
			rs.diag("EXPR_IN_KEY", LevelError, "expression", fmt.Sprintf("expressions cannot be used in field names (%s)", p), "", Origin{SourceID: src, Line: kv.KeyPos.Line, Col: kv.KeyPos.Col})
		}
		if p == "args" {
			if containsExpr(kv.Value) {
				rs.diag("EXPR_IN_ARGS", LevelError, "expression", "expressions cannot be used inside the args block", "", Origin{SourceID: src, Line: kv.KeyPos.Line})
			}
			continue
		}
		rs.walkKeys(kv.Value, src, p)
	}
	for i, it := range n.Items {
		rs.walkKeys(it, src, fmt.Sprintf("%s[%d]", path, i))
	}
}

func containsExpr(n *parser.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == parser.Scalar && strings.Contains(strings.ReplaceAll(n.Value, "$${{", ""), "${{") {
		return true
	}
	for _, kv := range n.Keys {
		if containsExpr(kv.Value) {
			return true
		}
	}
	for _, it := range n.Items {
		if containsExpr(it) {
			return true
		}
	}
	return false
}

func (rs *resolver) collectArgDecls() {
	// Each file must be valid on its own; the declarations then merge with
	// the ordinary rules (maps by key, lists appended, scalars replaced).
	m := &merger{}
	var merged *vnode
	for _, ls := range rs.layers {
		args := ls.tree.Get("args")
		if args == nil || args.Kind != parser.Map {
			continue
		}
		for _, kv := range args.Keys {
			o := Origin{SourceID: ls.id, Line: kv.KeyPos.Line, Col: kv.KeyPos.Col}
			if !envArgNameRe.MatchString(kv.Key) {
				rs.diag("ARG_NAME_INVALID", LevelError, "args", fmt.Sprintf("argument name %q must start with a letter or underscore and contain only letters, digits, underscores, and hyphens", kv.Key), "", o)
			}
			if _, ok := rs.argDecls[kv.Key]; !ok {
				rs.argDecls[kv.Key] = &argDecl{name: kv.Key}
				rs.argOrder = append(rs.argOrder, kv.Key)
			}
			rs.argDecls[kv.Key].origin = o
			v := kv.Value
			if v == nil || v.Kind != parser.Map {
				continue
			}
			required := v.Get("required") != nil && v.Get("required").Value == "true"
			if v.Get("default") != nil && required {
				rs.diag("ARG_DECL_INVALID", LevelError, "args", fmt.Sprintf("argument %q sets both default and required: true; exactly one is allowed", kv.Key), "args."+kv.Key, o)
			}
			if v.Get("default") == nil && !required {
				rs.diag("ARG_DECL_INVALID", LevelError, "args", fmt.Sprintf("argument %q must set exactly one of default or required: true", kv.Key), "args."+kv.Key, o)
			}
			if v.Get("enum") != nil && v.Get("pattern") != nil {
				rs.diag("ARG_DECL_INVALID", LevelError, "args", fmt.Sprintf("argument %q sets both enum and pattern; they cannot be combined", kv.Key), "args."+kv.Key, o)
			}
		}
		merged = m.mergeInto(merged, rs.convert(args, ls, "args", false), "args")
	}
	for _, name := range rs.argOrder {
		d, v := rs.argDecls[name], merged.get(name)
		def, req := v.get("default"), v.get("required")
		// When layers set both, the later one decides.
		useDefault := def != nil && def.kind == parser.Scalar && (req == nil || req.val != "true" || def.layer >= req.layer)
		if useDefault {
			s := def.val
			d.def, d.defNode = &s, def
			d.defOrigin = def.origin()
		} else {
			d.required = req != nil && req.val == "true"
		}
		d.enum = v.get("enum").scalars()
		d.pattern = v.str("pattern")
		d.description = v.str("description")
	}
}

// argLine is one NAME=VALUE pair with its origin.
type argLine struct {
	name, value string
	origin      Origin
	from        string
}

func parseArgFile(content []byte, src string, from string) ([]argLine, []Origin) {
	var out []argLine
	var bad []Origin
	for i, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name, value, ok := strings.Cut(t, "=")
		if !ok || strings.TrimSpace(name) == "" {
			bad = append(bad, Origin{SourceID: src, Line: i + 1})
			continue
		}
		out = append(out, argLine{name: strings.TrimSpace(name), value: value, origin: Origin{SourceID: src, Line: i + 1}, from: from})
	}
	return out, bad
}

func (rs *resolver) cliOrigin(flag string, i int) Origin {
	return Origin{SourceID: "cli", Via: []string{fmt.Sprintf("%s #%d", flag, i+1)}}
}

func (rs *resolver) resolveEnvArgs() {
	var supplied []argLine
	for _, f := range rs.opts.EnvArgsFiles {
		id := rs.addSource(Source{Kind: SourceEnvArgsFile, Path: rs.r.path(f.Path), SHA256: digest(f.Content), Expanded: true})
		lines, bad := parseArgFile(f.Content, id, "env-args-file")
		for _, b := range bad {
			rs.diag("ARGS_FILE_SYNTAX", LevelError, "args", "each non-empty, non-comment line of an args file must be NAME=VALUE", "", b)
		}
		supplied = append(supplied, lines...)
	}
	for i, a := range rs.opts.EnvArgs {
		name, value, ok := strings.Cut(a, "=")
		o := rs.cliOrigin("--env-arg", i)
		if !ok || name == "" {
			rs.diag("ENV_ARG_SYNTAX", LevelError, "args", fmt.Sprintf("--env-arg #%d must have the form NAME=VALUE", i+1), "", o)
			continue
		}
		supplied = append(supplied, argLine{name: name, value: value, origin: o, from: "flag"})
	}
	o := rs.opts
	if len(o.EnvArgs) > 0 || len(o.KitArgs) > 0 || o.Name != "" || o.Clone != nil || o.SkipHostCommands || o.Cloud || o.TTL != "" || o.OnTimeout != "" {
		rs.addSource(Source{ID: "cli", Kind: SourceCLI, Path: "command line", Expanded: true})
	}
	for _, name := range rs.argOrder {
		d := rs.argDecls[name]
		if d.def != nil {
			rs.argValues[name] = &ArgValue{Name: name, Value: *d.def, From: "default", Origin: d.defOrigin}
		}
	}
	for _, s := range supplied {
		d, ok := rs.argDecls[s.name]
		if !ok {
			rs.diag("ENV_ARG_UNDECLARED", LevelError, "args", fmt.Sprintf("argument %q is supplied but no environment file declares it under args", s.name), "", s.origin)
			continue
		}
		_ = d
		rs.argValues[s.name] = &ArgValue{Name: s.name, Value: s.value, From: s.from, Origin: s.origin}
	}
	for _, name := range rs.argOrder {
		d := rs.argDecls[name]
		v, ok := rs.argValues[name]
		if !ok {
			if d.required {
				rs.diag("ENV_ARG_REQUIRED", LevelError, "args", fmt.Sprintf("argument %q is required; pass --env-arg %s=VALUE or an --env-args-file", name, name), "args."+name, d.origin)
			}
			rs.envArgs = append(rs.envArgs, ArgValue{Name: name, From: "unset", Origin: d.origin})
			continue
		}
		if len(d.enum) > 0 && !contains(d.enum, v.Value) {
			rs.diag("ENV_ARG_INVALID", LevelError, "args", fmt.Sprintf("argument %q is not one of its enum values", name), "args."+name, v.Origin, d.origin)
		}
		if d.pattern != "" {
			re, err := regexp.Compile("^(?:" + d.pattern + ")$")
			if err != nil {
				rs.diag("ARG_DECL_INVALID", LevelError, "args", fmt.Sprintf("argument %q has a pattern that is not a valid RE2 expression", name), "args."+name, d.origin)
			} else if !re.MatchString(v.Value) {
				rs.diag("ENV_ARG_INVALID", LevelError, "args", fmt.Sprintf("argument %q does not match its pattern", name), "args."+name, v.Origin, d.origin)
			}
		}
		av := *v
		av.Value = rs.r.value(v.Value)
		rs.envArgs = append(rs.envArgs, av)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// convert turns one parsed layer into a vnode tree, expanding expressions.
func (rs *resolver) convert(n *parser.Node, ls *layerState, path string, expandOn bool) *vnode {
	v := &vnode{kind: n.Kind, pos: n.Pos, src: ls.id, layer: indexOf(rs.layers, ls) + 1, fileDir: ls.dir, quoted: n.Quoted}
	switch n.Kind {
	case parser.Scalar:
		v.raw, v.val, v.disp = n.Value, n.Value, n.Value
		if expandOn && hasExpr(n.Value) {
			resolve := func(hide bool) func(string) exprResult {
				return func(body string) exprResult {
					switch body {
					case "env.projectDir":
						return exprResult{value: rs.opts.ProjectDir, ok: rs.opts.ProjectDir != ""}
					case "env.fileDir":
						return exprResult{value: ls.dir, ok: true}
					}
					if name, ok := envArgRef(body); ok {
						if av, ok := rs.argValues[name]; ok {
							if hide {
								return exprResult{value: redactedText, ok: true}
							}
							return exprResult{value: av.Value, ok: true}
						}
					}
					return exprResult{}
				}
			}
			e := expandString(n.Value, resolve(false))
			v.val, v.disp = e.out, e.out
			if rs.r.strict() {
				v.disp = expandString(n.Value, resolve(true)).out
			}
			for _, ref := range e.refs {
				if name, ok := envArgRef(ref); ok {
					v.argRefs = append(v.argRefs, name)
				}
			}
			for _, u := range e.unresolved {
				msg := fmt.Sprintf("expression ${{ %s }} at %s cannot be resolved", rs.r.expr(u), path)
				if name, ok := envArgRef(u); ok {
					if _, declared := rs.argDecls[name]; declared {
						msg = fmt.Sprintf("expression ${{ %s }} at %s has no value (argument %q is required and was not supplied)", rs.r.expr(u), path, name)
					} else {
						msg = fmt.Sprintf("expression ${{ %s }} at %s references an undeclared argument %q", rs.r.expr(u), path, name)
					}
				} else if !strings.HasPrefix(u, "env.") {
					msg += "; only env.args.NAME, env.projectDir, and env.fileDir are expanded in environment files"
				}
				rs.diag("UNRESOLVED_EXPR", LevelError, "expression", msg, path, Origin{SourceID: ls.id, Line: n.Pos.Line, Col: n.Pos.Col})
			}
		}
	case parser.Map:
		v.kv = map[string]*vnode{}
		for _, kv := range n.Keys {
			cp := joinKey(path, kv.Key)
			child := rs.convert(kv.Value, ls, cp, expandOn && cp != "args")
			child.pos = kv.KeyPos
			if _, dup := v.kv[kv.Key]; !dup {
				v.keys = append(v.keys, kv.Key)
			}
			v.kv[kv.Key] = child
		}
	case parser.Seq:
		for i, it := range n.Items {
			v.items = append(v.items, rs.convert(it, ls, fmt.Sprintf("%s[%d]", path, i), expandOn))
		}
	}
	return v
}

func indexOf(ls []*layerState, x *layerState) int {
	for i, l := range ls {
		if l == x {
			return i
		}
	}
	return -1
}

func (rs *resolver) mergeLayers() {
	m := &merger{}
	for _, a := range rs.envArgs {
		if a.From == "flag" || a.From == "env-args-file" {
			if m.lostByArg == nil {
				m.lostByArg = make(map[string][]lostRef)
			}
			m.lostByArg[a.Name] = nil
		}
	}
	var root *vnode
	for _, ls := range rs.layers {
		if ls.tree.Kind != parser.Map {
			continue
		}
		tree := rs.convert(ls.tree, ls, "", true)
		root = m.mergeInto(root, tree, "")
	}
	if root == nil {
		root = &vnode{kind: parser.Map, kv: map[string]*vnode{}}
	}
	rs.root = root
	rs.lostByArg = m.lostByArg
}
