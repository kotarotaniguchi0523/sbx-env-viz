package parser_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kotarotaniguchi0523/sbxray/internal/parser"
)

func TestScalarsAreKeptVerbatim(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"off": "off", "yes": "yes", "007": "007", "\"1\"": "1", "1.0": "1.0", "~/x": "~/x"}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			// Arrange
			doc := []byte("v: " + src + "\n")

			// Act
			res, err := parser.Parse(doc)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Root.Get("v"); got.Kind != parser.Scalar || got.Value != want {
				t.Errorf("v = %+v, want scalar %q", got, want)
			}
		})
	}
}

func TestPositionsAreOneBased(t *testing.T) {
	t.Parallel()
	// Arrange
	doc := []byte("a: 1\nb:\n  c: \"x\"\n  d: [1, 2]\n")

	// Act
	res, err := parser.Parse(doc)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := res.Root.KeyPos("a"); !ok || p != (parser.Pos{Line: 1, Col: 1}) {
		t.Errorf("first key position = %+v, found=%v", p, ok)
	}
	c := res.Root.Get("b").Get("c")
	if c.Pos.Line != 3 {
		t.Errorf("c = %+v", c)
	}
	if p, ok := res.Root.Get("b").KeyPos("d"); !ok || p != (parser.Pos{Line: 4, Col: 3}) {
		t.Errorf("d key position = %+v", p)
	}
}

func TestQuotedScalar(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		quoted    bool
	}{{"quoted", `"x"`, true}, {"plain", "x", false}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := []byte("v: " + tc.src)

			res, err := parser.Parse(doc)

			if err != nil {
				t.Fatal(err)
			}
			if got := res.Root.Get("v"); got == nil || got.Quoted != tc.quoted {
				t.Fatalf("scalar = %+v, want quoted=%v", got, tc.quoted)
			}
		})
	}
}

func TestExplicitNullDiffersFromMissingKey(t *testing.T) {
	doc := []byte("e: ~")

	res, err := parser.Parse(doc)

	if err != nil {
		t.Fatal(err)
	}
	if e := res.Root.Get("e"); e == nil || e.Kind != parser.Null || res.Root.Get("missing") != nil {
		t.Fatal("expected an explicit null and an absent key")
	}
}

func TestDuplicateKeysAreFoundInEveryMapping(t *testing.T) {
	t.Parallel()
	// Arrange
	doc := []byte("a: 1\nb:\n  x: 1\n  x: 2\nlist:\n  - k: 1\n    k: 2\na: 3\n")

	// Act
	res, err := parser.Parse(doc)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]int{}
	for _, d := range res.Duplicates {
		got[d.Path] = [2]int{d.First.Line, d.Again.Line}
	}
	want := map[string][2]int{"a": {1, 8}, "b.x": {3, 4}, "list[0].k": {6, 7}}
	if len(got) != len(want) {
		t.Fatalf("duplicates = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s at lines %v, want %v", k, got[k], v)
		}
	}
}

func TestOnlyOneDocumentIsAccepted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  string
		ok   bool
	}{
		{"single", "a: 1\n", true},
		{"leading marker", "---\na: 1\n", true},
		{"empty", "", true},
		{"two documents", "a: 1\n---\nb: 2\n", false},
		{"broken second document", "a: 1\n---\nb: [\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			_, err := parser.Parse([]byte(tc.doc))

			// Assert
			if tc.ok && err != nil {
				t.Errorf("unexpected error %v", err)
			}
			if !tc.ok && !errors.Is(err, parser.ErrMultipleDocuments) {
				t.Errorf("want ErrMultipleDocuments, got %v", err)
			}
		})
	}
}

