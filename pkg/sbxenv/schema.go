package sbxenv

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

// shape describes the expected structure of a YAML subtree.
type shape struct {
	// kind is "scalar", "map", "seq", "free" (map of anything), or "any".
	kind   string
	fields map[string]*shape
	elem   *shape
	// alt accepts a second shape (string-or-object fields).
	alt *shape
}

func sScalar() *shape                      { return &shape{kind: "scalar"} }
func sAny() *shape                         { return &shape{kind: "any"} }
func sFree(elem *shape) *shape             { return &shape{kind: "free", elem: elem} }
func sSeq(elem *shape) *shape              { return &shape{kind: "seq", elem: elem} }
func sMap(fields map[string]*shape) *shape { return &shape{kind: "map", fields: fields} }
func sEither(a, b *shape) *shape           { a2 := *a; a2.alt = b; return &a2 }
func sScalarList() *shape                  { return sSeq(sScalar()) }
func sSecretSource() map[string]*shape {
	return map[string]*shape{
		"value": sScalar(), "ref": sScalar(), "command": sScalar(),
		"snapshot": sScalar(), "refresh": sScalar(), "backend": sScalar(), "noVerify": sScalar(),
	}
}

// envFileSchema follows the Docker Docs "Sandbox environment files" reference.
var envFileSchema = sMap(map[string]*shape{
	"schemaVersion": sScalar(),
	"name":          sScalar(),
	"agent":         sScalar(),
	"entrypoint":    sScalarList(),
	"cmd":           sScalarList(),
	"args": sFree(sMap(map[string]*shape{
		"default": sScalar(), "required": sScalar(), "description": sScalar(),
		"enum": sScalarList(), "pattern": sScalar(),
	})),
	"kits": sSeq(sEither(sScalar(), sMap(map[string]*shape{
		"source": sScalar(), "args": sFree(sScalar()),
	}))),
	"workspace": sEither(sScalar(), sMap(map[string]*shape{
		"path": sScalar(), "clone": sScalar(),
	})),
	"additionalWorkspaces": sSeq(sMap(map[string]*shape{
		"path": sScalar(), "target": sScalar(), "readOnly": sScalar(),
	})),
	"mounts": sFree(sScalar()),
	"env":    sFree(sScalar()),
	"sandboxOptions": sMap(map[string]*shape{
		"template": sScalar(), "memory": sScalar(), "cpus": sScalar(), "pullPolicy": sScalar(),
		"profile": sScalar(), "skills": sScalar(), "display": sScalar(), "fuse": sScalar(),
		"gpu": sScalar(), "usb": sScalarList(), "writableEnvFiles": sScalar(),
	}),
	"secrets": sFree(sMap(sSecretSource())),
	"bindings": sFree(sMap(map[string]*shape{
		"apiKey": sMap(map[string]*shape{"domains": sScalarList()}),
		"oauth":  sMap(map[string]*shape{"domains": sScalarList()}),
	})),
	"registries": sFree(sMap(map[string]*shape{
		"username": sMap(sSecretSource()),
		"secret":   sMap(sSecretSource()),
	})),
	"mcp": sMap(map[string]*shape{
		"servers": sSeq(sMap(map[string]*shape{
			"name": sScalar(), "url": sScalar(), "command": sScalar(), "args": sScalarList(),
		})),
	}),
	"ports": sSeq(sMap(map[string]*shape{
		"sandbox": sScalar(), "host": sScalar(), "protocol": sScalar(), "hostIP": sScalar(),
	})),
	"lifecycle": sMap(map[string]*shape{
		"initialize": sSeq(lifecycleCmd()),
		"postCreate": sSeq(lifecycleCmd()),
		"preRemove":  sSeq(lifecycleCmd()),
	}),
})

func lifecycleCmd() *shape {
	return sMap(map[string]*shape{
		"name": sScalar(), "command": sScalar(), "workdir": sScalar(), "timeout": sScalar(),
	})
}

// kitCapEntry is a single capability request; config is checked per type.
var kitCapEntry = sMap(map[string]*shape{
	"type": sScalar(), "name": sScalar(), "optional": sScalar(), "description": sScalar(),
	"config": sAny(), "source": sAny(), "declarationBlocks": sScalarList(),
	"group": sMap(map[string]*shape{
		"name": sScalar(), "optional": sScalar(), "description": sScalar(),
		"capabilities": sSeq(sAny()),
	}),
})

// kitSchema follows SPEC-v3 §4 (docker/sandbox-kit-spec).
var kitSchema = sMap(map[string]*shape{
	"schemaVersion": sScalar(), "kind": sScalar(), "displayName": sScalar(), "author": sScalar(),
	"description": sScalar(), "sourceUrl": sScalar(), "iconUrl": sScalar(), "version": sScalar(),
	"licenses": sScalarList(), "provides": sScalarList(), "requires": sScalarList(),
	"integrates": sScalarList(), "conflicts": sScalarList(),
	"capabilities": sSeq(kitCapEntry),
	"args": sFree(sMap(map[string]*shape{
		"default": sScalar(), "required": sScalar(), "description": sScalar(), "enum": sScalarList(),
		"pattern": sScalar(), "env": sScalar(), "buildArg": sScalar(), "buildSecret": sScalar(),
	})),
	"build": sScalar(), "dockerfile": sScalar(), "kits": sSeq(sAny()),
})

