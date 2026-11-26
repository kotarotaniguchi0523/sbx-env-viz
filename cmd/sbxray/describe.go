package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

//declscope:shared // Root help and describe share this usage.
const describeUsage = "Usage: sbxray describe [command] [--json]\nPrint the CLI contract or one command as JSON.\n"

type flagSpec struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Repeatable  bool     `json:"repeatable"`
	Default     any      `json:"default"`
	Values      []string `json:"values,omitempty"`
	Description string   `json:"description"`
}

//declscope:shared // Argument validation and discovery share threshold values.
var failOnValues = []string{"none", "warning", "error", "critical", "high", "medium", "low"}

//declscope:shared // parseArgs uses the contract to identify flags taking a value.
var flags = []flagSpec{
	{"--json", "boolean", false, false, nil, "Emit strictly redacted JSON"},
	{"--text", "boolean", false, false, nil, "Emit plain text"},
	{"--redact", "boolean", false, false, nil, "Apply strict redaction to text and TUI"},
	{"--fail-on", "string", false, "none", failOnValues, "Set the exit threshold"},
	{"--live", "boolean", false, false, nil, "Compare with running sandboxes"},
	{"--offline", "boolean", false, false, nil, "Disable live discovery"},
	{"--env-arg", "string", true, nil, nil, "Set an environment argument with NAME=VALUE"},
	{"--env-args-file", "string", true, nil, nil, "Read environment arguments from a file"},
	{"--kit-arg", "string", true, nil, nil, "Override a kit argument with [KIT.]NAME=VALUE"},
	{"--kit-args-file", "string", true, nil, nil, "Read kit arguments from a file"},
	{"--name", "string", false, nil, nil, "Override the sandbox name"},
	{"--clone", "boolean", false, nil, nil, "Override workspace.clone"},
	{"--skip-host-commands", "boolean", false, false, nil, "Treat host commands as skipped"},
	{"--cloud", "boolean", false, false, nil, "Validate cloud settings"},
	{"--ttl", "string", false, nil, nil, "Set the cloud expiration duration"},
	{"--on-timeout", "string", false, nil, []string{"stop", "restart", "delete"}, "Set the cloud expiration action"},
	{"--help", "boolean", false, false, nil, "Show usage"},
	{"-h", "boolean", false, false, nil, "Show usage"},
	{"--version", "boolean", false, false, nil, "Print version information"},
}

type commandSpec struct {
	Name        string        `json:"name"`
	Usage       string        `json:"usage"`
	Description string        `json:"description"`
	Subcommands []commandSpec `json:"subcommands,omitempty"`
}

//declscope:shared // main dispatches the describe command.
func runDescribe(args []string, stdout, stderr io.Writer) int {
	commands := []commandSpec{
		{"help", "sbxray help [command]", "Show command help", nil},
		{"skills", "sbxray skills [list|get]", "Read embedded agent guides", []commandSpec{
			{"list", "sbxray skills [list] [--json]", "List embedded guides", nil},
			{"get", "sbxray skills get <name> [--full]", "Print a guide", nil},
		}},
		{"describe", "sbxray describe [command] [--json]", "Print the CLI contract as JSON", nil},
	}
	jsonMode := slices.Contains(args, "--json")
	var topics []string
	for _, a := range args {
		switch a {
		case "--json":
			continue
		case "-h", "--help":
			fmt.Fprint(stdout, describeUsage)
			return exitOK
		default:
			if strings.HasPrefix(a, "-") {
				return report(stderr, jsonMode, problem("unknown_flag", "unknown flag "+a, "sbxray describe --help", a, []string{"--json", "--help"}))
			}
			topics = append(topics, a)
		}
	}
	var result any
	switch len(topics) {
	case 0:
		result = map[string]any{
			"name": "sbxray", "version": version,
			"schemaVersion": sbxenv.SchemaVersion, "targetSbxVersion": sbxenv.TargetSbxVersion,
			"exitCodes": map[string]string{"0": "ok", "1": "--fail-on threshold reached", "2": "usage or parse error"},
			"commands":  commands, "flags": flags,
			"jsonOutputFields": map[string]string{
				"meta":        "Schema, redaction and sources",
				"invocation":  "Resolved options",
				"summary":     "Boundary and diagnostic counts",
				"diagnostics": "Analysis findings",
				"items":       "Effective settings and provenance",
				"coverage":    "Limits of static analysis",
				"live":        "Optional live comparison",
			},
		}
	case 1:
		var names []string
		for _, c := range commands {
			names = append(names, c.Name)
			if c.Name == topics[0] {
				result = c
			}
		}
		if result == nil {
			return report(stderr, jsonMode, problem("unknown_command", fmt.Sprintf("unknown command %q", topics[0]), "sbxray describe", topics[0], names))
		}
	default:
		return report(stderr, jsonMode, problem("usage", "expected at most one command", "sbxray describe", "", nil))
	}
	return writeJSON(stdout, stderr, jsonMode, result)
}
