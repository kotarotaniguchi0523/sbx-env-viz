package sbxenv

import (
	"sort"
	"strings"
)

// redactor is the only way values enter a Plan. Every builder in this
// package formats display strings through it, so forgetting to redact a
// field means forgetting to copy it at all.
type redactor struct {
	mode Redaction
	// homes holds every spelling of $HOME, longest first.
	homes []string
}

func newRedactor(mode Redaction, home string, aliases []string) *redactor {
	if mode == "" {
		mode = RedactStrict
	}
	r := &redactor{mode: mode}
	seen := map[string]bool{}
	for _, h := range append([]string{home}, aliases...) {
		h = strings.TrimRight(h, `/\`)
		for _, v := range []string{h, strings.ReplaceAll(h, `\`, `/`)} {
			if len(v) >= 2 && !seen[v] {
				seen[v] = true
				r.homes = append(r.homes, v)
			}
		}
	}
	sort.Slice(r.homes, func(i, j int) bool { return len(r.homes[i]) > len(r.homes[j]) })
	return r
}

func (r *redactor) strict() bool { return r.mode == RedactStrict }

// path shortens $HOME to ~ wherever a path starts with it.
func (r *redactor) path(s string) string {
	for _, h := range r.homes {
		s = replaceHome(s, h)
	}
	return s
}

// replaceHome replaces home only where it is a whole leading path: at the
// start of s or after a separator such as space, '=', or ':', and followed
// by the end, '/', or '\'. "/private/home/al/x" and "/home/alice" stay.
func replaceHome(s, home string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, home)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(home)
		startOK := i == 0 || strings.ContainsRune(" \t\n\"'=:,;([{<@", rune(s[i-1]))
		endOK := end == len(s) || s[end] == '/' || s[end] == '\\'
		if !startOK || !endOK {
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString("~")
		s = s[end:]
	}
}

// text is free text that may embed paths (descriptions, kit references).
func (r *redactor) text(s string) string { return r.path(s) }

// value is an environment variable, argument, or other user-chosen value.
func (r *redactor) value(s string) string {
	if r.strict() {
		if s == "" {
			return ""
		}
		return "<redacted>"
	}
	return r.path(s)
}

// command is host or sandbox command text, which can embed tokens.
func (r *redactor) command(s string) string {
	if r.strict() {
		if s == "" {
			return ""
		}
		return "<redacted command>"
	}
	return r.path(s)
}

// secretRef formats a vault reference. Strict mode keeps only the provider
// and the kind of vault.
func (r *redactor) secretRef(ref string) string {
	if !r.strict() {
		return r.path(ref)
	}
	scheme, rest, ok := strings.Cut(ref, "://")
	if !ok {
		return "<redacted reference>"
	}
	scheme = strings.ToLower(scheme)
	switch scheme {
	case "op":
		vault, _, _ := strings.Cut(rest, "/")
		return "op://<" + vaultKind(vault) + " vault>/…"
	default:
		if !isSimpleToken(scheme) {
			return "<redacted reference>"
		}
		return scheme + "://<redacted>"
	}
}

func vaultKind(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "private", "personal", "employee", "my vault":
		return "personal"
	case "":
		return "unknown"
	}
	return "shared"
}

func isSimpleToken(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '+') {
			return false
		}
	}
	return true
}

// literalSecret never returns the value.
func (r *redactor) literalSecret() string { return "<literal value hidden>" }

// ShortenHome rewrites every leading occurrence of a home directory
// spelling in s to ~, with the same path-boundary rules the Plan uses.
func ShortenHome(s string, home string, aliases ...string) string {
	return newRedactor(RedactHome, home, aliases).path(s)
}
