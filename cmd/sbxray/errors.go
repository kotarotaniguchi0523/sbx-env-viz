package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
)

//declscope:shared // Root argument parsing passes structured details to report.
type cliError struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Next       string   `json:"next_step"`
	Candidates []string `json:"candidates,omitempty"`
}

func (e *cliError) Error() string { return e.Message }

//declscope:shared // Command parsers attach recovery steps and spelling suggestions.
func problem(code, message, next, input string, choices []string) *cliError {
	e := &cliError{Code: code, Message: model.EscapeControl(message), Next: next}
	input = strings.TrimLeft(input, "-")
	for _, choice := range choices {
		s := strings.TrimLeft(choice, "-")
		if input != "" && (strings.HasPrefix(s, input) || closeSpelling(input, s)) {
			e.Candidates = append(e.Candidates, choice)
		}
	}
	return e
}

// closeSpelling accepts at most two insertions, deletions or substitutions.
func closeSpelling(a, b string) bool {
	if len(a) > len(b)+2 || len(b) > len(a)+2 {
		return false
	}
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 0; i < len(a); i++ {
		prev := row[0]
		row[0] = i + 1
		for j := 1; j < len(row); j++ {
			cost := 1
			if a[i] == b[j-1] {
				cost = 0
			}
			old := row[j]
			row[j] = min(old+1, row[j-1]+1, prev+cost)
			prev = old
		}
	}
	return row[len(b)] <= 2
}

//declscope:shared // All command handlers use the same stderr contract.
func report(w io.Writer, jsonMode bool, e *cliError) int {
	if jsonMode {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]*cliError{"error": e})
	} else {
		fmt.Fprintln(w, "sbxray:", e.Message)
		if len(e.Candidates) > 0 {
			fmt.Fprintln(w, "candidates:", strings.Join(e.Candidates, ", "))
		}
		fmt.Fprintln(w, "next_step:", e.Next)
	}
	return exitUsage
}

//declscope:shared // Discovery commands write one JSON document on stdout.
func writeJSON(stdout, stderr io.Writer, jsonMode bool, data any) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return report(stderr, jsonMode, problem("output", err.Error(), "sbxray describe", "", nil))
	}
	return exitOK
}
