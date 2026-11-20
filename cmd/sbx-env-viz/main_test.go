package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T, files map[string]string) (string, env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	for name, c := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	e := env{
		stdin: strings.NewReader(""), stdout: &out, stderr: &errb,
		getenv: func(string) string { return "" },
		getwd:  func() (string, error) { return dir, nil },
		home:   func() (string, error) { return dir, nil },
		isTTY:  func() bool { return false },
	}
	return dir, e, &out, &errb
}

const okEnv = "schemaVersion: \"1\"\nagent: claude\nenv:\n  A: b\n"
const warnEnv = "schemaVersion: \"1\"\nagent: claude\nports:\n  - sandbox: 1\n    hostIP: 0.0.0.0\n"
const errEnv = "schemaVersion: \"1\"\nagent: claude\nbogus: 1\n"

func TestExitCodes(t *testing.T) {
	cases := []struct {
		file string
		args []string
		want int
	}{
		{okEnv, []string{"--text"}, 0},
		{okEnv, []string{"--fail-on=warning"}, 1}, // host-local-policy coverage is not a warning; but no warnings → check below
		{warnEnv, []string{"--fail-on", "warning"}, 1},
		{warnEnv, []string{"--fail-on", "error"}, 0},
		{errEnv, []string{"--fail-on=error"}, 1},
		{errEnv, []string{"--fail-on=none"}, 0},
		{okEnv, []string{"--fail-on=sometimes"}, 2},
		{okEnv, []string{"--nope"}, 2},
		{"a: 1\n---\nb: 2\n", nil, 2},
	}
	for i, tc := range cases {
		_, e, _, errb := setup(t, map[string]string{"sbxenv.yaml": tc.file})
		got := run(append([]string{"sbxenv.yaml"}, tc.args...), e)
		want := tc.want
		if i == 1 {
			want = 0 // okEnv has no warnings
		}
		if got != want {
			t.Errorf("case %d %v: exit %d, want %d (stderr %q)", i, tc.args, got, want, errb.String())
		}
	}
}

func TestJSONModeAndFlags(t *testing.T) {
	_, e, out, _ := setup(t, map[string]string{
		"sbxenv.yaml": "schemaVersion: \"1\"\nagent: claude\nargs:\n  x:\n    default: a\nenv:\n  X: ${{ env.args.x }}\n",
		"x.args":      "x=from-file\n",
	})
	code := run([]string{"--json", "--env-args-file", "x.args", "--env-arg=x=SECRETFLAG", "--name", "n1", "--clone=false", "--skip-host-commands", "."}, e)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "SECRETFLAG") || strings.Contains(out.String(), "from-file") {
		t.Error("--json must redact argument values")
	}
	inv := v["invocation"].(map[string]any)
	if inv["name"] != "n1" || inv["clone"] != false || inv["skipHostCommands"] != true {
		t.Errorf("invocation = %v", inv)
	}
}

func TestErrorsAreRedacted(t *testing.T) {
	dir, e, _, errb := setup(t, map[string]string{"sbxenv.yaml": "env:\n  T: \"TOPSECRET\n"})
	if code := run(nil, e); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(errb.String(), "TOPSECRET") || strings.Contains(errb.String(), dir) {
		t.Errorf("stderr leaks content or home: %q", errb.String())
	}
}

func TestHelpVersionAndConflicts(t *testing.T) {
	_, e, out, _ := setup(t, nil)
	if run([]string{"--help"}, e) != 0 || !strings.Contains(out.String(), "Usage") {
		t.Error("--help")
	}
	out.Reset()
	if run([]string{"--version"}, e) != 0 || !strings.Contains(out.String(), "sbx-env-viz") {
		t.Error("--version")
	}
	if run([]string{"--json", "--text"}, e) != 2 {
		t.Error("--json with --text should be a usage error")
	}
	if run(nil, e) != 2 {
		t.Error("no environment file should be a usage error")
	}
}

func TestParseArgsInterleaved(t *testing.T) {
	o, err := parseArgs([]string{"a.yaml", "--env-arg", "x=1", "b.yaml", "--kit-arg=k.y=2", "--clone", "--cloud", "--ttl", "2h", "--on-timeout=delete", "--", "--weird.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(o.paths, ",") != "a.yaml,b.yaml,--weird.yaml" || o.envArgs[0] != "x=1" || o.kitArgs[0] != "k.y=2" ||
		o.clone == nil || !*o.clone || !o.cloud || o.ttl != "2h" || o.onTimeout != "delete" {
		t.Errorf("parsed = %+v", o)
	}
}
