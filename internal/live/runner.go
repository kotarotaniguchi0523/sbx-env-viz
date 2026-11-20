// Package live reads the state of existing sandboxes from the sbx CLI
// (read-only commands with --json), finds the environment files that
// declare them, pairs declarations with sandboxes, and reports where the
// two disagree (drift).
//
// Every sbx call goes through Runner so tests can feed fixture output. Only
// listing commands are run; secret values are never requested or kept.
package live

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner runs one sbx command and returns its standard output.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ErrNoSbx means the sbx command is not installed or not on PATH.
var ErrNoSbx = errors.New("sbx command not found")

// maxOutput bounds what one sbx command may print.
const maxOutput = 16 << 20

// ExecRunner runs the sbx binary found on PATH.
type ExecRunner struct {
	// Timeout bounds each command; zero means 15 seconds.
	Timeout time.Duration
}

// Run executes `sbx ARGS... --no-input`. --no-input makes sbx fail instead
// of prompting, so a listing can never wait for an answer.
func (r ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("sbx")
	if err != nil {
		return nil, ErrNoSbx
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(append([]string{}, args...), "--no-input")...)
	var out limited
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sbx %s: %w: %s", strings.Join(args, " "), err, firstLine(stderr.String()))
	}
	if out.over {
		return nil, fmt.Errorf("sbx %s: output larger than %d bytes", strings.Join(args, " "), maxOutput)
	}
	return out.b.Bytes(), nil
}

type limited struct {
	b    bytes.Buffer
	over bool
}

func (l *limited) Write(p []byte) (int, error) {
	if l.b.Len()+len(p) > maxOutput {
		l.over = true
		return len(p), nil
	}
	return l.b.Write(p)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
