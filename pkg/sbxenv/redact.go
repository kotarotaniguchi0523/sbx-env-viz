package sbxenv

import (
	"strings"
)

// redactor is the only way values enter a Plan. Every builder in this
// package formats display strings through it, so forgetting to redact a
// field means forgetting to copy it at all.
type redactor struct {
	mode Redaction
	home string
}

func newRedactor(mode Redaction, home string) *redactor {
	if mode == "" {
		mode = RedactStrict
	}
	home = strings.TrimRight(home, `/\`)
	if len(home) < 2 {
		home = ""
	}
	return &redactor{mode: mode, home: home}
}

func (r *redactor) strict() bool { return r.mode == RedactStrict }

// path shortens $HOME to ~ in any string that may hold host paths.
func (r *redactor) path(s string) string {
	if r.home == "" || s == "" {
		return s
	}
	out := s
	for _, h := range []string{r.home, strings.ReplaceAll(r.home, `\`, `/`)} {
		out = replacePathPrefix(out, h)
	}
	return out
}

func replacePathPrefix(s, home string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, home)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(home)
		// Only replace at a path boundary (/home/al must not match /home/alice).
		if end < len(s) && s[end] != '/' && s[end] != '\\' {
			b.WriteString(s[:end])
			s = s[end:]
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
