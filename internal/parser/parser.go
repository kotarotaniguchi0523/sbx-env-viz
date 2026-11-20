// Package parser turns YAML bytes into a small position-carrying tree.
//
// It wraps go.yaml.in/yaml/v3 so that yaml.Node never leaks into public
// types. Scalars keep their source text verbatim (so `skills: off` stays the
// string "off"), every mapping is scanned for duplicate keys, and documents
// with more than one YAML document are rejected.
package parser

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Kind is the shape of a Node.
type Kind int

const (
	// Null is an explicit `~`/`null` or an empty value.
	Null Kind = iota
	// Scalar is a string, number, or boolean kept as source text.
	Scalar
	// Map is a mapping with ordered keys.
	Map
	// Seq is a sequence.
	Seq
)

func (k Kind) String() string {
	switch k {
	case Scalar:
		return "scalar"
	case Map:
		return "map"
	case Seq:
		return "seq"
	default:
		return "null"
	}
}

// Pos is a 1-based source position.
type Pos struct {
	Line int
	Col  int
}

// Node is a parsed YAML value with its position.
type Node struct {
	Kind Kind
	// Value is the scalar text exactly as YAML decoded it (no type coercion).
	Value string
	// Tag is the resolved YAML tag, such as "!!str" or "!!int".
	Tag string
	// Quoted reports whether the scalar was written in quotes.
	Quoted bool
	Pos    Pos
	// Keys holds mapping entries in source order.
	Keys []KeyValue
	// Items holds sequence entries.
	Items []*Node
}

// KeyValue is one mapping entry.
type KeyValue struct {
	Key    string
	KeyPos Pos
	Value  *Node
}

// Get returns the value for key, or nil.
func (n *Node) Get(key string) *Node {
	if n == nil || n.Kind != Map {
		return nil
	}
	for i := len(n.Keys) - 1; i >= 0; i-- {
		if n.Keys[i].Key == key {
			return n.Keys[i].Value
		}
	}
	return nil
}

// KeyPos returns the position of key within a mapping.
func (n *Node) KeyPos(key string) (Pos, bool) {
	if n == nil || n.Kind != Map {
		return Pos{}, false
	}
	for i := len(n.Keys) - 1; i >= 0; i-- {
		if n.Keys[i].Key == key {
			return n.Keys[i].KeyPos, true
		}
	}
	return Pos{}, false
}

// DuplicateKey reports a key that appears twice in one mapping.
type DuplicateKey struct {
	Key   string
	Path  string
	First Pos
	Again Pos
}

// Result is the outcome of parsing one document.
type Result struct {
	Root       *Node
	Duplicates []DuplicateKey
}

// ErrMultipleDocuments is returned when the input holds more than one document.
var ErrMultipleDocuments = errors.New("multiple YAML documents are not allowed; an environment file holds exactly one")

// SyntaxError is a YAML syntax error without the offending source text.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("YAML syntax error at line %d: %s", e.Line, e.Msg)
	}
	return "YAML syntax error: " + e.Msg
}

// Parse parses exactly one YAML document. Empty input yields a Null root.
func Parse(data []byte) (*Result, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return &Result{Root: &Node{Kind: Null, Pos: Pos{Line: 1, Col: 1}}}, nil
	}
	if err != nil {
		return nil, sanitizeYAMLError(err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, ErrMultipleDocuments
	} else if !errors.Is(err, io.EOF) {
		// A second document that fails to parse is still a second document.
		if strings.Contains(string(data), "\n---") {
			return nil, ErrMultipleDocuments
		}
		return nil, sanitizeYAMLError(err)
	}
	c := &converter{}
	var root *Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = c.convert(doc.Content[0], "", 0)
	} else {
		root = &Node{Kind: Null, Pos: Pos{Line: 1, Col: 1}}
	}
	return &Result{Root: root, Duplicates: c.dups}, nil
}

