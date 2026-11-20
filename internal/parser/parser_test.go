package parser

import (
	"errors"
	"testing"
)

func TestParsePositionsAndScalars(t *testing.T) {
	res, err := Parse([]byte("a: off\nb:\n  c: \"1\"\n  d: [x, y]\ne: ~\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := res.Root
	if r.Get("a").Value != "off" || r.Get("a").Kind != Scalar {
		t.Errorf("a = %+v", r.Get("a"))
	}
	c := r.Get("b").Get("c")
	if c.Value != "1" || !c.Quoted || c.Pos.Line != 3 {
		t.Errorf("c = %+v", c)
	}
	if p, ok := r.Get("b").KeyPos("d"); !ok || p.Line != 4 || p.Col != 3 {
		t.Errorf("d key pos = %+v", p)
	}
	if r.Get("e").Kind != Null {
		t.Errorf("e should be null")
	}
}

func TestDuplicateKeysEverywhere(t *testing.T) {
	src := "a: 1\nb:\n  x: 1\n  x: 2\nlist:\n  - k: 1\n    k: 2\na: 3\n"
	res, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"b.x": {3, 4}, "list[0].k": {6, 7}, "a": {1, 8}}
	if len(res.Duplicates) != len(want) {
		t.Fatalf("duplicates = %+v", res.Duplicates)
	}
	for _, d := range res.Duplicates {
		w, ok := want[d.Path]
		if !ok || d.First.Line != w[0] || d.Again.Line != w[1] {
			t.Errorf("duplicate %+v, want lines %v", d, w)
		}
	}
}

func TestSingleDocument(t *testing.T) {
	if _, err := Parse([]byte("a: 1\n---\nb: 2\n")); !errors.Is(err, ErrMultipleDocuments) {
		t.Errorf("want ErrMultipleDocuments, got %v", err)
	}
	res, err := Parse(nil)
	if err != nil || res.Root.Kind != Null {
		t.Errorf("empty input: %v %+v", err, res)
	}
	if _, err := Parse([]byte("---\na: 1\n")); err != nil {
		t.Errorf("a leading document marker is one document: %v", err)
	}
}

func TestAliasesResolve(t *testing.T) {
	res, err := Parse([]byte("base: &b\n  x: 1\nother: *b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.Get("other").Get("x").Value != "1" {
		t.Error("alias should resolve to the anchored mapping")
	}
}

func TestSyntaxHeader(t *testing.T) {
	if !HasSyntaxHeader([]byte("# syntax=docker/sandbox-kit:3\nschemaVersion: \"3\"\n")) {
		t.Error("header not detected")
	}
	if HasSyntaxHeader([]byte("schemaVersion: \"3\"\n")) {
		t.Error("false positive")
	}
}
