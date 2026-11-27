package live

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// Target is one sandbox to show: a declaration, a running sandbox, or both.
type Target struct {
	// Name is what the switcher shows.
	Name string
	// State is the Japanese status badge (稼働中, 停止中, 宣言のみ（未作成）, ...).
	State string
	// Files are the environment files, as displayed.
	Files []string
	// Plan is the resolved declaration; nil for a sandbox without files.
	Plan *sbxenv.Plan
	// Err explains, in Japanese, why the files could not be resolved.
	Err string
	// Sandbox is the sbx view; nil when the declaration is not created.
	Sandbox *Sandbox
	// Comparison is set when both sides exist.
	Comparison *Comparison
}

// Drifts returns the number of differences found.
func (t *Target) Drifts() int {
	if t.Comparison == nil {
		return 0
	}
	return len(t.Comparison.Drifts)
}

// Group is one set of files resolved together, as sbx env would.
type Group struct {
	Dir string
	// Paths are explicit files; empty means the no-path default, which
	// merges ~/.sbxenv.yaml under <Dir>/sbxenv.yaml.
	Paths []string
}

// Resolver resolves a file group into a plan with the caller's options.
type Resolver func(g Group) (*sbxenv.Plan, error)

// DiscoverRequest describes where to look.
type DiscoverRequest struct {
	WorkDir string
	// Runner is nil to stay offline.
	Runner  Runner
	Resolve Resolver
	// Show formats a host path for display (home shortened).
	Show Paths
	// Redaction is the plans' mode; under strict (or empty) the paths of
	// a sandbox no file declares are hidden too.
	Redaction sbxenv.Redaction
}

// Discover finds environment files in the working directory and next to
// every sandbox's workspace, asks sbx for existing sandboxes, pairs them by
// name or workspace path, and compares each pair. The snapshot is returned
// so the caller can report why live data is missing.
func Discover(ctx context.Context, req DiscoverRequest) ([]Target, Snapshot) {
	show := req.Show
	if show == nil {
		show = func(s string) string { return s }
	}
	var snap Snapshot
	if req.Runner != nil {
		snap = Collect(ctx, req.Runner)
	}
	dirs := []string{req.WorkDir}
	for _, sb := range snap.Sandboxes {
		for _, w := range sb.Workspaces {
			if p := w.matchPath(); filepath.IsAbs(p) {
				dirs = append(dirs, p, filepath.Dir(p))
			}
		}
	}
	var groups []Group
	seenDir := map[string]bool{}
	for _, d := range dirs {
		if d == "" || seenDir[canonical(d)] {
			continue
		}
		seenDir[canonical(d)] = true
		groups = append(groups, FileGroups(d)...)
	}

	var targets []Target
	paired := map[int]bool{}
	for _, g := range groups {
		t := Target{Files: groupFiles(g, show)}
		p, err := req.Resolve(g)
		if err != nil {
			t.Name = filepath.Base(g.Dir) + "/" + filepath.Base(t.Files[len(t.Files)-1])
			t.Err = "設定ファイルを解析できませんでした: " + clean(err.Error())
			t.State = "解析エラー"
			targets = append(targets, t)
			continue
		}
		t.Plan = p
		t.Name = targetName(p, g)
		if i := match(p.Declared, snap.Sandboxes, paired); i >= 0 {
			paired[i] = true
			sb := snap.Sandboxes[i]
			t.Sandbox = &sb
			if p.Declared.Name == "" {
				t.Name = sb.Name
			}
			t.State = StateLabel(sb.Status)
			cmp := Compare(p, &sb, &snap, show)
			t.Comparison = &cmp
		} else if snap.Available {
			t.State = "宣言のみ（未作成）"
		} else {
			t.State = "宣言"
		}
		targets = append(targets, t)
	}
	for i, sb := range snap.Sandboxes {
		if paired[i] {
			continue
		}
		sb := sb
		sb.Workspaces, sb.Mounts = shownMounts(sb.Workspaces, req.Redaction, show), shownMounts(sb.Mounts, req.Redaction, show)
		targets = append(targets, Target{Name: sb.Name, State: StateLabel(sb.Status) + "・宣言ファイル未検出", Sandbox: &sb})
	}
	rank := func(t Target) int {
		switch {
		case t.Sandbox != nil && t.Plan != nil:
			return 0
		case t.Sandbox != nil:
			return 1
		}
		return 2
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if rank(targets[i]) != rank(targets[j]) {
			return rank(targets[i]) < rank(targets[j])
		}
		return targets[i].Name < targets[j].Name
	})
	return targets, snap
}

