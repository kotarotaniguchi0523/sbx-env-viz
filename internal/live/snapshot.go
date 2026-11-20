package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
)

// Snapshot is what sbx reported about existing sandboxes.
type Snapshot struct {
	// Available is true when `sbx ls --json` answered.
	Available bool
	// Problem explains in Japanese why live data is missing or partial.
	Problem   string
	Sandboxes []Sandbox
	MCP       []MCPServer
	// MCPKnown is true when `sbx mcp ls --json` answered.
	MCPKnown bool
}

// Sandbox is one row of `sbx ls --json` plus its policy and secret names.
type Sandbox struct {
	Name, ID, Agent, Status string
	Workspaces              []Mount
	Mounts                  []Mount
	Ports                   []Port
	CreatedAt, LastUsedAt   string

	Rules      []Rule
	RulesKnown bool
	Secrets    []string
	// SecretsKnown is true when `sbx secret ls --sandbox NAME --json`
	// answered. Only names are read; values are never requested.
	SecretsKnown bool
}

// Mount is a host directory mounted into a sandbox. ReadOnly is nil when
// sbx did not report the access mode.
type Mount struct {
	Path     string
	Target   string
	ReadOnly *bool
}

// Port is a published port.
type Port struct {
	HostIP      string
	HostPort    int
	SandboxPort int
	Protocol    string
}

// Rule is one effective network rule.
type Rule struct {
	Decision   string // allow or deny
	Resource   string
	Source     string // local, org, kit, ...
	CreatedVia string // default, added, provisioned, approval
}

// MCPServer is one row of `sbx mcp ls --json`.
type MCPServer struct {
	Name, Transport, Status, AuthStatus string
}

// Collect asks sbx about every sandbox. It never fails: a missing sbx, a
// stopped daemon, or unreadable output leaves Available false and a
// Japanese Problem, so the caller falls back to the files alone.
func Collect(ctx context.Context, r Runner) Snapshot {
	var s Snapshot
	if r == nil {
		r = missing{}
	}
	out, err := r.Run(ctx, "ls", "--json")
	switch {
	case errors.Is(err, ErrNoSbx):
		s.Problem = "sbx コマンドが見つからないため、稼働中の sandbox は表示していません（設定ファイルだけを解析しています）。"
		return s
	case err != nil:
		s.Problem = "sbx から sandbox の一覧を取得できませんでした（デーモンが停止している可能性があります）。設定ファイルだけを解析しています: " + clean(err.Error())
		return s
	}
	s.Sandboxes, err = parseList(out)
	if err != nil {
		s.Problem = "sbx ls --json の出力を読めませんでした。設定ファイルだけを解析しています: " + clean(err.Error())
		return s
	}
	s.Available = true
	var partial []string
	for i := range s.Sandboxes {
		sb := &s.Sandboxes[i]
		if sb.Name == "" {
			continue
		}
		if out, err := r.Run(ctx, "policy", "ls", sb.Name, "--json"); err == nil {
			if sb.Rules, err = parseRules(out); err == nil {
				sb.RulesKnown = true
			}
		}
		if !sb.RulesKnown {
			partial = append(partial, sb.Name+" の通信ルール")
		}
		if out, err := r.Run(ctx, "secret", "ls", "--sandbox", sb.Name, "--json"); err == nil {
			if sb.Secrets, err = parseSecrets(out, sb.Name); err == nil {
				sb.SecretsKnown = true
			}
		}
		if !sb.SecretsKnown {
			partial = append(partial, sb.Name+" のシークレット一覧")
		}
	}
	if out, err := r.Run(ctx, "mcp", "ls", "--json"); err == nil {
		if s.MCP, err = parseMCP(out); err == nil {
			s.MCPKnown = true
		}
	}
	if !s.MCPKnown {
		partial = append(partial, "MCP サーバーの一覧")
	}
	if len(partial) > 0 {
		s.Problem = "sbx から取得できなかった情報があります（その項目は比較していません）: " + strings.Join(partial, "、")
	}
	return s
}

type missing struct{}

func (missing) Run(context.Context, ...string) ([]byte, error) { return nil, ErrNoSbx }

func clean(s string) string { return model.EscapeControl(firstLine(s)) }

// --- tolerant JSON reading ----------------------------------------------
//
// The sbx documentation fixes that list output is a JSON array with
// snake_case keys, and names the fields of `sbx ls` and of port rows. The
// exact nesting of policy, secret, and MCP rows is not documented, so the
// readers below look for the documented field names wherever they appear
// and ignore everything else.

func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// rows returns the objects of a top-level array, or of the array under one
// of keys when the top level is an object (older sbx releases).
func rows(v any, keys ...string) ([]map[string]any, error) {
	if m, ok := v.(map[string]any); ok {
		for _, k := range keys {
			if a, ok := m[k]; ok {
				v = a
				break
			}
		}
	}
	a, ok := v.([]any)
	if !ok {
		if v == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("expected a JSON array")
	}
	var out []map[string]any
	for _, e := range a {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return model.EscapeControl(v)
			}
		case json.Number:
			return v.String()
		case bool:
			return strconv.FormatBool(v)
		}
	}
	return ""
}

