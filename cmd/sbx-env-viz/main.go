// Command sbx-env-viz visualizes layered Docker Sandboxes environment files.
//
// It accepts the same paths and flags as `sbx env create`, resolves them
// offline, and shows the result as a TUI (TTY), text (--text, or non-TTY), or
// JSON (--json).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/loader"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/text"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// version is set with -ldflags "-X main.version=..." at release time.
var version = "dev"

// Exit codes.
const (
	exitOK        = 0
	exitThreshold = 1
	exitUsage     = 2
)

func main() {
	os.Exit(run(os.Args[1:], env{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		getenv: os.Getenv, getwd: os.Getwd, home: os.UserHomeDir,
		isTTY: func() bool {
			return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
		},
	}))
}

type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
	getwd          func() (string, error)
	home           func() (string, error)
	isTTY          func() bool
}

type options struct {
	paths        []string
	envArgs      []string
	envArgsFiles []string
	kitArgs      []string
	kitArgsFiles []string
	name         string
	clone        *bool
	skipHost     bool
	cloud        bool
	ttl          string
	onTimeout    string
	text, json   bool
	redact       bool
	failOn       string
	help         bool
	version      bool
}

const usage = `Usage: sbx-env-viz [PATH...] [flags]

Statically resolves sbx environment files (sbxenv.yaml) and local kits and
shows the effective configuration, provenance, boundaries, and diagnostics.
Paths and flags mirror "sbx env create". A directory means <dir>/sbxenv.yaml;
with no path, ./sbxenv.yaml is merged over ~/.sbxenv.yaml.

Output (default: TUI when stdin and stdout are terminals and TERM != dumb,
otherwise text):
  --text                  plain text summary
  --json                  schemaVersion "1" JSON IR (strictly redacted)
  --redact                strict redaction for TUI / text too
  --fail-on LEVEL         none (default) | warning | error; exit 1 when reached

sbx env flags:
  --env-arg NAME=VALUE    environment argument (repeatable)
  --env-args-file FILE    NAME=VALUE lines (repeatable; later files win)
  --kit-arg [KIT.]NAME=VALUE  kit argument override (repeatable)
  --kit-args-file FILE    kit argument lines (repeatable)
  --name NAME             sandbox name override
  --clone[=true|false]    override workspace.clone
  --skip-host-commands    treat lifecycle and local MCP commands as skipped
  --cloud                 analyse for sbx --cloud
  --ttl DURATION          cloud expiration (with --cloud)
  --on-timeout ACTION     cloud expiration action (with --cloud)

  --version, -h/--help

Exit codes: 0 ok, 1 --fail-on threshold reached, 2 usage or parse error.
`

var valueFlags = map[string]bool{
	"env-arg": true, "env-args-file": true, "kit-arg": true, "kit-args-file": true,
	"name": true, "ttl": true, "on-timeout": true, "fail-on": true,
}

