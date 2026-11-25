package parser_test

import (
	"errors"
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
	doc := []byte("a: 1\nb:\n  c: \"x\"\n  d: [1, 2]\ne: ~\n")

	// Act
	res, err := parser.Parse(doc)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	c := res.Root.Get("b").Get("c")
	if c.Pos.Line != 3 || !c.Quoted {
		t.Errorf("c = %+v", c)
	}
	if p, ok := res.Root.Get("b").KeyPos("d"); !ok || p != (parser.Pos{Line: 4, Col: 3}) {
		t.Errorf("d key position = %+v", p)
	}
	if res.Root.Get("e").Kind != parser.Null || res.Root.Get("missing") != nil {
		t.Error("null and missing values must be distinguishable")
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

func TestSyntaxErrorsCarryALineButNoContent(t *testing.T) {
	t.Parallel()
	// Arrange
	doc := []byte("a: 1\nb: \"SECRETVALUE\n")

	// Act
	_, err := parser.Parse(doc)

	// Assert
	var se *parser.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("want *parser.SyntaxError, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Errorf("error quotes content: %v", err)
	}
}

func TestAliasesResolveToTheirAnchor(t *testing.T) {
	t.Parallel()
	// Act
	res, err := parser.Parse([]byte("base: &b\n  x: 1\nother: *b\n"))

	// Assert
	if err != nil || res.Root.Get("other").Get("x").Value != "1" {
		t.Errorf("alias did not resolve: %v", err)
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
