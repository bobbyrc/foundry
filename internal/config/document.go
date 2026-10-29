package config

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Change is one edit to the config file, sent by the web UI. A path walks the file's YAML tree:
// each element is a mapping key, or a decimal index when the node is a list.
type Change struct {
	Op    string   `json:"op"`              // "set" or "delete"
	Path  []string `json:"path"`            // mapping keys; a decimal string indexes a sequence
	Value any      `json:"value,omitempty"` // for set: a JSON-decoded value (map[string]any, []any, string, float64, bool, nil)
}

// Problem is one validation message, with the setting it is about.
type Problem struct {
	Path    string `json:"path"` // dotted setting path, "" when the error names none
	Message string `json:"message"`
}

// maxDocumentNodes caps how many YAML nodes DocumentTree visits. An alias ("*a") is expanded at
// each use, so a few hundred bytes of YAML can describe billions of nodes. The cap stops that
// before it exhausts memory. A real config is far below it.
const maxDocumentNodes = 100_000

var errDocumentTooLarge = errors.New("config document is too large or has too many aliases")

// DocumentTree parses the file WITHOUT ${NAME} expansion (so references stay as written) into JSON-compatible values: map[string]any, []any, string, int64 or float64, bool, nil. An empty file gives map[string]any{}.
func DocumentTree(text []byte) (any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	b := &treeBuilder{}
	v, err := b.value(&doc)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return map[string]any{}, nil
	}
	return v, nil
}

// treeBuilder converts a YAML node tree into JSON-compatible values, counting the nodes it visits.
type treeBuilder struct {
	visited int
}

// value converts one YAML node into a JSON-compatible value.
func (b *treeBuilder) value(n *yaml.Node) (any, error) {
	b.visited++
	if b.visited > maxDocumentNodes {
		return nil, errDocumentTooLarge
	}
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return b.value(n.Content[0])
	case yaml.AliasNode:
		return b.value(n.Alias)
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: mapping keys must be scalars", k.Line)
			}
			v, err := b.value(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		list := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := b.value(c)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case yaml.ScalarNode:
		return scalarValue(n)
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// scalarValue decodes a scalar by its YAML type. Anything that is not null, bool, int or float
// (strings, timestamps) is kept as the text as written.
func scalarValue(n *yaml.Node) (any, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, err
		}
		return b, nil
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return i, nil
		}
		// Too large for int64: keep it as a float rather than failing the whole file.
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		return f, nil
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		return f, nil
	}
	return n.Value, nil
}

