// Package tui runs the real sbx-env-viz binary in a real pseudo terminal
// (Microsoft tui-test) and checks what a user sees: layout, keys, wide
// characters, colours, and escaping. Every test owns its HOME, its copy of
// the example project, and its terminal session, so tests run in parallel.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tuitest "github.com/microsoft/tui-test/bindings/go"
	"github.com/microsoft/tui-test/bindings/go/tuitesttest"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sbx-env-viz-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "sbx-env-viz")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/sbx-env-viz")
	build.Dir = repoRoot()
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build sbx-env-viz: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// launch describes one run of the binary.
type launch struct {
	cols, rows uint16
	args       []string
	color      bool
	files      map[string]string
}

// screen is a running sbx-env-viz in its own terminal.
type screen struct {
	t    *testing.T
	term *tuitest.Client
}

// start copies testdata/layered into HOME/proj, starts the binary there,
// and waits for the first frame.
func start(t *testing.T, l launch) *screen {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	if err := os.CopyFS(proj, os.DirFS(filepath.Join(repoRoot(), "testdata", "layered"))); err != nil {
		t.Fatal(err)
	}
	for name, content := range l.files {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if l.rows == 0 {
		l.rows = 40
	}
	if l.args == nil {
		l.args = []string{"base.sbxenv.yaml", "local.sbxenv.yaml"}
	}
	env := map[string]string{
		"HOME": home, "PATH": os.Getenv("PATH"), "LANG": "C.UTF-8",
		"TERM": "xterm-256color", "COLORTERM": "truecolor",
	}
	if !l.color {
		env["NO_COLOR"] = "1"
	}
	term := tuitesttest.New(t, tuitesttest.Options{
		Program: binary,
		Args:    l.args,
		Spawn: tuitest.SpawnOptions{
			Cols: tuitest.Ptr(l.cols), Rows: tuitest.Ptr(l.rows),
			Cwd: proj, Env: env, WaitReady: tuitest.Ptr(false),
		},
	})
	s := &screen{t: t, term: term}
	s.expect("sbx-env-viz")
	s.idle()
	return s
}

func (s *screen) idle() {
	s.t.Helper()
	if err := s.term.WaitIdle(tuitest.WaitOptions{Timeout: tuitest.Ptr(5 * time.Second)}); err != nil {
		s.t.Fatalf("wait idle: %v", err)
	}
}

// press sends keys one at a time. WaitIdle alone returns at once when the
// screen has not started to change yet, so each key first waits for the
// screen to differ from before the key (or a short grace period for keys
// that change nothing), then for it to settle.
func (s *screen) press(keys ...string) {
	s.t.Helper()
	for _, k := range keys {
		before := s.text()
		if err := s.term.Press(k); err != nil {
			s.t.Fatalf("press %q: %v", k, err)
		}
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if s.text() != before {
				break
			}
		}
		s.idle()
	}
}

// expect waits until text is visible.
func (s *screen) expect(text string) {
	s.t.Helper()
	err := s.term.GetByText(text, tuitest.TextSelectorOptions{}).First().
		Expect(tuitest.LocatorExpectOptions{Timeout: tuitest.Ptr(10 * time.Second)})
	if err != nil {
		s.t.Fatalf("expected %q on screen: %v\n%s", text, err, s.text())
	}
}

// gone waits until text is no longer visible.
func (s *screen) gone(text string) {
	s.t.Helper()
	err := s.term.GetByText(text, tuitest.TextSelectorOptions{}).First().
		Wait(tuitest.LocatorWaitOptions{State: tuitest.Hidden, Timeout: tuitest.Ptr(5 * time.Second)})
	if err != nil {
		s.t.Fatalf("expected %q to disappear: %v\n%s", text, err, s.text())
	}
}

func (s *screen) text() string {
	s.t.Helper()
	text, err := s.term.Text(tuitest.TextOptions{})
	if err != nil {
		s.t.Fatalf("read screen: %v", err)
	}
	return text
}

func (s *screen) lines() []string { return strings.Split(s.text(), "\n") }

// locations returns where text is drawn (row and column of each match).
func (s *screen) locations(text string) []tuitest.TextMatch {
	s.t.Helper()
	locs, err := s.term.GetByText(text, tuitest.TextSelectorOptions{}).Locations()
	if err != nil {
		s.t.Fatalf("locate %q: %v", text, err)
	}
	return locs
}

// styleChange is one entry of a style snapshot: the attributes that change
// at a cell. Absent fields are unchanged.
type styleChange struct {
	Row, Col int
	FG, BG   string
	Inverse  *bool
}

// styles reads the style changes recorded in a passing style snapshot.
// Cells() is avoided on purpose: in tui-test v0.1.1 it can crash the Go
// runtime when a goroutine stack shrinks, which happens under t.Parallel.
func (s *screen) styles(name string) []styleChange {
	s.t.Helper()
	raw, err := os.ReadFile(filepath.Join("__snapshots__", name+".snap"))
	if err != nil {
		s.t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "\n{")
	if start < 0 {
		s.t.Fatalf("snapshot %s has no style section", name)
	}
	var doc struct {
		Colors map[string]map[string]any `json:"colors"`
	}
	if err := json.Unmarshal([]byte(text[start+1:]), &doc); err != nil {
		s.t.Fatalf("parse style section of %s: %v", name, err)
	}
	var out []styleChange
	for pos, attrs := range doc.Colors {
		var c styleChange
		fmt.Sscanf(pos, "%d,%d", &c.Col, &c.Row)
		if v, ok := attrs["fg"]; ok {
			c.FG = fmt.Sprint(v)
		}
		if v, ok := attrs["bg"]; ok {
			c.BG = fmt.Sprint(v)
		}
		if v, ok := attrs["inverse"].(bool); ok {
			c.Inverse = &v
		}
		out = append(out, c)
	}
	return out
}

func coloured(c styleChange) bool {
	return (c.FG != "" && c.FG != "default") || (c.BG != "" && c.BG != "default")
}

// snapshot compares the screen with __snapshots__/<name>.snap;
// UPDATE_SNAPSHOTS=1 rewrites the baseline.
func (s *screen) snapshot(name string, style bool) {
	s.t.Helper()
	s.idle()
	// Snapshots live in __snapshots__ under the working directory, which
	// go test sets to this package; SnapshotOptions.Cwd has no effect in
	// v0.1.1.
	result, err := s.term.ExpectSnapshot(name, tuitest.SnapshotOptions{
		Update: os.Getenv("UPDATE_SNAPSHOTS") == "1", IncludeStyle: style,
	})
	if err != nil {
		s.t.Fatalf("snapshot %s: %v", name, err)
	}
	if result == tuitest.SnapshotWritten && os.Getenv("CI") != "" {
		s.t.Fatalf("snapshot %s was missing; generate it locally with UPDATE_SNAPSHOTS=1 and commit it", name)
	}
}
