package sbxenv_test

import (
	"testing"

	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

const argsEnv = `schemaVersion: "1"
agent: claude
args:
  ch:
    default: d
env:
  CH: ${{ env.args.ch }}
`

func TestEnvArgPrecedence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		files map[string]string
		order []string
		flags []string
		want  string
	}{
		{name: "default only", want: "d"},
		{name: "args file beats default", files: map[string]string{"1.args": "ch=file1\n"}, order: []string{"1.args"}, want: "file1"},
		{name: "later args file wins", files: map[string]string{"1.args": "ch=file1\n", "2.args": "# c\n\nch=file2\n"}, order: []string{"1.args", "2.args"}, want: "file2"},
		{name: "flag beats every file", files: map[string]string{"1.args": "ch=file1\n"}, order: []string{"1.args"}, flags: []string{"ch=flag"}, want: "flag"},
		{name: "value may contain =", flags: []string{"ch=a=b=c"}, want: "a=b=c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", argsEnv)
			for name, c := range tc.files {
				f.write(t, name, c)
			}

			// Act
			p := f.resolve(t, run{paths: []string{"."}, envArgsFiles: tc.order,
				opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome, EnvArgs: tc.flags}})

			// Assert
			if got := item(t, p, "env.CH").Value; got != tc.want {
				t.Errorf("env.CH = %q, want %q", got, tc.want)
			}
			if got := item(t, p, "args.ch").Value; got != tc.want {
				t.Errorf("args.ch = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpressionsExpandOnlyTheDocumentedForms(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	shared := t.TempDir() // a second directory, outside $HOME
	envPath := writeFile(t, shared, "env.yaml", argsEnv+`  DIR: ${{ env.fileDir }}
  PROJ: ${{ env.projectDir }}/src
  ESCAPED: $${{ env.args.ch }}
  SHELL: $PATH:/opt
  BRACES: ${HOME}
`)

	// Act
	p := f.resolve(t, run{paths: []string{envPath}, opts: sbxenv.ResolveOptions{Redaction: sbxenv.RedactHome}})

	// Assert
	want := map[string]string{
		"env.DIR":     shared,
		"env.PROJ":    shared + "/src",
		"env.ESCAPED": "${{ env.args.ch }}",
		"env.SHELL":   "$PATH:/opt",
		"env.BRACES":  "${HOME}",
	}
	for id, w := range want {
		if got := item(t, p, id).Value; got != w {
			t.Errorf("%s = %q, want %q", id, got, w)
		}
	}
	noDiag(t, p, "UNRESOLVED_EXPR")
}

func TestUnresolvableExpressionsAreErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"undeclared argument": "  X: ${{ env.args.nope }}\n",
		"unknown namespace":   "  X: ${{ env.home }}\n",
		"required not given":  "  X: ${{ env.args.req }}\n",
	}
	for name, envLine := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", minimal+"args:\n  req:\n    required: true\nenv:\n"+envLine)

			// Act
			p := f.resolve(t, run{paths: []string{"."}})

			// Assert
			d := requireDiag(t, p, "UNRESOLVED_EXPR", sbxenv.LevelError)
			if len(d.Origins) == 0 || d.Origins[0].Line != 7 {
				t.Errorf("diagnostic should point at line 7 (the env value): %+v", d.Origins)
			}
		})
	}
}

func TestArgumentValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		decl  string
		flags []string
		code  string
	}{
		{"enum violation", "  a:\n    default: x\n    enum: [x, y]\n", []string{"a=z"}, "ENV_ARG_INVALID"},
		{"pattern violation", "  a:\n    default: \"1\"\n    pattern: \"[0-9]+\"\n", []string{"a=1x"}, "ENV_ARG_INVALID"},
		{"required missing", "  a:\n    required: true\n", nil, "ENV_ARG_REQUIRED"},
		{"undeclared flag", "  a:\n    default: x\n", []string{"b=1"}, "ENV_ARG_UNDECLARED"},
		{"default and required", "  a:\n    default: x\n    required: true\n", nil, "ARG_DECL_INVALID"},
		{"enum and pattern", "  a:\n    default: x\n    enum: [x]\n    pattern: x\n", nil, "ARG_DECL_INVALID"},
		{"malformed flag", "  a:\n    default: x\n", []string{"novalue"}, "ENV_ARG_SYNTAX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			f := newFixture(t)
			f.write(t, "sbxenv.yaml", minimal+"args:\n"+tc.decl)

			// Act
			p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{EnvArgs: tc.flags}})

			// Assert
			requireDiag(t, p, tc.code, sbxenv.LevelError)
		})
	}
}

func TestEnvArgOverriddenByLaterFixedValueIsReportedIneffective(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "a.yaml", argsEnv)
	f.write(t, "b.yaml", "env:\n  CH: fixed\n")

	// Act
	p := f.resolve(t, run{paths: []string{"a.yaml", "b.yaml"}, opts: sbxenv.ResolveOptions{EnvArgs: []string{"ch=mine"}}})

	// Assert
	d := requireDiag(t, p, "ENV_ARG_INEFFECTIVE", sbxenv.LevelWarning)
	var lines []int
	for _, o := range d.Origins {
		if o.SourceID == "L2" {
			lines = append(lines, o.Line)
		}
	}
	if len(lines) == 0 || lines[0] != 2 {
		t.Errorf("should point at the overriding line b.yaml:2, got %+v", d.Origins)
	}
}

func TestEnvArgThatStillTakesEffectIsNotReported(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", argsEnv)

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{EnvArgs: []string{"ch=mine"}}})

	// Assert
	noDiag(t, p, "ENV_ARG_INEFFECTIVE", "ENV_ARG_UNUSED")
}

func TestDeclaredButUnreferencedEnvArgIsInfo(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"args:\n  a:\n    default: x\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}, opts: sbxenv.ResolveOptions{EnvArgs: []string{"a=y"}}})

	// Assert
	requireDiag(t, p, "ENV_ARG_UNUSED", sbxenv.LevelInfo)
}

func TestExpressionsInKeysAndArgsBlockAreRejected(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFixture(t)
	f.write(t, "sbxenv.yaml", minimal+"args:\n  a:\n    default: ${{ env.projectDir }}\nenv:\n  ${{ env.args.a }}: x\n")

	// Act
	p := f.resolve(t, run{paths: []string{"."}})

	// Assert
	requireDiag(t, p, "EXPR_IN_ARGS", sbxenv.LevelError)
	requireDiag(t, p, "EXPR_IN_KEY", sbxenv.LevelError)
}
