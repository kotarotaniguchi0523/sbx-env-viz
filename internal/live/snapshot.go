package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
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

// Mount is a host directory mounted into a sandbox. sbx ls reports paths
// only, not whether a mount is read-only.
type Mount struct {
	Path string
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
	Decision string // allow or deny
	Resource string
	// Origin and Layer say where the rule comes from (sbx's own words).
	Origin, Layer string
	// CreatedVia is provenance.created_via: default, added, provisioned,
	// or approval.
	CreatedVia string
}

// MCPServer is one row of `sbx mcp ls --json`.
type MCPServer struct {
	Name, Transport, Status, Type string
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
			if sb.Secrets, err = parseSecrets(out); err == nil {
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

// --- reading sbx output -------------------------------------------------
//
// The structs below follow the JSON that sbx v0.54.0 prints. Fields this
// tool does not use are left out on purpose: in particular a secret row's
// "secret" field has no counterpart, so encoding/json discards it and its
// value never enters the process's data.

type lsRow struct {
	Name       string   `json:"name"`
	ID         string   `json:"id"`
	Agent      string   `json:"agent"`
	Status     string   `json:"status"`
	LastUsedAt string   `json:"last_used_at"`
	CreatedAt  string   `json:"created_at"`
	Ports      []lsPort `json:"ports"`
	Mounts     []string `json:"mounts"`
	Workspaces []string `json:"workspaces"`
}

type lsPort struct {
	HostIP      string `json:"host_ip"`
	HostPort    int    `json:"host_port"`
	SandboxPort int    `json:"sandbox_port"`
	Protocol    string `json:"protocol"`
}

type policyDoc struct {
	Rules []struct {
		ResourceType string   `json:"resource_type"`
		Decision     string   `json:"decision"`
		Resources    []string `json:"resources"`
		Origin       string   `json:"origin"`
		Layer        string   `json:"layer"`
		Status       string   `json:"status"`
		Provenance   struct {
			CreatedVia string `json:"created_via"`
		} `json:"provenance"`
	} `json:"rules"`
}

// secretDoc maps only scope, type, and name of each secret row.
type secretDoc struct {
	Secrets []struct {
		Scope string `json:"scope"`
		Type  string `json:"type"`
		Name  string `json:"name"`
	} `json:"secrets"`
}

type mcpRow struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Status    string `json:"status"`
	Type      string `json:"type"`
}

func unmarshal(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	return d.Decode(v)
}

func esc(s string) string { return model.EscapeControl(s) }

func paths(list []string) []Mount {
	var out []Mount
	for _, p := range list {
		if p != "" {
			out = append(out, Mount{Path: esc(p)})
		}
	}
	return out
}

func parseList(b []byte) ([]Sandbox, error) {
	var rows []lsRow
	if err := unmarshal(b, &rows); err != nil {
		// Releases before v0.48.0 wrapped the array in {"sandboxes": [...]}.
		var old struct {
			Sandboxes []lsRow `json:"sandboxes"`
		}
		if unmarshal(b, &old) != nil {
			return nil, err
		}
		rows = old.Sandboxes
	}
	var out []Sandbox
	for _, r := range rows {
		sb := Sandbox{Name: esc(r.Name), ID: esc(r.ID), Agent: esc(r.Agent), Status: esc(r.Status),
			Workspaces: paths(r.Workspaces), Mounts: paths(r.Mounts),
			CreatedAt: esc(r.CreatedAt), LastUsedAt: esc(r.LastUsedAt)}
		for _, p := range r.Ports {
			sb.Ports = append(sb.Ports, Port{HostIP: esc(p.HostIP), HostPort: p.HostPort, SandboxPort: p.SandboxPort, Protocol: strings.ToLower(esc(p.Protocol))})
		}
		out = append(out, sb)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseRules returns the active network rules, one per resource.
func parseRules(b []byte) ([]Rule, error) {
	var doc policyDoc
	if err := unmarshal(b, &doc); err != nil {
		return nil, err
	}
	seen := map[Rule]bool{}
	var out []Rule
	for _, r := range doc.Rules {
		typ := strings.ToLower(r.ResourceType)
		decision := strings.ToLower(r.Decision)
		if (typ != "network" && typ != "") || (decision != "allow" && decision != "deny") || strings.Contains(strings.ToLower(r.Status), "inactive") {
			continue
		}
		for _, res := range r.Resources {
			rule := Rule{Decision: decision, Resource: strings.ToLower(esc(res)), Origin: strings.ToLower(esc(r.Origin)),
				Layer: strings.ToLower(esc(r.Layer)), CreatedVia: strings.ToLower(esc(r.Provenance.CreatedVia))}
			if res != "" && !seen[rule] {
				seen[rule] = true
				out = append(out, rule)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Decision < out[j].Decision
	})
	return out, nil
}

// parseSecrets returns the names of the sandbox's stored secrets.
func parseSecrets(b []byte) ([]string, error) {
	var doc secretDoc
	if err := unmarshal(b, &doc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range doc.Secrets {
		if s.Name == "" || strings.EqualFold(s.Scope, "global") || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, esc(s.Name))
	}
	sort.Strings(out)
	return out, nil
}

func parseMCP(b []byte) ([]MCPServer, error) {
	var rows []mcpRow
	if err := unmarshal(b, &rows); err != nil {
		return nil, err
	}
	var out []MCPServer
	for _, r := range rows {
		if r.Name != "" {
			out = append(out, MCPServer{Name: esc(r.Name), Transport: esc(r.Transport), Status: esc(r.Status), Type: esc(r.Type)})
		}
	}
	return out, nil
}