func num(m map[string]any, keys ...string) int {
	for _, k := range keys {
		switch v := m[k].(type) {
		case json.Number:
			if n, err := strconv.Atoi(v.String()); err == nil {
				return n
			}
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return 0
}

func boolp(m map[string]any, keys ...string) *bool {
	for _, k := range keys {
		if b, ok := m[k].(bool); ok {
			return &b
		}
	}
	switch strings.ToLower(str(m, "mode", "access")) {
	case "ro", "readonly", "read-only":
		t := true
		return &t
	case "rw", "readwrite", "read-write":
		f := false
		return &f
	}
	return nil
}

func parseList(b []byte) ([]Sandbox, error) {
	v, err := decode(b)
	if err != nil {
		return nil, err
	}
	rs, err := rows(v, "sandboxes")
	if err != nil {
		return nil, err
	}
	var out []Sandbox
	for _, m := range rs {
		out = append(out, Sandbox{
			Name: str(m, "name"), ID: str(m, "id"), Agent: str(m, "agent"), Status: str(m, "status", "state"),
			Workspaces: mounts(m["workspaces"]), Mounts: mounts(m["mounts"]), Ports: ports(m["ports"]),
			CreatedAt: str(m, "created_at"), LastUsedAt: str(m, "last_used_at"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func mounts(v any) []Mount {
	var list []any
	switch x := v.(type) {
	case []any:
		list = x
	case string, map[string]any:
		list = []any{x}
	}
	var out []Mount
	for _, e := range list {
		switch x := e.(type) {
		case string:
			if x != "" {
				out = append(out, Mount{Path: model.EscapeControl(x)})
			}
		case map[string]any:
			p := str(x, "path", "host_path", "workspace_path", "source", "host")
			if p == "" {
				continue
			}
			out = append(out, Mount{Path: p, Target: str(x, "target", "mount_target", "sandbox_path", "destination"),
				ReadOnly: boolp(x, "read_only", "readonly")})
		}
	}
	return out
}

var portText = regexp.MustCompile(`^(?:\[?([0-9A-Fa-f.:]*?)\]?:)?(\d+)->(\d+)(?:/(\w+))?$`)

func ports(v any) []Port {
	list, _ := v.([]any)
	var out []Port
	for _, e := range list {
		switch x := e.(type) {
		case map[string]any:
			out = append(out, Port{HostIP: str(x, "host_ip"), HostPort: num(x, "host_port"),
				SandboxPort: num(x, "sandbox_port", "port"), Protocol: strings.ToLower(str(x, "protocol"))})
		case string:
			if m := portText.FindStringSubmatch(strings.TrimSpace(x)); m != nil {
				hp, _ := strconv.Atoi(m[2])
				sp, _ := strconv.Atoi(m[3])
				out = append(out, Port{HostIP: m[1], HostPort: hp, SandboxPort: sp, Protocol: strings.ToLower(m[4])})
			}
		}
	}
	return out
}

// parseRules collects every object that carries a decision and a
// resource, inheriting source and created_via from enclosing objects.
func parseRules(b []byte) ([]Rule, error) {
	v, err := decode(b)
	if err != nil {
		return nil, err
	}
	seen := map[Rule]bool{}
	var out []Rule
	var walk func(v any, source, via string)
	walk = func(v any, source, via string) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e, source, via)
			}
		case map[string]any:
			if s := str(x, "source"); s != "" {
				source = s
			}
			if s := str(x, "created_via"); s != "" {
				via = s
			}
			decision := strings.ToLower(str(x, "decision"))
			typ := strings.ToLower(str(x, "resource_type", "type"))
			inactive := x["active"] == false || strings.EqualFold(str(x, "status"), "inactive")
			if (decision == "allow" || decision == "deny") && !strings.Contains(typ, "file") && !inactive {
				var res []string
				for _, k := range []string{"resource", "resource_value", "pattern", "domain", "host"} {
					if s := str(x, k); s != "" {
						res = append(res, s)
					}
				}
				if a, ok := x["resources"].([]any); ok {
					for _, e := range a {
						if s, ok := e.(string); ok && s != "" {
							res = append(res, model.EscapeControl(s))
						} else if m, ok := e.(map[string]any); ok {
							if s := str(m, "value", "resource", "pattern", "domain"); s != "" {
								res = append(res, s)
							}
						}
					}
				}
				for _, r := range res {
					rule := Rule{Decision: decision, Resource: strings.ToLower(r), Source: strings.ToLower(source), CreatedVia: strings.ToLower(via)}
					if !seen[rule] {
						seen[rule] = true
						out = append(out, rule)
					}
				}
			}
			for k, c := range x {
				if k != "resources" {
					walk(c, source, via)
				}
			}
		}
	}
	walk(v, "", "")
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Decision < out[j].Decision
	})
	return out, nil
}

// parseSecrets returns the service names of secret rows. Value fields are
// never read.
func parseSecrets(b []byte, sandbox string) ([]string, error) {
	v, err := decode(b)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			name := str(x, "service", "secret_name")
			if name == "" && (x["scope"] != nil || x["placeholder"] != nil || x["env_name"] != nil || x["secret_type"] != nil) {
				name = str(x, "name")
			}
			if sb := str(x, "sandbox", "sandbox_name"); name != "" && (sb == "" || sb == sandbox) && !strings.EqualFold(str(x, "scope"), "global") {
				seen[name] = true
			}
			for k, c := range x {
				if k != "value" && k != "secret_value" {
					walk(c)
				}
			}
		}
	}
	walk(v)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func parseMCP(b []byte) ([]MCPServer, error) {
	v, err := decode(b)
	if err != nil {
		return nil, err
	}
	rs, err := rows(v, "servers")
	if err != nil {
		return nil, err
	}
	var out []MCPServer
	for _, m := range rs {
		name := str(m, "name", "server_name")
		if name == "" {
			continue
		}
		out = append(out, MCPServer{Name: name, Transport: str(m, "transport", "transport_type"),
			Status: str(m, "status", "state"), AuthStatus: str(m, "auth_status")})
	}
	return out, nil
}
