package sbxenv_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// redactionTemplate places the same value in every position a user value
// can enter an environment file. VALUE is replaced by a YAML double-quoted
// scalar, so any string survives the round trip.
const redactionTemplate = `schemaVersion: "1"
name: ${{ env.args.b }}
agent: claude
workspace: ./${{ env.args.b }}
entrypoint: [/bin/run, VALUE]
args:
  a:
    default: VALUE
  b:
    default: x
env:
  E: VALUE
  F: ${{ env.args.a }}
  G: ${{ env.args.b }}
sandboxOptions:
  template: ghcr.io/example/${{ env.args.b }}:1
additionalWorkspaces:
  - path: ../shared
    target: /w/${{ env.args.b }}
ports:
  - sandbox: 80
    hostIP: ${{ env.args.b }}
secrets:
  lit:
    value: VALUE
  scalar: VALUE
  cmd:
    command: VALUE
lifecycle:
  initialize:
    - command: VALUE
mcp:
  servers:
    - name: ${{ env.args.b }}
      command: srv
      args: [VALUE]
    - name: remote
      url: https://user:${{ env.args.b }}@mcp.example.com/sse
kits:
  - source: ./kit
    args:
      host: ${{ env.args.b }}
  - git+https://user:${{ env.args.b }}@github.com/org/repo.git#ref=abc
`

// kitDescriptor routes its argument into every structural field a kit has.
const kitDescriptor = `schemaVersion: "3"
kind: mixin
args:
  host:
    default: none
capabilities:
  - type: com.docker.sandbox/network-policy@1
    config:
      runtime:
        allow: ["${{ kit.args.host }}"]
  - type: com.docker.sandbox/network-policy@2
    config:
      runtime:
        allow:
          - hosts: [bounded.example.com]
            methods: ["${{ kit.args.host }}"]
            paths: ["/${{ kit.args.host }}/**"]
  - type: com.docker.sandbox/credential@1
    optional: true
    config:
      service: s
      phase: runtime
      apiKey:
        name: TOKEN
        inject: [{domain: "${{ kit.args.host }}", header: Authorization}]
  - type: com.docker.sandbox/host-mount@1
    config: {path: "/data/${{ kit.args.host }}"}
  - type: com.docker.sandbox/ssh-agent@1
    config: {phase: runtime, unrestricted: false, sign: ["${{ kit.args.host }}"]}
  - type: com.docker.sandbox/lifecycle@1
    config:
      files:
        - path: "/etc/${{ kit.args.host }}"
`

type kitDir string

func (k kitDir) LoadKit(dir string) (sbxenv.KitInput, error) {
	return sbxenv.KitInput{DescriptorPath: dir + "/kit.yaml", Content: []byte(k)}, nil
}

// referenceOnly matches a value that is nothing but a reference name such
// as ${{ env.args.x }}. Names are structure and may be shown.
var referenceOnly = regexp.MustCompile(`^\$\{\{\s*(env|kit)(\.[A-Za-z_][A-Za-z0-9_-]*)+\s*\}\}$`)

func strictJSON(t *testing.T, value string) (string, bool) {
	quoted, _ := json.Marshal(value)
	doc := strings.ReplaceAll(redactionTemplate, "VALUE", string(quoted))
	p, err := sbxenv.Resolve([]sbxenv.LayerInput{{Path: "/w/sbxenv.yaml", Content: []byte(doc), Kind: sbxenv.LayerEnv}},
		sbxenv.ResolveOptions{Redaction: sbxenv.RedactStrict, EnvArgs: []string{"b=" + value}, Kits: kitDir(kitDescriptor)})
	if err != nil {
		return err.Error(), false
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), true
}

func FuzzStrictRedactionNeverEchoesValues(f *testing.F) {
	for _, seed := range []string{"hunter2-secret", "ghp_0123456789abcdef", "op://Private/x/y", "multi\nline value", `quote"and\backslash`, "日本語の秘密", "${{ env.args.a }}", "  padded  ", "a: b"} {
		f.Add(seed)
	}
	baseline, _ := strictJSON(&testing.T{}, "PLACEHOLDER")
	f.Fuzz(func(t *testing.T, value string) {
		// Only values that could not appear for another reason are judged.
		if len(value) < 6 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r") || strings.TrimSpace(value) != value {
			t.Skip()
		}
		escaped, _ := json.Marshal(value)
		inner := string(escaped[1 : len(escaped)-1])
		if strings.Contains(baseline, value) || strings.Contains(baseline, inner) || referenceOnly.MatchString(value) {
			t.Skip()
		}

		// Act
		out, ok := strictJSON(t, value)

		// Assert
		if strings.Contains(out, value) || strings.Contains(out, inner) {
			where := "plan"
			if !ok {
				where = "error"
			}
			t.Fatalf("strict %s echoes the planted value %q", where, value)
		}
	})
}
