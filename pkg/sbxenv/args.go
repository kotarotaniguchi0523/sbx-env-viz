//declscope:shared // args.go is the argument model env.args (resolve.go) and kit args (kits.go) both use
package sbxenv

import "strings"

// argDecl is one declared argument of an environment file or a kit.
type argDecl struct {
	name        string
	def         *string
	defOrigin   Origin
	required    bool
	enum        []string
	pattern     string
	description string
	origin      Origin
	// defNode carries the default's merge history across layers.
	defNode *vnode
	// export, buildArg, and buildSecret are kit-arg delivery mechanisms.
	export, buildArg, buildSecret string
}

// argLine is one NAME=VALUE pair with its origin.
type argLine struct {
	name, value string
	origin      Origin
	from        string
}

// parseArgFile reads an args file of NAME=VALUE lines; bad holds the origins
// of the lines that are not.
func parseArgFile(content []byte, src string, from string) ([]argLine, []Origin) {
	var out []argLine
	var bad []Origin
	for i, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name, value, ok := strings.Cut(t, "=")
		if !ok || strings.TrimSpace(name) == "" {
			bad = append(bad, Origin{SourceID: src, Line: i + 1})
			continue
		}
		out = append(out, argLine{name: strings.TrimSpace(name), value: value, origin: Origin{SourceID: src, Line: i + 1}, from: from})
	}
	return out, bad
}