// capConfigSchemas are strict config shapes for the capability types this
// tool models in depth.
func netPolicyV2() *shape {
	entry := sSeq(sEither(sScalar(), sMap(map[string]*shape{
		"hosts": sScalarList(), "methods": sScalarList(), "paths": sScalarList(),
	})))
	phase := sMap(map[string]*shape{"allow": entry, "deny": entry})
	return sMap(map[string]*shape{"install": phase, "runtime": phase})
}

var capConfigSchemas = map[string]*shape{
	"network-policy@2": netPolicyV2(),
	"network-policy@1": sMap(map[string]*shape{
		"install": sMap(map[string]*shape{"allow": sScalarList(), "deny": sScalarList()}),
		"runtime": sMap(map[string]*shape{"allow": sScalarList(), "deny": sScalarList()}),
	}),
	"credential@1": sMap(map[string]*shape{
		"service": sScalar(),
		"phase":   sEither(sScalar(), sScalarList()),
		"apiKey": sMap(map[string]*shape{
			"name": sScalar(), "proxyManaged": sScalar(),
			"inject": sSeq(sMap(map[string]*shape{
				"domain": sScalar(), "header": sScalar(), "format": sScalar(),
				"scheme": sScalar(), "username": sScalar(),
			})),
		}),
		"oauth": sMap(map[string]*shape{
			"tokenEndpoint":  sMap(map[string]*shape{"host": sScalar(), "path": sScalar()}),
			"resourceHosts":  sScalarList(),
			"sentinels":      sMap(map[string]*shape{"accessToken": sScalar(), "refreshToken": sScalar()}),
			"credentialFile": sMap(map[string]*shape{"path": sScalar(), "format": sScalar(), "structure": sAny()}),
			"responseFields": sMap(map[string]*shape{"accessToken": sScalar(), "refreshToken": sScalar(), "expiresIn": sScalar()}),
			"passthrough":    sScalar(),
		}),
	}),
	"host-mount@1": sMap(map[string]*shape{"path": sScalar(), "mode": sScalar()}),
	"ssh-agent@1": sMap(map[string]*shape{
		"phase":        sEither(sScalar(), sScalarList()),
		"unrestricted": sScalar(), "sign": sScalarList(), "authenticate": sScalarList(),
	}),
	"lifecycle@1": sMap(map[string]*shape{
		"install": sSeq(sMap(map[string]*shape{
			"command": sEither(sScalar(), sScalarList()), "user": sScalar(),
			"env": sScalarList(), "description": sScalar(),
		})),
		"startup": sSeq(sMap(map[string]*shape{
			"command": sEither(sScalar(), sScalarList()), "user": sScalar(), "background": sScalar(),
			"env": sScalarList(), "description": sScalar(),
		})),
		"files": sSeq(sMap(map[string]*shape{
			"path": sScalar(), "content": sScalar(), "mode": sScalar(), "overwrite": sScalar(),
			"description": sScalar(),
		})),
		"interactive": sScalarList(),
	}),
}

// schemaIssue is an unknown field or a shape mismatch.
type schemaIssue struct {
	path    string
	pos     parser.Pos
	unknown bool
	want    string
	got     string
}

func (i schemaIssue) message() string {
	if i.unknown {
		return fmt.Sprintf("unknown field %q (sbx decodes environment files and kit descriptors strictly, so this is rejected)", i.path)
	}
	return fmt.Sprintf("field %q should be a %s, found a %s", i.path, i.want, i.got)
}

func checkShape(n *parser.Node, s *shape, path string, out *[]schemaIssue) {
	if n == nil || s == nil || s.kind == "any" || n.Kind == parser.Null {
		return
	}
	if !shapeMatches(n, s) {
		if s.alt != nil && shapeMatches(n, s.alt) {
			checkShape(n, s.alt, path, out)
			return
		}
		want := s.kind
		if s.alt != nil {
			want += " or " + s.alt.kind
		}
		*out = append(*out, schemaIssue{path: path, pos: n.Pos, want: describeKind(want), got: n.Kind.String()})
		return
	}
	switch s.kind {
	case "map":
		for _, kv := range n.Keys {
			child, ok := s.fields[kv.Key]
			p := joinKey(path, kv.Key)
			if !ok {
				*out = append(*out, schemaIssue{path: p, pos: kv.KeyPos, unknown: true})
				continue
			}
			checkShape(kv.Value, child, p, out)
		}
	case "free":
		for _, kv := range n.Keys {
			checkShape(kv.Value, s.elem, joinKey(path, kv.Key), out)
		}
	case "seq":
		for i, it := range n.Items {
			checkShape(it, s.elem, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

func shapeMatches(n *parser.Node, s *shape) bool {
	switch s.kind {
	case "scalar":
		return n.Kind == parser.Scalar
	case "map", "free":
		return n.Kind == parser.Map
	case "seq":
		return n.Kind == parser.Seq
	}
	return true
}

func describeKind(k string) string {
	r := strings.NewReplacer("free", "map", "scalar", "scalar value", "seq", "list")
	return r.Replace(k)
}

func joinKey(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
