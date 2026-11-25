package sbxenv_test

import (
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// Each pseudo secret is planted in one place a value can enter the plan.
var pseudoSecrets = map[string]string{
	"ghp_PSEUDOSECRET0001": "env value",
	"sk-PSEUDOSECRET0002":  "literal secret value",
	"PSEUDOSECRET0003":     "secret command",
	"PseudoVault0004":      "op:// item name",
	"PSEUDOSECRET0005":     "--env-arg value",
	"PSEUDOSECRET0006":     "--env-args-file value",
	"PSEUDOSECRET0007":     "lifecycle command",
	"PSEUDOSECRET0008":     "mcp command argument",
	"PSEUDOSECRET0009":     "mcp url query",
	"PSEUDOSECRET0010":     "kit entry arg used in a hook",
	"PSEUDOSECRET0011":     "registry secret command",
	"PSEUDOSECRET0012":     "entrypoint",
	"PSEUDOSECRET0013":     "unknown capability config",
	"PSEUDOSECRET0014":     "--kit-arg value",
	"PSEUDOSECRET0015":     "arg default",
	"PSEUDOSECRET0016":     "--kit-args-file value",
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
  third:
    default: none
capabilities:
  - type: com.docker.sandbox/lifecycle@1
    config:
      install:
        - command: echo ${{ kit.args.key }} ${{ kit.args.other }} ${{ kit.args.third }}
  - type: io.example/custom@1
    optional: true
    config:
      apiKey: PSEUDOSECRET0013
`

func secretPlan(t *testing.T, mode sbxenv.Redaction) (*sbxenv.Plan, fixture) {
	t.Helper()
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", secretEnv)
	f.write(t, "kits/k/k.yaml", secretKit)
	f.write(t, "s.args", "filed=PSEUDOSECRET0006\n")
	f.write(t, "k.args", "third=PSEUDOSECRET0016\n")
	p := f.resolve(t, run{paths: []string{"."}, envArgsFiles: []string{"s.args"}, kitArgsFiles: []string{"k.args"},
		opts: sbxenv.ResolveOptions{Redaction: mode, EnvArgs: []string{"flagged=PSEUDOSECRET0005"}, KitArgs: []string{"other=PSEUDOSECRET0014"}}})
	return p, f
}

func TestStrictRedactionNeverExposesPseudoSecrets(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p, f := secretPlan(t, sbxenv.RedactStrict)
	out := toJSON(t, p)

	// Assert
	for secret, where := range pseudoSecrets {
		if strings.Contains(out, secret) {
			t.Errorf("strict plan leaks the %s (%s)", where, secret)
		}
	}
	if strings.Contains(out, f.home) {
		t.Error("strict plan leaks the home directory")
	}
	// The planted values did reach the analysis: diagnostics fired on them.
	requireDiag(t, p, "ENV_ARG_INVALID", sbxenv.LevelError)
	requireDiag(t, p, "SECRET_LITERAL", sbxenv.LevelWarning)
}

func TestStrictRedactionKeepsWhatAReviewerNeeds(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p, _ := secretPlan(t, sbxenv.RedactStrict)

	// Assert
	want := map[string]string{
		"secrets.vault":       "ref: op://<personal vault>/…",
		"secrets.literal":     "value: <literal value hidden>",
		"secrets.cmd":         "command: <redacted command>",
		"mcp.servers[remote]": "url: https://mcp.example.com/…",
		"env.GH":              "<redacted>",
		"workspace":           "~/proj",
	}
	for id, w := range want {
		if got := item(t, p, id).Value; got != w {
			t.Errorf("%s = %q, want %q", id, got, w)
		}
	}
	if p.Meta.Redaction != sbxenv.RedactStrict {
		t.Errorf("meta.redaction = %q", p.Meta.Redaction)
	}
}

func TestVaultKindsAreClassified(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"op://Private/x/y":    "ref: op://<personal vault>/…",
		"op://Team Vault/x/y": "ref: op://<shared vault>/…",
		"awssm://prod/db":     "ref: awssm://<redacted>",
		"no-scheme-ref":       "ref: <redacted reference>",
	}
	for ref, want := range cases {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", minimal+"secrets:\n  s:\n    ref: \""+ref+"\"\n")

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			if got := item(t, p, "secrets.s").Value; got != want {
				t.Errorf("value = %q, want %q", got, want)
			}
		})
	}
}

func TestHomeOnlyRedactionShowsValuesButNeverLiteralSecrets(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	p, f := secretPlan(t, sbxenv.RedactHome)
	out := toJSON(t, p)

	// Assert
	if strings.Contains(out, "sk-PSEUDOSECRET0002") {
		t.Error("literal secret values must never be copied, in any mode")
	}
	if strings.Contains(out, f.home+"/") {
		t.Error("$HOME must be shortened to ~ in every mode")
	}
	if got := item(t, p, "env.GH").Value; got != "ghp_PSEUDOSECRET0001" {
		t.Errorf("a local TTY shows env values: %q", got)
	}
}

func TestHomeShorteningRespectsPathBoundaries(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	sibling := f.home + "-other/data" // shares the prefix but is a different directory
	f.write(t, "sbxenv.yaml", minimal+"env:\n  A: "+f.home+"/x\n  B: "+sibling+"\n  C: "+f.home+"\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	for id, want := range map[string]string{"env.A": "~/x", "env.B": sibling, "env.C": "~"} {
		if got := item(t, p, id).Value; got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestParseErrorsNeverQuoteContent(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"unterminated quote": "schemaVersion: \"1\"\nenv:\n  TOKEN: \"PSEUDOSECRET0099\n  other: [\n",
		"tab indentation":    "a:\n\tb: PSEUDOSECRET0098\n",
		"bad mapping":        "a: b: PSEUDOSECRET0097\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", content)

			// Act
			_, err := f.resolveErr(t, run{paths: []string{"."}})

			// Assert
			if err == nil {
				t.Fatal("expected a parse error")
			}
			if strings.Contains(err.Error(), "PSEUDOSECRET") || strings.Contains(err.Error(), f.home) {
				t.Errorf("error leaks content or home: %v", err)
			}
		})
	}
}

func TestHomeIsNotReplacedInsideAnotherPath(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"env:\n  A: /private"+f.home+"/x\n  B: PATH="+f.home+"/bin\n  C: \""+f.home+"/a:"+f.home+"/b\"\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	want := map[string]string{"env.A": "/private" + f.home + "/x", "env.B": "PATH=~/bin", "env.C": "~/a:~/b"}
	for id, w := range want {
		if got := item(t, p, id).Value; got != w {
			t.Errorf("%s = %q, want %q", id, got, w)
		}
	}
}

func TestCredentialsAndScalarSecretsAreHiddenInEveryMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []sbxenv.Redaction{sbxenv.RedactHome, sbxenv.RedactStrict} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			// The credential-shaped URLs are assembled at run time so the
			// repository's own secret scan does not flag this fixture.
			cred := func(n string) string { return "user" + ":" + "PSEUDOSECRET" + n + "@" }
			f.write(t, "sbxenv.yaml", minimal+"secrets:\n  scalar: PSEUDOSECRET0101\n"+
				"sandboxOptions:\n  template: "+cred("0102")+"ghcr.io/example/base:1\n"+
				"mcp:\n  servers:\n    - name: remote\n      url: https://"+cred("0103")+"mcp.example.com/sse\n"+
				"kits:\n  - git+https://"+cred("0104")+"github.com/org/repo.git#ref=abc\n")

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{Redaction: mode}})
			out := toJSON(t, p)

			// Assert
			for _, secret := range []string{"PSEUDOSECRET0101", "PSEUDOSECRET0102", "PSEUDOSECRET0103", "PSEUDOSECRET0104"} {
				if strings.Contains(out, secret) {
					t.Errorf("%s mode leaks %s", mode, secret)
				}
			}
			if got := item(t, p, "secrets.scalar").Value; got != "<literal value hidden>" {
				t.Errorf("scalar secret = %q", got)
			}
			if !hasItem(p, "kits[git+https://<credentials>@github.com/org/repo.git#ref=abc]") {
				t.Error("the kit stays identifiable without its credentials")
			}
		})
	}
}

func TestStrictHidesArgumentValuesWhereverTheyFlow(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "kits/k/k.yaml", "schemaVersion: \"3\"\nkind: mixin\nargs:\n  host:\n    default: none\ncapabilities:\n  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow: [\"${{ kit.args.host }}.example.com\"]\n")
	f.write(t, "sbxenv.yaml", minimal+"args:\n  n:\n    default: x\nname: ${{ env.args.n }}\nworkspace: ./${{ env.args.n }}\nkits:\n  - source: ./kits/k\n    args:\n      host: ${{ env.args.n }}\n")

	// Act
	strict := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{EnvArgs: []string{"n=PSEUDOSECRET0105"}}})
	local := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{EnvArgs: []string{"n=PSEUDOSECRET0105"}, Redaction: sbxenv.RedactHome}})

	// Assert
	if strings.Contains(toJSON(t, strict), "PSEUDOSECRET0105") {
		t.Error("strict output leaks an argument value through name, workspace, or a kit")
	}
	if got := item(t, strict, "workspace").Value; got != "~/proj/<redacted>" {
		t.Errorf("strict workspace = %q, want the structure with the value hidden", got)
	}
	if !hasItem(strict, "kit:k/network-policy@1/runtime.allow/<redacted>.example.com") {
		t.Error("strict egress should keep its shape with the value hidden")
	}
	if !hasItem(local, "kit:k/network-policy@1/runtime.allow/PSEUDOSECRET0105.example.com") {
		t.Error("a local view shows the real resolved domain")
	}
}