// ApplyChanges applies changes in order to the YAML node tree and encodes it again; comments are kept, formatting may be normalised.
func ApplyChanges(text []byte, changes []Change) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode {
		doc = yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if root := doc.Content[0]; isNull(root) {
		// An empty or "~" document: make it a mapping in place so comments on it survive.
		root.Kind, root.Tag, root.Value, root.Style = yaml.MappingNode, "!!map", "", 0
	}
	root := doc.Content[0]

	for _, c := range changes {
		if len(c.Path) == 0 {
			return nil, errors.New("change has an empty path")
		}
		var err error
		switch c.Op {
		case "set":
			err = setAt(root, c.Path, c.Value)
		case "delete":
			err = deleteAt(root, c.Path)
		default:
			err = fmt.Errorf("%s: unknown op %q", dotted(c.Path), c.Op)
		}
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buf.Bytes(), nil
}

// setAt sets the value at path under root, creating missing mappings on the way.
func setAt(root *yaml.Node, path []string, value any) error {
	parent := root
	var parentKey *yaml.Node // the key whose value is parent; nil at the document root
	inFlow := false          // a node above parent is flow style, so parent stays as written
	for depth, seg := range path[:len(path)-1] {
		child, err := step(parent, parentKey, inFlow, seg, dotted(path[:depth+1]), true)
		if err != nil {
			return err
		}
		parentKey = keyNode(parent, seg)
		inFlow = inFlow || parent.Style&yaml.FlowStyle != 0
		parent = child
	}
	var val yaml.Node
	if err := val.Encode(normalizeNumbers(value)); err != nil {
		return fmt.Errorf("%s: encode value: %w", dotted(path), err)
	}
	blockStyle(&val)
	last := path[len(path)-1]
	at := dotted(path)
	switch parent.Kind {
	case yaml.MappingNode:
		if i := mappingIndex(parent, last); i >= 0 {
			copyComments(parent.Content[i+1], &val)
			parent.Content[i+1] = &val
			return nil
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: last}
		if !inFlow {
			openBlock(parent, parentKey)
		}
		parent.Content = append(parent.Content, key, &val)
		return nil
	case yaml.SequenceNode:
		i, ok := listIndex(last)
		switch {
		case !ok:
			return fmt.Errorf("%s: %q is not a list index", at, last)
		case i == len(parent.Content):
			if !inFlow {
				openBlock(parent, parentKey)
			}
			parent.Content = append(parent.Content, &val)
			return nil
		case i < len(parent.Content):
			copyComments(parent.Content[i], &val)
			parent.Content[i] = &val
			return nil
		}
		return fmt.Errorf("%s: index %d is past the end of the list", at, i)
	}
	return fmt.Errorf("%s: parent is not a mapping or list", at)
}

// deleteAt removes the key or list element at path. A missing path is an error.
func deleteAt(root *yaml.Node, path []string) error {
	parent := root
	for depth, seg := range path[:len(path)-1] {
		child, err := step(parent, nil, false, seg, dotted(path[:depth+1]), false)
		if err != nil {
			return err
		}
		parent = child
	}
	last := path[len(path)-1]
	at := dotted(path)
	switch parent.Kind {
	case yaml.MappingNode:
		i := mappingIndex(parent, last)
		if i < 0 {
			return fmt.Errorf("%s: not found", at)
		}
		parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
		return nil
	case yaml.SequenceNode:
		i, ok := listIndex(last)
		if !ok || i >= len(parent.Content) {
			return fmt.Errorf("%s: not found", at)
		}
		parent.Content = append(parent.Content[:i], parent.Content[i+1:]...)
		return nil
	}
	return fmt.Errorf("%s: parent is not a mapping or list", at)
}

// step returns the child of parent named seg. With create, a missing mapping key is added as an
// empty mapping, and a null value is turned into one; without it, a missing child is an error.
// parentKey is the key that holds parent, passed on to openBlock. inFlow says a node above parent
// is flow style, in which case parent is left as written.
func step(parent, parentKey *yaml.Node, inFlow bool, seg, at string, create bool) (*yaml.Node, error) {
	switch parent.Kind {
	case yaml.MappingNode:
		if i := mappingIndex(parent, seg); i >= 0 {
			v := parent.Content[i+1]
			if create && isNull(v) {
				v.Kind, v.Tag, v.Value, v.Style = yaml.MappingNode, "!!map", "", 0
			}
			return v, nil
		}
		if !create {
			return nil, fmt.Errorf("%s: not found", at)
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg}
		v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if !inFlow {
			openBlock(parent, parentKey)
		}
		parent.Content = append(parent.Content, key, v)
		return v, nil
	case yaml.SequenceNode:
		i, ok := listIndex(seg)
		if !ok || i >= len(parent.Content) {
			return nil, fmt.Errorf("%s: not found", at)
		}
		return parent.Content[i], nil
	}
	return nil, fmt.Errorf("%s: parent is not a mapping or list", at)
}

// mappingIndex returns the index of key's key node in the mapping's Content, or -1. The value
// sits at the next index.
func mappingIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// listIndex parses a decimal list index.
func listIndex(seg string) (int, bool) {
	if seg == "" || strings.Trim(seg, "0123456789") != "" {
		return 0, false
	}
	i, err := strconv.Atoi(seg)
	return i, err == nil
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// blockStyle makes a value that onlet writes read as block YAML, the form people edit by hand:
// mappings and lists are block style, and a multi-line string is a literal block, one text line
// per file line. yaml.v3 writes a multi-line string as a quoted line with \n escapes when it sits
// inside a flow mapping, so keeping the value out of flow style matters.
func blockStyle(n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		n.Style = 0
	case yaml.ScalarNode:
		if n.ShortTag() == "!!str" && strings.Contains(n.Value, "\n") {
			n.Style = yaml.LiteralStyle
		}
	}
	for _, c := range n.Content {
		blockStyle(c)
	}
}

// openBlock turns a flow mapping or list (such as "agents: {}") into block style. Call it only on
// a node the change is adding an entry to, so that untouched flow nodes keep their style. A node
// nested inside a flow ancestor stays flow, because opening the ancestors would restyle siblings
// the change does not touch. When the flow node has a line comment, it moves to its key (key is
// nil at the document root), so the comment stays on the "agents:" line and not on the next key.
// Callers skip openBlock when a flow ancestor exists; see setAt.
func openBlock(n, key *yaml.Node) {
	if n.Style&yaml.FlowStyle == 0 {
		return
	}
	n.Style &^= yaml.FlowStyle
	if key != nil && n.LineComment != "" && key.LineComment == "" {
		key.LineComment, n.LineComment = n.LineComment, ""
	}
}

// keyNode returns the key node for seg in a mapping parent, or nil.
func keyNode(parent *yaml.Node, seg string) *yaml.Node {
	if parent.Kind != yaml.MappingNode {
		return nil
	}
	if i := mappingIndex(parent, seg); i >= 0 {
		return parent.Content[i]
	}
	return nil
}

// copyComments keeps the comments of the value being replaced, so a comment beside a setting
// survives when the setting changes.
func copyComments(old, replacement *yaml.Node) {
	replacement.HeadComment = old.HeadComment
	replacement.LineComment = old.LineComment
	replacement.FootComment = old.FootComment
}

// normalizeNumbers turns whole-number float64 values into int64, so that 15 is written as 15
// and not 15.0. JSON has one number type, so the UI cannot tell them apart.
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
		return x
	}
	return v
}

