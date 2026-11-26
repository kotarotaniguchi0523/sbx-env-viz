package main

import (
	"embed"
	"fmt"
	"io"
	"slices"
	"strings"
)

//go:embed skills/*.md
var embeddedSkills embed.FS

type builtinSkill struct {
	name     string
	file     string
	fullFile string
}

var builtinSkills = []builtinSkill{
	{name: "core", file: "skills/core.md", fullFile: "skills/core_full.md"},
	{name: "ci", file: "skills/ci.md"},
	{name: "live-drift", file: "skills/live-drift.md"},
}

// parseSkillDoc splits optional YAML frontmatter (--- ... ---) from a skill
// markdown document and returns its description and body.
func parseSkillDoc(raw string) (description, body string) {
	if !strings.HasPrefix(raw, "---\n") {
		return "", raw
	}
	rest := raw[len("---\n"):]
	fm, after, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return "", raw
	}
	for _, line := range strings.Split(fm, "\n") {
		k, v, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(k) == "description" {
			description = strings.TrimSpace(v)
		}
	}
	return description, after
}

//declscope:shared // main.go prints this for `sbxray help skills` and `sbxray skills --help`.
const skillsUsage = `Usage:
  sbxray skills [list] [--json]
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
	jsonMode := slices.Contains(args, "--json")
	if len(args) > 0 && args[0] == "get" {
		return runSkillsGet(args[1:], stdout, stderr, jsonMode)
	}
	args = slices.DeleteFunc(args, func(s string) bool { return s == "--json" })
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			fmt.Fprint(stdout, skillsUsage)
			return exitOK
		case "list":
			if len(args) > 1 {
				if args[1] == "--help" || args[1] == "-h" {
					fmt.Fprint(stdout, skillsUsage)
					return exitOK
				}
				if strings.HasPrefix(args[1], "-") {
					return report(stderr, jsonMode, problem("unknown_flag", "unknown flag "+args[1], "sbxray skills list", args[1], []string{"--json", "--help"}))
				}
				return report(stderr, jsonMode, problem("usage", fmt.Sprintf("unexpected argument %q", args[1]), "sbxray skills list", args[1], []string{"--json", "--help"}))
			}
		default:
			if strings.HasPrefix(args[0], "-") {
				return report(stderr, jsonMode, problem("unknown_flag", "unknown flag "+args[0], "sbxray skills list", args[0], []string{"--json", "--help"}))
			}
			return report(stderr, jsonMode, problem("unknown_subcommand", fmt.Sprintf("unknown subcommand %q", args[0]), "sbxray skills list", args[0], []string{"list", "get", "help"}))
		}
	}
	type entry struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SupportsFull bool   `json:"supportsFull"`
	}
	var entries []entry
	if !jsonMode {
		fmt.Fprintln(stdout, "Built-in skills (run 'sbxray skills get <name>' to read):")
		fmt.Fprintln(stdout)
	}
	for _, s := range builtinSkills {
		raw, err := embeddedSkills.ReadFile(s.file)
		if err != nil {
			return report(stderr, jsonMode, problem("load", err.Error(), "sbxray skills list", "", nil))
		}
		desc, _ := parseSkillDoc(string(raw))
		if jsonMode {
			entries = append(entries, entry{s.name, desc, s.fullFile != ""})
		} else {
			fmt.Fprintf(stdout, "  %-12s %s\n", s.name, desc)
		}
	}
	if jsonMode {
		return writeJSON(stdout, stderr, true, map[string][]entry{"skills": entries})
	}
	return exitOK
}

func runSkillsGet(args []string, stdout, stderr io.Writer, jsonMode bool) int {
	var name string
	var full bool
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, skillsUsage)
			return exitOK
		case "--json":
			continue
		case "--full":
			full = true
		default:
			if strings.HasPrefix(a, "-") {
				return report(stderr, jsonMode, problem("unknown_flag", "unknown flag "+a, "sbxray skills get core --full", a, []string{"--full", "--help"}))
			}
			if name != "" {
				return report(stderr, jsonMode, problem("usage", fmt.Sprintf("expected one skill name, got extra %q", a), "sbxray skills list", "", nil))
			}
			name = a
		}
	}
	if name == "" {
		return report(stderr, jsonMode, problem("usage", "missing skill name", "sbxray skills list", "", nil))
	}
	for _, s := range builtinSkills {
		if s.name != name {
			continue
		}
		if jsonMode {
			return report(stderr, true, problem("unknown_flag", "unknown flag --json for skills get", "sbxray skills list --json", "", nil))
		}
		raw, err := embeddedSkills.ReadFile(s.file)
		if err != nil {
			return report(stderr, jsonMode, problem("load", err.Error(), "sbxray skills list", "", nil))
		}
		_, body := parseSkillDoc(string(raw))
		fmt.Fprint(stdout, body)
		if full && s.fullFile != "" {
			extra, err := embeddedSkills.ReadFile(s.fullFile)
			if err != nil {
				return report(stderr, jsonMode, problem("load", err.Error(), "sbxray skills list", "", nil))
			}
			fmt.Fprintln(stdout)
			fmt.Fprint(stdout, string(extra))
		}
		return exitOK
	}
	var names []string
	for _, s := range builtinSkills {
		names = append(names, s.name)
	}
	return report(stderr, jsonMode, problem("unknown_skill", fmt.Sprintf("unknown skill %q", name), "sbxray skills list", name, names))
}
