// Command sbxray visualizes layered Docker Sandboxes environment files.
//
// It accepts the same paths and flags as `sbx env create`, resolves them
// offline, and shows the result as a TUI (TTY), text (--text, or non-TTY), or
// JSON (--json).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/loader"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/jsonout"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/text"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/tui"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
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
		getenv: os.Getenv, getwd: os.Getwd, home: os.UserHomeDir, sbx: live.ExecRunner{},
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
	// sbx runs read-only sbx commands for live data.
	sbx live.Runner
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
	live         bool
	offline      bool
	help         bool
	version      bool
}

const usage = `Usage: sbxray [PATH...] [flags]

Statically resolves sbx environment files (sbxenv.yaml) and local kits and
shows the effective configuration, provenance, boundaries, and diagnostics.
Paths and flags mirror "sbx env create". A directory means <dir>/sbxenv.yaml;
with no path, ./sbxenv.yaml is merged over ~/.sbxenv.yaml.

With no path in a terminal (TUI), it also finds sandboxes automatically:
read-only "sbx ls / policy ls / secret ls / mcp ls --json" list the
existing sandboxes, sbxenv*.yaml files are searched in the working
directory and next to each workspace, declarations are paired with
sandboxes, and differences (drift) are shown. Without sbx, or when the
daemon is stopped, only the files are analysed. Secret values are never
requested.

Output (default: TUI when stdin and stdout are terminals and TERM != dumb,
otherwise text):
  --text                  plain text summary
  --json                  schemaVersion "1" JSON IR (strictly redacted)
  --redact                strict redaction for TUI / text too
  --fail-on LEVEL         exit 1 when reached. none (default) | warning | error
                          (diagnostic levels) | critical | high | medium | low
                          (the Risk view's scale: diagnostics, risky items, and
                          drift with --live; analysis coverage is not counted)
  --live                  also read existing sandboxes from sbx (any output)
  --offline               never run sbx (the default for paths, --text, --json)

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
		case "live":
			o.live, err = boolVal()
		case "offline":
			o.offline, err = boolVal()
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
		if sbxenv.Severity(o.failOn).Rank() == 0 {
			return nil, fmt.Errorf("--fail-on must be none, warning, error, critical, high, medium, or low")
		}
	}
	if o.text && o.json {
		return nil, errors.New("--text and --json cannot be combined")
	}
	if o.live && o.offline {
		return nil, errors.New("--live and --offline cannot be combined")
	}
	return o, nil
}

func run(args []string, e env) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(e.stderr, "sbxray: %v\n\n%s", err, usage)
		return exitUsage
	}
	if o.help {
		fmt.Fprint(e.stdout, usage)
		return exitOK
	}
	if o.version {
		fmt.Fprintf(e.stdout, "sbxray %s (IR schema %s, targets sbx %s)\n", version, sbxenv.SchemaVersion, sbxenv.TargetSbxVersion)
		return exitOK
	}
	wd, err := e.getwd()
	if err != nil {
		fmt.Fprintf(e.stderr, "sbxray: cannot determine the working directory\n")
		return exitUsage
	}
	home, _ := e.home()
	var homeAliases []string
	if home != "" {
		if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
			homeAliases = append(homeAliases, resolved)
		}
	}
	shorten := func(s string) string { return model.EscapeControl(sbxenv.ShortenHome(s, home, homeAliases...)) }
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
	resolve := func(req loader.Request) (*sbxenv.Plan, error) {
		res, err := loader.Load(req)
		if err != nil {
			return nil, err
		}
		return sbxenv.Resolve(res.Layers, sbxenv.ResolveOptions{
			ProjectDir: res.ProjectDir, HomeDir: home, HomeAliases: homeAliases,
			EnvArgsFiles: res.EnvArgsFiles, EnvArgs: o.envArgs,
			KitArgsFiles: res.KitArgsFiles, KitArgs: o.kitArgs,
			Name: o.name, Clone: o.clone, SkipHostCommands: o.skipHost,
			Cloud: o.cloud, TTL: o.ttl, OnTimeout: o.onTimeout,
			Kits: loader.FSKits{}, Redaction: redaction,
		})
	}
	show := func(p string) string { return model.EscapeControl(sbxenv.ShortenHome(p, home, homeAliases...)) }

	// Without paths in a terminal, discover sandboxes and files and let the
	// person switch between them.
	if len(o.paths) == 0 && mode == "tui" && !o.offline {
		targets, snap := live.Discover(context.Background(), live.DiscoverRequest{
			WorkDir: wd, Runner: e.sbx, Show: show,
			Resolve: func(g live.Group) (*sbxenv.Plan, error) {
				return resolve(loader.Request{Paths: g.Paths, EnvArgsFiles: o.envArgsFiles, KitArgsFiles: o.kitArgsFiles, WorkDir: g.Dir, HomeDir: home})
			},
		})
		if len(targets) > 0 {
			line, err := tui.RunTargets(targets, snap.Problem, e.stdin, e.stdout)
			if err != nil {
				fmt.Fprintf(e.stderr, "sbxray: %s\n", shorten(err.Error()))
				return exitUsage
			}
			fmt.Fprintln(e.stdout, line)
			worst := exitOK
			for _, t := range targets {
				if t.Plan != nil {
					worst = max(worst, threshold(o.failOn, t.Plan, t.Comparison))
				}
			}
			return worst
		}
		// Nothing found: fall through to the usual message about files.
	}

	plan, err := resolve(loader.Request{Paths: o.paths, EnvArgsFiles: o.envArgsFiles, KitArgsFiles: o.kitArgsFiles, WorkDir: wd, HomeDir: home})
	if err != nil {
		fmt.Fprintf(e.stderr, "sbxray: %s\n", shorten(err.Error()))
		return exitUsage
	}
	var target *live.Target
	if o.live {
		target = liveTarget(plan, e.sbx, show)
	}
	switch {
	case mode == "tui" && target != nil:
		var line string
		line, err = tui.RunTargets([]live.Target{*target}, target.Err, e.stdin, e.stdout)
		if err == nil {
			fmt.Fprintln(e.stdout, line)
		}
	case mode == "json" && target != nil:
		err = writeJSONWithLive(e.stdout, plan, target)
	case mode == "json":
		err = jsonout.Write(e.stdout, plan)
	case mode == "tui":
		var line string
		line, err = tui.Run(plan, e.stdin, e.stdout)
		if err == nil {
			fmt.Fprintln(e.stdout, line)
		}
	default:
		err = text.Write(e.stdout, plan)
		if err == nil && target != nil {
			err = text.WriteLive(e.stdout, target)
		}
	}
	if err != nil {
		fmt.Fprintf(e.stderr, "sbxray: %s\n", shorten(err.Error()))
		return exitUsage
	}
	var cmp *live.Comparison
	if target != nil {
		cmp = target.Comparison
	}
	return threshold(o.failOn, plan, cmp)
}

// threshold decides the exit code. warning and error count diagnostics
// only. The risk levels use the scale of the Risk view: diagnostics, items
// rated above info, and drift found by --live. Coverage gaps are premises
// of every analysis, not findings, so they never trip the threshold.
func threshold(failOn string, p *sbxenv.Plan, cmp *live.Comparison) int {
	if want := sbxenv.Severity(failOn); want.Rank() > 0 {
		worst := 0
		for _, r := range model.RiskList(p) {
			if r.Kind != model.KindCoverage {
				worst = max(worst, r.Severity.Rank())
			}
		}
		if cmp != nil {
			for _, d := range cmp.Drifts {
				worst = max(worst, d.Severity.Rank())
			}
		}
		if worst >= want.Rank() {
			return exitThreshold
		}
		return exitOK
	}
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

// liveTarget pairs one resolved plan with the sandbox it created. Err
// carries the reason when sbx gave no answer or no sandbox matches.
func liveTarget(plan *sbxenv.Plan, r live.Runner, show live.Paths) *live.Target {
	snap := live.CollectMatched(context.Background(), r, plan.Declared)
	t := &live.Target{Name: "（宣言）", State: "宣言", Plan: plan, Err: snap.Problem}
	if plan.Declared != nil && plan.Declared.Name != "" {
		for _, it := range plan.Items {
			if it.ID == "name" {
				t.Name = it.Value
				break
			}
		}
	}
	if !snap.Available {
		return t
	}
	t.State = "宣言のみ（未作成）"
	if len(snap.Sandboxes) > 0 {
		sb := snap.Sandboxes[0]
		cmp := live.Compare(plan, &sb, &snap, show)
		if plan.Declared == nil || sb.Name != plan.Declared.Name {
			t.Name = sb.Name
		}
		t.State, t.Sandbox, t.Comparison = live.StateLabel(sb.Status), &sb, &cmp
	}
	return t
}

func writeJSONWithLive(w io.Writer, plan *sbxenv.Plan, t *live.Target) error {
	var buf bytes.Buffer
	if err := jsonout.Write(&buf, plan); err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		return err
	}
	l := map[string]any{"state": t.State}
	if t.Err != "" {
		l["problem"] = t.Err
	}
	if t.Sandbox != nil {
		l["sandbox"] = t.Name
	}
	if c := t.Comparison; c != nil {
		drifts := []map[string]any{}
		for _, d := range c.Drifts {
			drifts = append(drifts, map[string]any{"kind": d.Kind, "title": d.Title, "declared": d.Declared, "actual": d.Actual,
				"fix": d.Fix, "severity": d.Severity, "itemId": d.ItemID, "source": d.Source})
		}
		l["drift"], l["checked"], l["skipped"] = drifts, c.Checked, c.Skipped
	}
	doc["live"] = l
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
