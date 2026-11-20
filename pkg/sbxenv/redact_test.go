package sbxenv

import (
	"errors"
	"strings"
	"testing"
)

// pseudoSecrets are planted in every place a value can enter the plan. None
// of them may appear in strictly redacted output.
var pseudoSecrets = []string{
	"ghp_PSEUDOSECRET0001", // env value
	"sk-PSEUDOSECRET0002",  // literal secret value
	"PSEUDOSECRET0003",     // secret command
	"PseudoVault0004",      // op:// item name
	"PSEUDOSECRET0005",     // --env-arg value
	"PSEUDOSECRET0006",     // args file value
	"PSEUDOSECRET0007",     // lifecycle command
	"PSEUDOSECRET0008",     // mcp command argument
	"PSEUDOSECRET0009",     // mcp url query
	"PSEUDOSECRET0010",     // kit arg value used in a hook
	"PSEUDOSECRET0011",     // registry secret command
	"PSEUDOSECRET0012",     // entrypoint
	"PSEUDOSECRET0013",     // unknown capability config
	"PSEUDOSECRET0014",     // --kit-arg value
	"PSEUDOSECRET0015",     // arg default
}

const secretEnv = `schemaVersion: "1"
agent: claude
workspace: .
entrypoint: [/bin/run, --token=PSEUDOSECRET0012]
args:
  token:
    default: PSEUDOSECRET0015
  flagged:
    default: x
    enum: [x, y]
  filed:
    default: z
env:
  GH: ghp_PSEUDOSECRET0001
  FROM_ARG: ${{ env.args.token }}
  FROM_FLAG: ${{ env.args.flagged }}
  FROM_FILE: ${{ env.args.filed }}
secrets:
  literal:
    value: sk-PSEUDOSECRET0002
  cmd:
    command: vault read PSEUDOSECRET0003
  vault:
    ref: op://Private/PseudoVault0004/credential
registries:
  ghcr.io:
    secret:
      command: print PSEUDOSECRET0011
mcp:
  servers:
    - name: local
      command: server
      args: [--key, PSEUDOSECRET0008]
    - name: remote
      url: https://mcp.example.com/sse?token=PSEUDOSECRET0009
lifecycle:
  postCreate:
    - command: ./seed.sh --session PSEUDOSECRET0007
kits:
  - source: ./kits/k
    args:
      key: PSEUDOSECRET0010
`

const secretKit = `schemaVersion: "3"
kind: mixin
args:
  key:
    default: none
  other:
    default: none
capabilities:
  - type: com.docker.sandbox/lifecycle@1
    config:
      install:
        - command: echo ${{ kit.args.key }} ${{ kit.args.other }}
  - type: io.example/custom@1
    optional: true
    config:
      apiKey: PSEUDOSECRET0013
`

func secretPlan(t *testing.T, mode Redaction) *Plan {
	t.Helper()
	return resolve(t, ResolveOptions{
		Redaction:    mode,
		Kits:         memKits{testProj + "/kits/k": secretKit},
		EnvArgs:      []string{"flagged=PSEUDOSECRET0005"},
		EnvArgsFiles: []ArgFile{{Path: testProj + "/s.args", Content: []byte("filed=PSEUDOSECRET0006\n")}},
		KitArgs:      []string{"other=PSEUDOSECRET0014"},
	}, layer(testProj+"/sbxenv.yaml", secretEnv))
}

func TestStrictRedactionHidesPseudoSecrets(t *testing.T) {
	p := secretPlan(t, RedactStrict)
	out := mustJSON(t, p)
	for _, s := range pseudoSecrets {
		if strings.Contains(out, s) {
			t.Errorf("strict plan leaks %q", s)
		}
	}
	if strings.Contains(out, testHome) {
		t.Errorf("strict plan leaks the home directory")
	}
	// Diagnostics are part of the plan and were checked above; make sure
	// some were produced from the planted values (enum violation, literal).
	requireDiag(t, p, "ENV_ARG_INVALID", LevelError)
	requireDiag(t, p, "SECRET_LITERAL", LevelWarning)
	if v := item(t, p, "secrets.vault").Value; v != "ref: op://<personal vault>/…" {
		t.Errorf("vault ref = %q", v)
	}
	if v := item(t, p, "mcp.servers[remote]").Value; v != "url: https://mcp.example.com/…" {
		t.Errorf("mcp url = %q", v)
	}
}

func TestHomeRedactionKeepsValuesButNeverLiteralSecrets(t *testing.T) {
	p := secretPlan(t, RedactHome)
	out := mustJSON(t, p)
	if strings.Contains(out, "sk-PSEUDOSECRET0002") {
		t.Error("literal secret values must never be copied, even without --redact")
	}
	if strings.Contains(out, testHome+"/") {
		t.Error("$HOME must be shortened to ~ in every mode")
	}
	if v := item(t, p, "env.GH").Value; v != "ghp_PSEUDOSECRET0001" {
		t.Errorf("local TTY mode shows env values: %q", v)
	}
	if !strings.Contains(out, "~/proj/sbxenv.yaml") {
		t.Error("source path should be shown as ~/proj/sbxenv.yaml")
	}
}

func TestParseErrorsDoNotQuoteContent(t *testing.T) {
	bad := "schemaVersion: \"1\"\nenv:\n  TOKEN: \"PSEUDOSECRET0099\n  other: [\n"
	_, err := Resolve([]LayerInput{layer(testProj+"/a.yaml", bad)}, ResolveOptions{HomeDir: testHome})
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), "PSEUDOSECRET0099") || strings.Contains(err.Error(), testHome) {
		t.Errorf("parse error leaks content: %v", err)
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Errorf("want ParseError, got %T", err)
	}
	bad2 := "a:\n\tb: PSEUDOSECRET0098\n"
	if _, err := Resolve([]LayerInput{layer(testProj+"/a.yaml", bad2)}, ResolveOptions{HomeDir: testHome}); err == nil || strings.Contains(err.Error(), "PSEUDOSECRET0098") {
		t.Errorf("tab error leaks content: %v", err)
	}
}

func TestRedactorPaths(t *testing.T) {
	r := newRedactor(RedactHome, "/home/al")
	if got := r.path("/home/al/x and /home/alice/y and /home/al"); got != "~/x and /home/alice/y and ~" {
		t.Errorf("path = %q", got)
	}
	s := newRedactor(RedactStrict, "")
	for in, want := range map[string]string{
		"op://Private/x/y":    "op://<personal vault>/…",
		"op://Team Vault/x/y": "op://<shared vault>/…",
		"awssm://prod/db":     "awssm://<redacted>",
		"no scheme":           "<redacted reference>",
		"WEIRD SCHEME!://x":   "<redacted reference>",
	} {
		if got := s.secretRef(in); got != want {
			t.Errorf("secretRef(%q) = %q, want %q", in, got, want)
		}
	}
}
