// Package loader reads environment files, argument files, and local kits
// from the file system, following `sbx env` path rules.
package loader

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// DefaultFileName is the file sbx looks for in a directory.
const DefaultFileName = "sbxenv.yaml"

// UserDefaultsName is the user defaults file in the home directory.
const UserDefaultsName = ".sbxenv.yaml"

// maxFileSize bounds every file read (environment files, args files, kits).
const maxFileSize = 4 << 20

// Request describes what to load.
type Request struct {
	// Paths are positional arguments (files or directories).
	Paths        []string
	EnvArgsFiles []string
	KitArgsFiles []string
	// WorkDir is the directory the command runs from.
	WorkDir string
	HomeDir string
}

// Result is the loaded input for sbxenv.Resolve.
type Result struct {
	Layers       []sbxenv.LayerInput
	ProjectDir   string
	EnvArgsFiles []sbxenv.ArgFile
	KitArgsFiles []sbxenv.ArgFile
}

// Load applies the sbx env rules: a directory means <dir>/sbxenv.yaml;
// without paths, ~/.sbxenv.yaml (if present) and ./sbxenv.yaml (if present)
// are merged in that order; with paths, ~/.sbxenv.yaml is skipped.
func Load(req Request) (*Result, error) {
	res := &Result{}
	if len(req.Paths) == 0 {
		res.ProjectDir = req.WorkDir
		if req.HomeDir != "" {
			p := filepath.Join(req.HomeDir, UserDefaultsName)
			if data, err := readFile(p); err == nil {
				res.Layers = append(res.Layers, sbxenv.LayerInput{Path: p, Content: data, Kind: sbxenv.LayerUserDefaults})
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
		p := filepath.Join(req.WorkDir, DefaultFileName)
		if data, err := readFile(p); err == nil {
			res.Layers = append(res.Layers, sbxenv.LayerInput{Path: p, Content: data, Kind: sbxenv.LayerEnv})
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if len(res.Layers) == 0 {
			return nil, fmt.Errorf("no environment file: %s not found and no ~/%s; pass a file or directory", DefaultFileName, UserDefaultsName)
		}
	} else {
		for i, raw := range req.Paths {
			p := raw
			if !filepath.IsAbs(p) {
				p = filepath.Join(req.WorkDir, p)
			}
			p = filepath.Clean(p)
			st, err := os.Stat(p)
			if err != nil {
				return nil, fmt.Errorf("environment file %q: %w", raw, describe(err))
			}
			dir := filepath.Dir(p)
			if st.IsDir() {
				dir = p
				p = filepath.Join(p, DefaultFileName)
			}
			if i == 0 {
				res.ProjectDir = dir
			}
			data, err := readFile(p)
			if err != nil {
				shown := raw
				if st.IsDir() {
					shown = filepath.Join(raw, DefaultFileName)
				}
				return nil, fmt.Errorf("environment file %q: %w", shown, describe(err))
			}
			res.Layers = append(res.Layers, sbxenv.LayerInput{Path: p, Content: data, Kind: sbxenv.LayerEnv})
		}
	}
	var err error
	if res.EnvArgsFiles, err = readArgFiles(req.EnvArgsFiles, req.WorkDir); err != nil {
		return nil, err
	}
	if res.KitArgsFiles, err = readArgFiles(req.KitArgsFiles, req.WorkDir); err != nil {
		return nil, err
	}
	return res, nil
}

func readArgFiles(paths []string, wd string) ([]sbxenv.ArgFile, error) {
	var out []sbxenv.ArgFile
	for _, raw := range paths {
		p := raw
		if !filepath.IsAbs(p) {
			p = filepath.Join(wd, p)
		}
		data, err := readFile(p)
		if err != nil {
			return nil, fmt.Errorf("args file %q: %w", raw, describe(err))
		}
		out = append(out, sbxenv.ArgFile{Path: filepath.Clean(p), Content: data})
	}
	return out, nil
}

func readFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("is a directory")
	}
	if st.Size() > maxFileSize {
		return nil, fmt.Errorf("file is larger than %d bytes", maxFileSize)
	}
	buf := make([]byte, st.Size())
	n, err := io.ReadFull(f, buf)
	return buf[:n], err
}

// describe drops the path from OS errors (callers add a redacted one).
func describe(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// FSKits loads local kit descriptors from disk.
type FSKits struct{}

// LoadKit finds the descriptor in dir: <dir>/<base>.yaml (companion-pair
// convention), else the single YAML file starting with the kit syntax line,
// else a legacy spec.yaml.
func (FSKits) LoadKit(dir string) (sbxenv.KitInput, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return sbxenv.KitInput{}, describe(err)
	}
	if !st.IsDir() {
		return sbxenv.KitInput{}, fmt.Errorf("not a directory")
	}
	base := filepath.Base(dir)
	for _, name := range []string{base + ".yaml", base + ".yml"} {
		p := filepath.Join(dir, name)
		if data, err := readFile(p); err == nil {
			return sbxenv.KitInput{DescriptorPath: p, Content: data}, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sbxenv.KitInput{}, describe(err)
	}
	var candidates []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
			continue
		}
		candidates = append(candidates, n)
	}
	sort.Strings(candidates)
	for _, n := range candidates {
		p := filepath.Join(dir, n)
		data, err := readFile(p)
		if err == nil && parser.HasSyntaxHeader(data) {
			return sbxenv.KitInput{DescriptorPath: p, Content: data}, nil
		}
	}
	for _, n := range []string{"spec.yaml", "spec.yml"} {
		p := filepath.Join(dir, n)
		if data, err := readFile(p); err == nil {
			return sbxenv.KitInput{DescriptorPath: p, Content: data}, nil
		}
	}
	return sbxenv.KitInput{}, fmt.Errorf("no kit descriptor (%s.yaml or a file starting with # syntax=docker/sandbox-kit)", base)
}
