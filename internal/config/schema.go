package config

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"sync"
)

// configSource is config.go itself. Help text is read from its comments, so the UI's help can
// never drift from the code's own documentation.
//
//go:embed config.go
var configSource string

// SchemaNode describes one setting of foundry.yaml for the web UI's editor.
type SchemaNode struct {
	Key    string       `json:"key"`              // YAML key; "" for map values and list elements
	Type   string       `json:"type"`             // section | map | list | string | int | float | bool | duration | any (a free-form value, such as a deployer's options map, that the UI edits as YAML text)
	Help   string       `json:"help,omitempty"`   // the field's doc comment from config.go, as one paragraph
	Fields []SchemaNode `json:"fields,omitempty"` // section: fields in declaration order
	Item   *SchemaNode  `json:"item,omitempty"`   // map and list: the value or element schema
}

var (
	schemaOnce sync.Once
	schemaRoot SchemaNode
)

// Schema describes every setting in Config. It is built once, from the struct's reflected
// types and the doc comments in config.go. A field of a type it does not know panics, so a new
// config type cannot be left out of the editor silently.
func Schema() SchemaNode {
	schemaOnce.Do(func() {
		b := schemaBuilder{help: parseHelp(configSource)}
		schemaRoot = SchemaNode{Type: "section", Fields: b.structNode(reflect.TypeOf(Config{}), "Config").Fields}
	})
	return schemaRoot
}

// durationType is config.Duration, which is written as "15m" and is not an ordinary struct.
var durationType = reflect.TypeOf(Duration{})

type schemaBuilder struct {
	help map[string]string // "Type.Field" -> help text
}

// structNode builds a section from t's fields. base names the struct for help lookups; an
// anonymous struct has no name of its own, so its caller passes one such as "Loops.IssueIntake".
func (b schemaBuilder) structNode(t reflect.Type, base string) SchemaNode {
	if t.Name() != "" {
		base = t.Name()
	}
	var fields []SchemaNode
	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if hasOption(opts, "inline") {
			if sf.Type.Kind() != reflect.Struct {
				panic(fmt.Sprintf("config schema: %s.%s is inline but not a struct", base, sf.Name))
			}
			fields = append(fields, b.structNode(sf.Type, base).Fields...)
			continue
		}
		if name == "" {
			name = strings.ToLower(sf.Name)
		}
		helpKey := base + "." + sf.Name
		node := b.typeNode(sf.Type, helpKey)
		node.Key = name
		node.Help = leadWithYAMLKey(b.help[helpKey], sf.Name, name)
		fields = append(fields, node)
	}
	return SchemaNode{Type: "section", Fields: fields}
}

// typeNode builds the schema for a field, map value or list element of type t. helpKey is the
// owner and field of the setting, used when t is an anonymous struct.
func (b schemaBuilder) typeNode(t reflect.Type, helpKey string) SchemaNode {
	if t == durationType {
		return SchemaNode{Type: "duration"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return b.typeNode(t.Elem(), helpKey)
	case reflect.Struct:
		return b.structNode(t, helpKey)
	case reflect.Interface:
		return SchemaNode{Type: "any"}
	case reflect.Map:
		if t.Elem().Kind() == reflect.Interface {
			// A map[string]any is free-form: the UI edits it as a whole, not key by key.
			return SchemaNode{Type: "any"}
		}
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("config schema: %s has a map key type %s, want string", helpKey, t.Key()))
		}
		item := b.typeNode(t.Elem(), helpKey)
		return SchemaNode{Type: "map", Item: &item}
	case reflect.Slice:
		item := b.typeNode(t.Elem(), helpKey)
		return SchemaNode{Type: "list", Item: &item}
	case reflect.String:
		return SchemaNode{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return SchemaNode{Type: "int"}
	case reflect.Float32, reflect.Float64:
		return SchemaNode{Type: "float"}
	case reflect.Bool:
		return SchemaNode{Type: "bool"}
	}
	panic(fmt.Sprintf("config schema: %s has unsupported type %s", helpKey, t))
}

// leadWithYAMLKey rewrites help text that opens with the Go field name, such as "AllowedHosts are
// extra ...", so that it opens with the setting as the user sees it in the file: "Allowed hosts are
// extra ...". The key is humanized: underscores become spaces and the first letter is capitalised.
func leadWithYAMLKey(help, goName, key string) string {
	rest, ok := strings.CutPrefix(help, goName+" ")
	if !ok {
		return help
	}
	label := strings.ReplaceAll(key, "_", " ")
	return strings.ToUpper(label[:1]) + label[1:] + " " + rest
}

// hasOption reports whether a yaml tag's options (the part after the first comma) include opt.
func hasOption(opts, opt string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == opt {
			return true
		}
	}
	return false
}

// parseHelp maps "Type.Field" to the field's doc comment, or its trailing line comment when it
// has no doc comment, as one paragraph. Anonymous struct fields are keyed as "Type.Field.Sub".
func parseHelp(src string) map[string]string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "config.go", src, parser.ParseComments)
	if err != nil {
		panic("config schema: parse config.go: " + err.Error())
	}
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				collectHelp(ts.Name.Name, st, out)
			}
		}
	}
	return out
}

func collectHelp(prefix string, st *ast.StructType, out map[string]string) {
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // an embedded struct has no name of its own to document
		}
		text := fieldText(field)
		for _, name := range field.Names {
			key := prefix + "." + name.Name
			if text != "" {
				out[key] = text
			}
			if inner, ok := field.Type.(*ast.StructType); ok {
				collectHelp(key, inner, out)
			}
		}
	}
}

// fieldText returns a field's doc comment, else its trailing comment, collapsed to one paragraph.
func fieldText(field *ast.Field) string {
	group := field.Doc
	if group == nil {
		group = field.Comment
	}
	if group == nil {
		return ""
	}
	return strings.Join(strings.Fields(group.Text()), " ")
}
