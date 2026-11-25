package sbxenv_test

import (
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

func kitEnv(t *testing.T, kits map[string]string, env string) *sbxenv.Plan {
	t.Helper()
	f := newFixture(t)
	var refs []string
	for name, body := range kits {
		f.write(t, "kits/"+name+"/"+name+".yaml", "schemaVersion: \"3\"\nkind: mixin\ncapabilities:\n"+body)
		refs = append(refs, "./kits/"+name)
	}
	f.write(t, "sbxenv.yaml", minimal+"kits: ["+strings.Join(refs, ", ")+"]\n"+env)
	return f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})
}

func boundaryFor(p *sbxenv.Plan, itemID string, kind sbxenv.BoundaryKind) bool {
	for _, b := range p.Boundaries {
		if b.ItemID == itemID && b.Kind == kind {
			return true
		}
	}
	return false
}

func TestDenyWinsOverAllowAcrossKits(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]string{
		"same kit": {"a": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: [x.example.com, ok.example.com]\n        deny: [x.example.com]\n"},
		"other kit": {
			"a": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: [x.example.com, ok.example.com]\n",
			"b": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        deny: [\"*.example.com\"]\n        allow: [other.test]\n",
		},
	}
	for name, kits := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			p := kitEnv(t, kits, "")

			// Assert
			denied := item(t, p, "kit:a/network-policy@1/runtime.allow/x.example.com")
			if boundaryFor(p, denied.ID, sbxenv.BoundaryNetwork) || has(denied.Badges, sbxenv.BadgeNet) {
				t.Errorf("a denied host must not be shown as allowed egress: %+v", denied)
			}
			if v, _ := field(denied, "denied by"); v == "" {
				t.Errorf("the item should say which deny overrides it: %+v", denied.Fields)
			}
			if !boundaryFor(p, "kit:a/network-policy@1/runtime.allow/"+map[string]string{"same kit": "ok.example.com", "other kit": "ok.example.com"}[name], sbxenv.BoundaryNetwork) && name == "same kit" {
				t.Error("an allowed host that no deny covers stays allowed")
			}
		})
	}
}

func TestEgressCountExcludesDeniedHosts(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: [a.test, b.test, c.test]\n        deny: [b.test]\n"}, "")

	// Assert
	if p.Summary.EgressAllows != 2 {
		t.Errorf("egress allows = %d, want 2", p.Summary.EgressAllows)
	}
}

func TestCredentialIntoDeniedDomainIsReported(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{
		"a": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: [api.x.test]\n  - type: com.docker.sandbox/credential@1\n    optional: true\n    config: {service: s, phase: runtime, apiKey: {inject: [{domain: api.x.test}]}}\n",
		"b": "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        deny: [api.x.test]\n",
	}, "bindings:\n  s: {apiKey: {domains: [api.x.test]}}\n")

	// Assert
	requireDiag(t, p, "CREDENTIAL_INJECT_DENIED", sbxenv.LevelWarning)
}

