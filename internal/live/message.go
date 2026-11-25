package live

import (
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
)

// clean turns an error message into one line that is safe to show.
//
//declscope:shared // snapshot.go and discover.go put sbx and parse errors into the text they show
func clean(s string) string { return model.EscapeControl(firstLine(s)) }

// firstLine keeps the first line of s, cut to 200 bytes.
//
//declscope:shared // runner.go quotes sbx's stderr in its error with it
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
