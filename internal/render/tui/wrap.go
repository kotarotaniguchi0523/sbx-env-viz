package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// wrap breaks plain text into lines at most w cells wide. Words break at
// spaces; a token wider than the line (a path, a URL, a run of Japanese)
// breaks after its last "/" that fits, and only then at the width itself.
//
//declscope:shared // detail.go's doc.render and overview.go's informational card lines wrap with it
func wrap(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		flush := func() {
			out = append(out, strings.TrimRight(line, " "))
			line = ""
		}
		for _, word := range strings.SplitAfter(para, " ") {
			if ansi.StringWidth(line)+ansi.StringWidth(strings.TrimRight(word, " ")) <= w {
				line += word
				continue
			}
			long := ansi.StringWidth(strings.TrimRight(word, " ")) > w
			room := w - ansi.StringWidth(line)
			// Japanese has no spaces between words, so a run that holds
			// wide characters may break anywhere instead of leaving the
			// rest of the line empty.
			if line != "" && wide(word) && room >= 4 {
				head := kinsoku(word, keepASCIIWord(word, cutAt(word, room)))
				out = append(out, line+head)
				line, word = "", word[len(head):]
			} else if line != "" && (!long || room < w/3) {
				flush()
			}
			for ansi.StringWidth(line)+ansi.StringWidth(strings.TrimRight(word, " ")) > w {
				head := kinsoku(word, keepASCIIWord(word, cutAt(word, w-ansi.StringWidth(line))))
				out = append(out, line+head)
				line, word = "", word[len(head):]
			}
			line += word
		}
		flush()
	}
	return out
}

// keepASCIIWord moves a cut that would split an ASCII word such as
// "--skip-host-commands" back to the start of that word.
func keepASCIIWord(word, head string) string {
	if head == "" || len(head) >= len(word) || !asciiWord(word[len(head)]) || !asciiWord(head[len(head)-1]) {
		return head
	}
	i := len(head)
	for i > 0 && asciiWord(head[i-1]) {
		i--
	}
	if i == 0 {
		return head
	}
	return head[:i]
}

func asciiWord(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == '/' || b == ':' || (b >= '0' && b <= '9') || (b|0x20 >= 'a' && b|0x20 <= 'z')
}

// kinsoku moves the last character of head to the next line when the
// next line would otherwise start with closing punctuation such as "。".
func kinsoku(word, head string) string {
	rest := word[len(head):]
	r, _ := utf8.DecodeRuneInString(rest)
	if rest == "" || !strings.ContainsRune("、。，．）」』】・ー", r) {
		return head
	}
	_, size := utf8.DecodeLastRuneInString(head)
	if size >= len(head) {
		return head
	}
	return head[:len(head)-size]
}

// wide reports whether s holds a character two cells wide.
func wide(s string) bool {
	for _, r := range s {
		if ansi.StringWidth(string(r)) > 1 {
			return true
		}
	}
	return false
}

// cutAt returns the longest prefix of s that fits in w cells, ending just
// after a "/" when one falls in the second half of that prefix.
func cutAt(s string, w int) string {
	width, end, slash := 0, 0, -1
	for i, r := range s {
		rw := ansi.StringWidth(string(r))
		if width+rw > w {
			break
		}
		width += rw
		end = i + len(string(r))
		if r == '/' && width > w/2 {
			slash = end
		}
	}
	if end == 0 {
		_, size := utf8.DecodeRuneInString(s)
		return s[:size]
	}
	if slash > 0 {
		return s[:slash]
	}
	return s[:end]
}