func parseArgs(args []string) (*options, error) {
	o := &options{failOn: "none"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.paths = append(o.paths, args[i+1:]...)
			break
		}
		if a == "-h" || a == "--help" {
			o.help = true
			continue
		}
		if !strings.HasPrefix(a, "--") || a == "-" {
			if strings.HasPrefix(a, "-") && a != "-" {
				return nil, fmt.Errorf("unknown flag %s", a)
			}
			o.paths = append(o.paths, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		if valueFlags[name] && !hasVal {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			val = args[i]
		}
		boolVal := func() (bool, error) {
			if !hasVal {
				return true, nil
			}
			switch strings.ToLower(val) {
			case "true", "1":
				return true, nil
			case "false", "0":
				return false, nil
			}
			return false, fmt.Errorf("flag --%s expects true or false", name)
		}
		var err error
		switch name {
		case "env-arg":
			o.envArgs = append(o.envArgs, val)
		case "env-args-file":
			o.envArgsFiles = append(o.envArgsFiles, val)
		case "kit-arg":
			o.kitArgs = append(o.kitArgs, val)
		case "kit-args-file":
			o.kitArgsFiles = append(o.kitArgsFiles, val)
		case "name":
			o.name = val
		case "ttl":
			o.ttl = val
		case "on-timeout":
			o.onTimeout = val
		case "fail-on":
			o.failOn = val
		case "clone":
			var b bool
			b, err = boolVal()
			o.clone = &b
		case "skip-host-commands":
			o.skipHost, err = boolVal()
		case "cloud":
			o.cloud, err = boolVal()
		case "text":
			o.text, err = boolVal()
		case "json":
			o.json, err = boolVal()
		case "redact":
			o.redact, err = boolVal()
		case "version":
			o.version = true
		default:
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if err != nil {
			return nil, err
		}
	}
	switch o.failOn {
	case "none", "warning", "error":
	default:
		return nil, fmt.Errorf("--fail-on must be none, warning, or error")
	}
	if o.text && o.json {
		return nil, errors.New("--text and --json cannot be combined")
	}
	return o, nil
}

func run(args []string, e env) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(e.stderr, "sbx-env-viz: %v\n\n%s", err, usage)
		return exitUsage
	}
	if o.help {
		fmt.Fprint(e.stdout, usage)
		return exitOK
	}
	if o.version {
		fmt.Fprintf(e.stdout, "sbx-env-viz %s (IR schema %s, targets sbx %s)\n", version, sbxenv.SchemaVersion, sbxenv.TargetSbxVersion)
		return exitOK
	}
	wd, err := e.getwd()
	if err != nil {
		fmt.Fprintf(e.stderr, "sbx-env-viz: cannot determine the working directory\n")
		return exitUsage
	}
	home, _ := e.home()
	var homeAliases []string
	if home != "" {
		if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
			homeAliases = append(homeAliases, resolved)
		}
	}
	shorten := func(s string) string { return sbxenv.ShortenHome(s, home, homeAliases...) }
	res, err := loader.Load(loader.Request{Paths: o.paths, EnvArgsFiles: o.envArgsFiles, KitArgsFiles: o.kitArgsFiles, WorkDir: wd, HomeDir: home})
	if err != nil {
		fmt.Fprintf(e.stderr, "sbx-env-viz: %s\n", shorten(err.Error()))
		return exitUsage
	}
	mode := "text"
	switch {
	case o.json:
		mode = "json"
	case o.text:
		mode = "text"
	case e.isTTY() && e.getenv("TERM") != "dumb":
		mode = "tui"
	}
	redaction := sbxenv.RedactHome
	if o.json || o.redact {
		redaction = sbxenv.RedactStrict
	}
	plan, err := sbxenv.Resolve(res.Layers, sbxenv.ResolveOptions{
		ProjectDir: res.ProjectDir, HomeDir: home, HomeAliases: homeAliases,
		EnvArgsFiles: res.EnvArgsFiles, EnvArgs: o.envArgs,
		KitArgsFiles: res.KitArgsFiles, KitArgs: o.kitArgs,
		Name: o.name, Clone: o.clone, SkipHostCommands: o.skipHost,
		Cloud: o.cloud, TTL: o.ttl, OnTimeout: o.onTimeout,
		Kits: loader.FSKits{}, Redaction: redaction,
	})
	if err != nil {
		fmt.Fprintf(e.stderr, "sbx-env-viz: %s\n", shorten(err.Error()))
		return exitUsage
	}
	switch mode {
	case "json":
		err = jsonout.Write(e.stdout, plan)
	case "tui":
		var line string
		line, err = tui.Run(plan, e.stdin, e.stdout)
		if err == nil {
			fmt.Fprintln(e.stdout, line)
		}
	default:
		err = text.Write(e.stdout, plan)
	}
	if err != nil {
		fmt.Fprintf(e.stderr, "sbx-env-viz: %s\n", shorten(err.Error()))
		return exitUsage
	}
	return threshold(o.failOn, plan)
}

func threshold(failOn string, p *sbxenv.Plan) int {
	switch failOn {
	case "error":
		if p.Summary.Errors > 0 {
			return exitThreshold
		}
	case "warning":
		if p.Summary.Errors > 0 || p.Summary.Warnings > 0 {
			return exitThreshold
		}
	}
	return exitOK
}
