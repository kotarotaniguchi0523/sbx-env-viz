package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// ForTerminal returns a copy of p in which every string is safe to print:
// control characters, newlines, and bidirectional overrides are shown as
// escapes. The files being audited are untrusted, so they must not be able
// to move the cursor, recolour text, or forge an extra output line.
func ForTerminal(p *sbxenv.Plan) *sbxenv.Plan {
	b, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("plan is not serialisable: %v", err))
	}
	var cp sbxenv.Plan
	if err := json.Unmarshal(b, &cp); err != nil {
		panic(fmt.Sprintf("plan does not round-trip: %v", err))
	}
	escapeStrings(reflect.ValueOf(&cp).Elem())
	return &cp
}

func escapeStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(EscapeControl(v.String()))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			escapeStrings(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			escapeStrings(v.Index(i))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			escapeStrings(v.Elem())
		}
	}
}

// EscapeControl rewrites terminal-significant characters as visible escapes.
func EscapeControl(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case isControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isControl(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r == 0x061c, r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