func TestSyntaxErrorsHideInputAndPreserveKnownLine(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		line      int
	}{
		{"known line", "a: ok\nb: value: PRIVATE\n", 2},
		{"unknown line", "a: *PRIVATE\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.doc)

			_, err := parser.Parse(input)

			var se *parser.SyntaxError
			if !errors.As(err, &se) || se.Line != tc.line {
				t.Fatalf("syntax error = %v, want line %d", err, tc.line)
			}
			message := err.Error()
			if se.Msg == "" || !strings.Contains(message, se.Msg) || strings.Contains(message, "PRIVATE") {
				t.Errorf("expected a diagnostic without input content, got %q", message)
			}
			location := strings.TrimSuffix(message, se.Msg)
			if tc.line > 0 && !strings.Contains(location, fmt.Sprint(tc.line)) || tc.line == 0 && strings.Contains(location, "0") {
				t.Errorf("unexpected location in %q", message)
			}
		})
	}
}

func TestAliasesAndMergeSequencePreservePrecedence(t *testing.T) {
	res, err := parser.Parse([]byte("a: &a {shared: first, x: 1}\nb: &b {shared: later, y: 2}\nseq: &seq [*a, *b]\nmerged: {<<: *seq, x: override}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"shared": "first", "x": "override", "y": "2"} {
		got := res.Root.Get("merged").Get(k)
		if got == nil || got.Value != want {
			t.Errorf("merged.%s = %+v, want %q", k, got, want)
		}
	}
	if got := res.Root.Get("seq").Items[0].Get("x"); got == nil || got.Value != "1" {
		t.Fatalf("plain alias: %+v", got)
	}
}

func TestAliasDepthBoundary(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("base: &a0 value\n")
	for i := 1; i <= 34; i++ {
		fmt.Fprintf(&doc, "level%d: &a%d [*a%d]\n", i, i, i-1)
	}
	res, err := parser.Parse([]byte(doc.String()))
	if err != nil {
		t.Fatal(err)
	}
	for _, depth := range []int{33, 34} {
		n := res.Root.Get(fmt.Sprintf("level%d", depth))
		for n.Kind == parser.Seq {
			n = n.Items[0]
		}
		if depth == 33 && n.Value != "value" || depth == 34 && n.Kind != parser.Null {
			t.Errorf("depth %d: %+v", depth, n)
		}
	}
}

func TestKitSyntaxHeaderDetection(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"# syntax=docker/sandbox-kit:3\nschemaVersion: \"3\"\n": true,
		"  # syntax=docker/sandbox-kit:3\n":                     true,
		"schemaVersion: \"3\"\n# syntax=docker/sandbox-kit:3\n": false,
		"": false,
	}
	for doc, want := range cases {
		// Act / Assert
		if got := parser.HasSyntaxHeader([]byte(doc)); got != want {
			t.Errorf("HasSyntaxHeader(%q) = %v, want %v", doc, got, want)
		}
	}
}

func TestAliasExpansionBudget(t *testing.T) {
	for _, merge := range []bool{false, true} {
		doc := "a: &a {value: 1}\n"
		for ch := 'b'; ch <= 'o'; ch++ {
			name, prev := string(ch), string(ch-1)
			pair := "[*" + prev + ", *" + prev + "]"
			if merge {
				pair = "{<<: " + pair + "}"
			}
			doc += name + ": &" + name + " " + pair + "\n"
		}
		_, err := parser.Parse([]byte(doc))
		var syntax *parser.SyntaxError
		if !errors.As(err, &syntax) || !strings.Contains(syntax.Msg, "expansion limit") {
			t.Fatalf("merge=%v: %v", merge, err)
		}
	}
}

func TestAliasExpansionAcceptsExactlyTheBudget(t *testing.T) {
	// Expanding the anchored sequence visits its root and each scalar once.
	for _, tc := range []struct {
		name     string
		count    int
		accepted bool
	}{
		{"at limit", 9999, true}, {"one above limit", 10000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte("base: &base [" + strings.Repeat("x,", tc.count) + "]\ncopy: *base\n")

			res, err := parser.Parse(input)

			if tc.accepted {
				if err != nil {
					t.Fatal(err)
				}
				copied := res.Root.Get("copy")
				if copied == nil || len(copied.Items) != tc.count {
					t.Fatal("expected the complete expanded sequence")
				}
				return
			}
			var se *parser.SyntaxError
			if !errors.As(err, &se) || !strings.Contains(se.Msg, "expansion limit") {
				t.Fatalf("want expansion limit error, got %v", err)
			}
		})
	}
}