// sanitizeYAMLError keeps only the line number and a message that never
// quotes document content, so a secret on a broken line cannot reach stderr.
func sanitizeYAMLError(err error) error {
	msg := err.Error()
	line := 0
	if i := strings.Index(msg, "line "); i >= 0 {
		fmt.Sscanf(msg[i:], "line %d", &line)
	}
	reason := "the document could not be parsed"
	switch {
	case strings.Contains(msg, "mapping values are not allowed"):
		reason = "mapping values are not allowed in this context"
	case strings.Contains(msg, "did not find expected key"):
		reason = "did not find expected key (check indentation)"
	case strings.Contains(msg, "found character that cannot start any token"):
		reason = "found a character that cannot start any token (tabs are not allowed for indentation)"
	case strings.Contains(msg, "could not find expected ':'"):
		reason = "could not find expected ':'"
	case strings.Contains(msg, "unknown anchor"):
		reason = "unknown anchor"
	case strings.Contains(msg, "did not find expected"):
		reason = "unexpected structure (check indentation and list markers)"
	case strings.Contains(msg, "found unexpected end of stream"):
		reason = "unexpected end of stream (unclosed quote or bracket)"
	}
	return &SyntaxError{Line: line, Msg: reason}
}

type converter struct {
	dups []DuplicateKey
}

const maxAliasDepth = 32

func (c *converter) convert(y *yaml.Node, path string, depth int) *Node {
	if y.Kind == yaml.AliasNode {
		if depth > maxAliasDepth || y.Alias == nil {
			return &Node{Kind: Null, Pos: Pos{y.Line, y.Column}}
		}
		n := c.convert(y.Alias, path, depth+1)
		return n
	}
	pos := Pos{Line: y.Line, Col: y.Column}
	switch y.Kind {
	case yaml.ScalarNode:
		tag := y.ShortTag()
		if tag == "!!null" {
			return &Node{Kind: Null, Pos: pos, Tag: tag}
		}
		return &Node{
			Kind:   Scalar,
			Value:  y.Value,
			Tag:    tag,
			Quoted: y.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0,
			Pos:    pos,
		}
	case yaml.MappingNode:
		n := &Node{Kind: Map, Pos: pos}
		seen := map[string]Pos{}
		var merged []*yaml.Node
		for i := 0; i+1 < len(y.Content); i += 2 {
			k, v := y.Content[i], y.Content[i+1]
			if k.ShortTag() == "!!merge" {
				merged = append(merged, v)
				continue
			}
			key := k.Value
			kp := Pos{Line: k.Line, Col: k.Column}
			childPath := joinPath(path, key)
			if first, ok := seen[key]; ok {
				c.dups = append(c.dups, DuplicateKey{Key: key, Path: childPath, First: first, Again: kp})
			} else {
				seen[key] = kp
			}
			n.Keys = append(n.Keys, KeyValue{Key: key, KeyPos: kp, Value: c.convert(v, childPath, depth)})
		}
		// A merge key (<<) adds the keys of the referenced mapping, or of each
		// mapping in a sequence with earlier ones first, that the mapping does
		// not set itself. Explicit keys win wherever they appear.
		for _, src := range merged {
			sources := []*yaml.Node{src}
			if deref(src).Kind == yaml.SequenceNode {
				sources = deref(src).Content
			}
			for _, m := range sources {
				mn := c.convert(m, path, depth+1)
				for _, kv := range mn.Keys {
					if _, ok := seen[kv.Key]; !ok {
						seen[kv.Key] = kv.KeyPos
						n.Keys = append(n.Keys, kv)
					}
				}
			}
		}
		return n
	case yaml.SequenceNode:
		n := &Node{Kind: Seq, Pos: pos}
		for i, it := range y.Content {
			n.Items = append(n.Items, c.convert(it, fmt.Sprintf("%s[%d]", path, i), depth))
		}
		return n
	}
	return &Node{Kind: Null, Pos: pos}
}

func deref(y *yaml.Node) *yaml.Node {
	for i := 0; y.Kind == yaml.AliasNode && y.Alias != nil && i < maxAliasDepth; i++ {
		y = y.Alias
	}
	return y
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// HasSyntaxHeader reports whether data starts with a kit descriptor syntax line.
func HasSyntaxHeader(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return bytes.HasPrefix(bytes.TrimSpace(first), []byte("# syntax=docker/sandbox-kit"))
}
