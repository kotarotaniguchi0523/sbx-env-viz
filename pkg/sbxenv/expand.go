//declscope:shared // expand.go is the ${{ }} expression utility resolve.go, kits.go, merge.go and caprules.go use
package sbxenv

import (
	"regexp"
	"strings"
)

// exprRe matches ${{ ... }} with an optional extra leading $ (the escape).
//
//declscope:private // the expression grammar stays inside expand.go
var exprRe = regexp.MustCompile(`\$?\$\{\{\s*([^{}]*?)\s*\}\}`)

var envArgNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var kitArgNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// exprResult is what an expression resolver reports.
type exprResult struct {
	value string
	ok    bool
	// keep leaves the expression text in place (statically unknowable).
	keep bool
}

type expansion struct {
	out string
	// refs lists every expression body found (escaped ones excluded).
	refs []string
	// unresolved lists expression bodies the resolver rejected.
	unresolved []string
	// kept lists expression bodies left in place on purpose.
	kept []string
}

func hasExpr(s string) bool { return strings.Contains(s, "${{") }

// expandString substitutes expressions once; substituted text is never
// expanded again.
func expandString(s string, resolve func(expr string) exprResult) expansion {
	if !hasExpr(s) {
		return expansion{out: s}
	}
	var e expansion
	e.out = exprRe.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "$$") {
			return m[1:]
		}
		body := strings.TrimSpace(exprRe.FindStringSubmatch(m)[1])
		e.refs = append(e.refs, body)
		r := resolve(body)
		switch {
		case r.keep:
			e.kept = append(e.kept, body)
			return m
		case !r.ok:
			e.unresolved = append(e.unresolved, body)
			return m
		}
		return r.value
	})
	return e
}

// envArgRef returns NAME for "env.args.NAME".
func envArgRef(body string) (string, bool) {
	name, ok := strings.CutPrefix(body, "env.args.")
	if !ok || !envArgNameRe.MatchString(name) {
		return "", false
	}
	return name, true
}

// kitArgRef returns NAME for "kit.args.NAME".
func kitArgRef(body string) (string, bool) {
	name, ok := strings.CutPrefix(body, "kit.args.")
	if !ok || !kitArgNameRe.MatchString(name) {
		return "", false
	}
	return name, true
}
