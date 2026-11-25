package live

import "path/filepath"

// samePath reports whether a and b name the same directory once symbolic
// links are resolved. An empty path matches nothing.
//
//declscope:shared // drift.go and discover.go both pair a declaration with a sandbox by its workspace path
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonical(a) == canonical(b)
}

// canonical cleans p and resolves symbolic links in its longest existing
// prefix, so /var/x and /private/var/x (macOS) compare equal even when x
// itself does not exist.
//
//declscope:shared // discover.go keys the directories it has already searched by it
func canonical(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