func TestNetworkPolicyV2IsModelled(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": `  - type: com.docker.sandbox/network-policy@2
    config:
      runtime:
        allow:
          - github.com
          - hosts: [api.github.com]
            methods: [GET, HEAD]
            paths: [/repos/**]
        deny:
          - hosts: [api.github.com]
            methods: [DELETE]
  - type: com.docker.sandbox/credential@1
    optional: true
    config: {service: gh, phase: runtime, apiKey: {inject: [{domain: api.github.com, header: Authorization}]}}
`}, "bindings:\n  gh: {apiKey: {domains: [api.github.com]}}\n")

	// Assert
	noDiag(t, p, "CREDENTIAL_INJECT_NOT_ALLOWED")
	if len(coverage(p, sbxenv.CoverageUnmodeledCap)) != 0 {
		t.Errorf("network-policy@2 should be modelled, coverage: %+v", p.Coverage)
	}
	bounded := item(t, p, "kit:a/network-policy@2/runtime.allow/api.github.com")
	if !strings.Contains(bounded.Value, "GET,HEAD") || !strings.Contains(bounded.Value, "/repos/**") {
		t.Errorf("bounded entry should show its methods and paths: %q", bounded.Value)
	}
	if !boundaryFor(p, bounded.ID, sbxenv.BoundaryNetwork) {
		t.Error("a bounded allow is still egress; a method-bounded deny does not remove it")
	}
	if p.Summary.EgressAllows != 2 {
		t.Errorf("egress allows = %d, want 2", p.Summary.EgressAllows)
	}
}

func TestNetworkPolicyV2WildcardIsUnrestrictedEgress(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/network-policy@2\n    config:\n      runtime:\n        allow: [\"*\"]\n"}, "")

	// Assert
	requireDiag(t, p, "EGRESS_UNRESTRICTED", sbxenv.LevelWarning)
}

func TestCapabilityGrammarErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, code string
	}{
		{"type beside group", "  - type: com.docker.sandbox/ssh-agent@1\n    group:\n      capabilities:\n        - type: com.docker.sandbox/git-identity@1\n", "KIT_CAP_GRAMMAR"},
		{"nested group", "  - group:\n      capabilities:\n        - group:\n            capabilities: []\n", "KIT_CAP_GRAMMAR"},
		{"two network-policy@1", "  - type: com.docker.sandbox/network-policy@1\n    config: {runtime: {allow: [a.test]}}\n  - type: com.docker.sandbox/network-policy@1\n    config: {runtime: {allow: [b.test]}}\n", "KIT_SINGLETON"},
		{"@1 with @2", "  - type: com.docker.sandbox/network-policy@1\n    config: {runtime: {allow: [a.test]}}\n  - type: com.docker.sandbox/network-policy@2\n    config: {runtime: {allow: [b.test]}}\n", "KIT_SINGLETON"},
		{"two git-identity", "  - type: com.docker.sandbox/git-identity@1\n  - type: com.docker.sandbox/git-identity@1\n", "KIT_SINGLETON"},
		{"required unknown type", "  - type: io.example/thing@1\n", "UNKNOWN_CAPABILITY_REQUIRED"},
		{"required unknown sandbox type", "  - type: com.docker.sandbox/frobnicate@1\n", "UNKNOWN_CAPABILITY_REQUIRED"},
		{"type without version", "  - type: notatype\n    optional: true\n", "KIT_CAP_GRAMMAR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			p := kitEnv(t, map[string]string{"a": tc.body}, "")

			// Assert
			requireDiag(t, p, tc.code, sbxenv.LevelError)
		})
	}
}

func TestWarningBadgeLandsOnTheItemTheDiagnosticIsAbout(t *testing.T) {
	t.Parallel()
	// Arrange / Act: the bounded entry comes first and gets the plain ID.
	p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/ssh-agent@1\n    config: {phase: runtime, unrestricted: false, sign: [git]}\n  - type: com.docker.sandbox/ssh-agent@1\n    config: {phase: runtime}\n"}, "")

	// Assert
	d := requireDiag(t, p, "SSH_AGENT_UNRESTRICTED", sbxenv.LevelWarning)
	flagged := item(t, p, d.ItemID)
	if !strings.Contains(flagged.Value, "unrestricted") {
		t.Errorf("diagnostic points at %s (%q), not the unrestricted entry", d.ItemID, flagged.Value)
	}
	for _, it := range p.Items {
		if strings.HasPrefix(it.ID, "kit:a/ssh-agent@1") && !strings.Contains(it.Value, "unrestricted") && has(it.Badges, sbxenv.BadgeWarn) {
			t.Errorf("the bounded entry %s must not carry [!]", it.ID)
		}
	}
}

func TestBoundHostMountCountsOnce(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/host-mount@1\n    config: {path: /data}\n"}, "mounts:\n  /data: ./hostdata\n")

	// Assert
	if p.Summary.RWMounts != 1 || p.Summary.HostPersistent != 1 {
		t.Errorf("one host directory counted as rw=%d persistent=%d", p.Summary.RWMounts, p.Summary.HostPersistent)
	}
}

func TestHostMountPathRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path    string
		invalid bool
	}{
		{"/data", false},
		{"${{ kit.env.HOME }}/.cache", false},
		{"/cache/..", true},
		{"/cache/.", true},
		{"/a//b", true},
		{"/a/", true},
		{"relative", true},
		{"/", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/host-mount@1\n    config: {path: \"" + tc.path + "\"}\n"}, "")

			// Assert
			if got := len(diags(p, "HOST_MOUNT_PATH_INVALID")) > 0; got != tc.invalid {
				t.Errorf("invalid = %v, want %v", got, tc.invalid)
			}
		})
	}
}

func TestHostMountPathConflicts(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{
		"a": "  - type: com.docker.sandbox/host-mount@1\n    config: {path: /data}\n",
		"b": "  - type: com.docker.sandbox/host-mount@1\n    config: {path: /data}\n  - type: com.docker.sandbox/volume@1\n    config: {path: /vol}\n  - type: com.docker.sandbox/host-mount@1\n    config: {path: /vol}\n",
	}, "")

	// Assert
	if got := len(diags(p, "HOST_MOUNT_CONFLICT")); got != 2 {
		t.Errorf("conflicts = %d, want 2 (same path in two kits; host-mount over a volume)", got)
	}
}

func TestSSHAgentConfigRules(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bounded without lists":       "{phase: runtime, unrestricted: false}",
		"unrestricted with lists":     "{phase: runtime, sign: [git]}",
		"empty list":                  "{phase: runtime, unrestricted: false, sign: []}",
		"missing phase":               "{unrestricted: false, sign: [git]}",
		"authenticate with a port":    "{phase: runtime, unrestricted: false, authenticate: [\"git@github.com:22\"]}",
		"authenticate with wildcards": "{phase: runtime, unrestricted: false, authenticate: [\"*.example.com\"]}",
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/ssh-agent@1\n    optional: true\n    config: " + cfg + "\n"}, "")

			// Assert
			requireDiag(t, p, "SSH_AGENT_INVALID", sbxenv.LevelError)
		})
	}
}

func TestCredentialPhaseAndServiceRules(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"unknown phase":   "{service: s, phase: build, apiKey: {name: A}}",
		"duplicate phase": "{service: s, phase: [runtime, runtime], apiKey: {name: A}}",
		"empty phase":     "{service: s, phase: [], apiKey: {name: A}}",
		"bad service":     "{service: S_Bad, phase: runtime, apiKey: {name: A}}",
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act
			p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/credential@1\n    optional: true\n    config: " + cfg + "\n"}, "")

			// Assert
			requireDiag(t, p, "CREDENTIAL_INVALID", sbxenv.LevelError)
			noDiag(t, p, "CREDENTIAL_CONFLICT")
		})
	}
}

func TestOptionalGroupIsSkippedWholeWhenAMemberIsUnbound(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": `  - group:
      optional: true
      capabilities:
        - type: com.docker.sandbox/credential@1
          config: {service: s, phase: runtime, apiKey: {name: TOKEN}}
        - type: com.docker.sandbox/host-mount@1
          config: {path: /g}
        - type: com.docker.sandbox/network-policy@1
          config: {runtime: {allow: ["*"]}}
`}, "")

	// Assert
	if p.Summary.RWMounts != 0 || p.Summary.EgressAllows != 0 {
		t.Errorf("a skipped group grants nothing: rw=%d egress=%d", p.Summary.RWMounts, p.Summary.EgressAllows)
	}
	noDiag(t, p, "EGRESS_UNRESTRICTED", "HOST_MOUNT_SHARED")
	for _, id := range []string{"kit:a/host-mount@1//g", "kit:a/network-policy@1/runtime.allow/*"} {
		if !has(item(t, p, id).Badges, sbxenv.BadgeSkip) {
			t.Errorf("%s should be marked [SKIP]", id)
		}
	}
}

func TestKitArgumentExportsAreShown(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/a/a.yaml", "schemaVersion: \"3\"\nkind: mixin\nargs:\n  timeout:\n    default: \"30\"\n    env: BROWSER_TIMEOUT\n  both:\n    default: x\n    env: X\n    buildArg: X\n")
	f.write(t, "sbxenv.yaml", minimal+"kits: [./kits/a]\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	if v, _ := field(item(t, p, "kits[./kits/a]"), "exports BROWSER_TIMEOUT"); v != "timeout" {
		t.Errorf("container export not shown: %+v", item(t, p, "kits[./kits/a]").Fields)
	}
	requireDiag(t, p, "KIT_ARG_DECL_INVALID", sbxenv.LevelError)
}

func TestMissingBindingIsAWarningBecauseGlobalBindingsAreInvisible(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p := kitEnv(t, map[string]string{"a": "  - type: com.docker.sandbox/credential@1\n    config: {service: s, phase: runtime, apiKey: {name: A}}\n"}, "")

	// Assert
	d := requireDiag(t, p, "CREDENTIAL_UNBOUND", sbxenv.LevelWarning)
	if !strings.Contains(d.Message, "credentials.yaml") {
		t.Errorf("message should say a global binding may already exist: %s", d.Message)
	}
}

func TestNetworkDenyCoversEntireAllowPattern(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		allow, deny string
		blocked     bool
	}{
		{"*", "any.host.invalid", false},
		{"**.example.com", "*.example.com", false},
		{"*.sub.example.com", "**.example.com", true},
		{"**", "*", true},
	} {
		t.Run(tc.allow+"/"+tc.deny, func(t *testing.T) {
			body := "  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: ['" + tc.allow + "']\n        deny: ['" + tc.deny + "']\n"
			p := kitEnv(t, map[string]string{"a": body}, "")
			if allowed := boundaryFor(p, "kit:a/network-policy@1/runtime.allow/"+tc.allow, sbxenv.BoundaryNetwork); allowed == tc.blocked {
				t.Errorf("network boundary present = %v, want %v", allowed, !tc.blocked)
			}
			if tc.allow == "*" || tc.allow == "**" {
				if tc.blocked {
					noDiag(t, p, "EGRESS_UNRESTRICTED")
				} else {
					requireDiag(t, p, "EGRESS_UNRESTRICTED", sbxenv.LevelWarning)
				}
			}
		})
	}
}
