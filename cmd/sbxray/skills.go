package main

import (
	"embed"
	"fmt"
	"io"
	"strings"
)

//go:embed skills/*.md
var embeddedSkills embed.FS

type builtinSkill struct {
	name        string
	description string
	file        string
	fullFile    string
}

var builtinSkills = []builtinSkill{
	{
		name:        "core",
		description: "Core agent workflow, JSON IR schema, and rules (--full adds flag reference)",
		file:        "skills/core.md",
		fullFile:    "skills/core_full.md",
	},
	{
		name:        "ci",
		description: "CI and pull request gating with --fail-on, --redact, and --cloud",
		file:        "skills/ci.md",
	},
	{
		name:        "live-drift",
		description: "Read-only comparison against running sbx sandboxes with --live",
		file:        "skills/live-drift.md",
	},
}

//declscope:shared // main.go prints this for `sbxray help skills` and `sbxray skills --help`.
const skillsUsage = `Usage:
  sbxray skills [list]
  sbxray skills get <name> [--full]

Print version-matched documentation and agent workflows embedded in this binary.

Commands:
  list                List all built-in skills (default)
  get <name>          Print a skill guide to stdout
    --full            Include full reference material (supported by: core)

Examples:
  sbxray skills list
  sbxray skills get core
  sbxray skills get core --full
  sbxray skills get ci
  sbxray skills get live-drift
`

//declscope:shared // main.go dispatches `sbxray skills ...` subcommands here.
func runSkills(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeSkillList(stdout)
		return exitOK
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, skillsUsage)
		return exitOK
	case "list":
		if len(args) > 1 {
			if args[1] == "-h" || args[1] == "--help" {
				fmt.Fprint(stdout, skillsUsage)
				return exitOK
			}
			fmt.Fprintf(stderr, "sbxray skills list: unexpected argument %q\n\n%s", args[1], skillsUsage)
			return exitUsage
		}
		writeSkillList(stdout)
		return exitOK
	case "get":
		return runSkillsGet(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "sbxray skills: unknown subcommand %q (use 'list' or 'get <name>')\n\n%s", args[0], skillsUsage)
		return exitUsage
	}
}

func writeSkillList(w io.Writer) {
	fmt.Fprintln(w, "Built-in skills (run 'sbxray skills get <name>' to read):")
	fmt.Fprintln(w)
	for _, s := range builtinSkills {
		fmt.Fprintf(w, "  %-12s %s\n", s.name, s.description)
	}
}

func runSkillsGet(args []string, stdout, stderr io.Writer) int {
	var name string
	var full bool
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, skillsUsage)
			return exitOK
		case "--full":
			full = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(stderr, "sbxray skills get: unknown flag %s\n\n%s", a, skillsUsage)
				return exitUsage
			}
			if name != "" {
				fmt.Fprintf(stderr, "sbxray skills get: expected one skill name, got extra %q\n\n%s", a, skillsUsage)
				return exitUsage
			}
			name = a
		}
	}
	if name == "" {
		fmt.Fprintf(stderr, "sbxray skills get: missing skill name\n\n")
		writeSkillList(stderr)
		return exitUsage
	}
	for _, s := range builtinSkills {
		if s.name != name {
			continue
		}
		body, err := embeddedSkills.ReadFile(s.file)
		if err != nil {
			fmt.Fprintf(stderr, "sbxray skills get: %v\n", err)
			return exitUsage
		}
		fmt.Fprint(stdout, string(body))
		if full && s.fullFile != "" {
			extra, err := embeddedSkills.ReadFile(s.fullFile)
			if err != nil {
				fmt.Fprintf(stderr, "sbxray skills get: %v\n", err)
				return exitUsage
			}
			fmt.Fprintln(stdout)
			fmt.Fprint(stdout, string(extra))
		}
		return exitOK
	}
	fmt.Fprintf(stderr, "sbxray skills get: unknown skill %q\n\n", name)
	writeSkillList(stderr)
	return exitUsage
}