// FileGroups lists the environment files of one directory. sbxenv.yaml
// alone is the no-path default; sbxenv.X.yaml is resolved with its
// sbxenv.X.local.yaml companion when one exists.
//
//declscope:ignore overexported // live_test.go tests the file pairing through it directly, apart from Discover
func FileGroups(dir string) []Group {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && (n == "sbxenv.yaml" || (strings.HasPrefix(n, "sbxenv.") && strings.HasSuffix(n, ".yaml"))) {
			files[n] = true
		}
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Group
	used := map[string]bool{}
	for _, n := range names {
		if strings.HasSuffix(n, ".local.yaml") {
			continue
		}
		companion := strings.TrimSuffix(n, ".yaml") + ".local.yaml"
		switch {
		case n == "sbxenv.yaml" && !files[companion]:
			out = append(out, Group{Dir: dir})
		case files[companion]:
			used[companion] = true
			out = append(out, Group{Dir: dir, Paths: []string{filepath.Join(dir, n), filepath.Join(dir, companion)}})
		default:
			out = append(out, Group{Dir: dir, Paths: []string{filepath.Join(dir, n)}})
		}
	}
	for _, n := range names {
		if strings.HasSuffix(n, ".local.yaml") && !used[n] {
			out = append(out, Group{Dir: dir, Paths: []string{filepath.Join(dir, n)}})
		}
	}
	return out
}

func groupFiles(g Group, show Paths) []string {
	if len(g.Paths) == 0 {
		return []string{show(filepath.Join(g.Dir, "sbxenv.yaml"))}
	}
	var out []string
	for _, p := range g.Paths {
		out = append(out, show(p))
	}
	return out
}

func targetName(p *sbxenv.Plan, g Group) string {
	if p.Declared != nil && p.Declared.Name != "" {
		for _, it := range p.Items {
			if it.ID == "name" {
				return it.Value
			}
		}
	}
	file := "sbxenv.yaml"
	if len(g.Paths) > 0 {
		file = filepath.Base(g.Paths[0])
	}
	return filepath.Base(g.Dir) + "/" + file
}

// match finds the sandbox a declaration created: same name first, then the
// same primary workspace directory.
//
//declscope:shared // CollectMatched and Discover use the same pairing rules
func match(d *sbxenv.Declared, sbs []Sandbox, taken map[int]bool) int {
	if d == nil {
		return -1
	}
	if d.Name != "" {
		for i, sb := range sbs {
			if !taken[i] && sb.Name == d.Name {
				return i
			}
		}
		return -1
	}
	for _, w := range d.Workspaces {
		if !w.Primary {
			continue
		}
		for i, sb := range sbs {
			if taken[i] {
				continue
			}
			for _, m := range sb.Workspaces {
				if samePath(m.matchPath(), w.Path) {
					return i
				}
			}
		}
	}
	return -1
}

// StateLabel turns an sbx status into a Japanese badge.
func StateLabel(status string) string {
	switch strings.ToLower(status) {
	case "running", "started", "active":
		return "稼働中"
	case "stopped", "exited", "hibernated", "paused":
		return "停止中"
	case "":
		return "状態不明"
	}
	return status
}

// shownMounts is mounts with each path as a person reads it: a sandbox no
// file declares is drawn from sbx's paths alone, so they go through the
// same livePath as a drift's.
func shownMounts(mounts []Mount, mode sbxenv.Redaction, show Paths) []Mount {
	out := make([]Mount, len(mounts))
	for i, w := range mounts {
		out[i] = Mount{Path: livePath(mode, show, w.Path)}
	}
	return out
}