// dotted joins path elements with dots, for error messages.
func dotted(path []string) string {
	return strings.Join(path, ".")
}

// problemPrefix matches the setting path that Validate puts at the start of each message, such
// as "projects.web.repo: " or "server.public_url \"...\" ". It accepts only the top-level keys of
// the file, so a YAML syntax error or another message without a path is left whole.
var problemPrefix = regexp.MustCompile(`^((?:server|limits|harnesses|agents|routing|policies|projects|loops|notify|self_project)[^\s:]*)(?::\s|\s)`)

// ProblemsFrom turns an error from Parse or Validate into one Problem per message. Validate joins errors with errors.Join and prefixes each with the setting path (read validate.go to see the exact prefix format and match it); a message without such a prefix (e.g. a YAML syntax error) gets Path "".
func ProblemsFrom(err error) []Problem {
	if err == nil {
		return nil
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		var out []Problem
		for _, sub := range multi.Unwrap() {
			out = append(out, ProblemsFrom(sub)...)
		}
		return out
	}
	msg := err.Error()
	if m := problemPrefix.FindStringSubmatch(msg); m != nil {
		return []Problem{{Path: m[1], Message: msg[len(m[0]):]}}
	}
	return []Problem{{Message: msg}}
}

// RestartRequired lists settings read only at start-up that differ between old and new: "server.addr", "server.data_dir", "server.allowed_hosts", "notify" (reflect.DeepEqual; in that order).
func RestartRequired(old, new *Config) []string {
	if old == nil || new == nil {
		return nil
	}
	var out []string
	if old.Server.Addr != new.Server.Addr {
		out = append(out, "server.addr")
	}
	if old.Server.DataDir != new.Server.DataDir {
		out = append(out, "server.data_dir")
	}
	if !reflect.DeepEqual(old.Server.AllowedHosts, new.Server.AllowedHosts) {
		out = append(out, "server.allowed_hosts")
	}
	if !reflect.DeepEqual(old.Notify, new.Notify) {
		out = append(out, "notify")
	}
	return out
}
