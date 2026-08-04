package models_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Phase 0 put the https_url tag on the three calendar-URL fields. This test is
// what stops the FOURTH URL field from shipping without it — by reading the
// source of every request struct rather than naming the fields, so a model added
// next month is covered without anyone remembering this file exists.
//
// Two rules, both about the same hole:
//
//  1. A bound struct's URL field must carry https_url. Without it the field is
//     either unvalidated or validated by validator's built-in `url` tag, which
//     accepts javascript:, data: and mailto: — and mentor-supplied URLs are
//     rendered into href and iframe src.
//  2. Nothing may use the bare `url` tag at all, for the same reason. The
//     scheme-restricting built-in is the separate, unused `http_url`.
//
// The predicate itself lives in pkg/safeurl and is tested there; this only
// asserts every field reaches it.

// urlFieldName matches a Go field name that holds a URL.
var urlFieldName = regexp.MustCompile(`(URL|Url|Uri|URI)$`)

func TestEveryBoundURLFieldRequiresHTTPS(t *testing.T) {
	for _, file := range modelSourceFiles(t) {
		for _, s := range boundStructs(t, file) {
			for _, field := range s.fields {
				if !urlFieldName.MatchString(field.name) {
					continue
				}
				t.Run(s.name+"."+field.name, func(t *testing.T) {
					require.Contains(t, splitBinding(field.binding), "https_url",
						"%s.%s is a URL on a struct bound from a request body, so it must carry "+
							"the https_url tag (see pkg/safeurl); binding tag is %q",
						s.name, field.name, field.binding)
				})
			}
		}
	}
}

func TestNoFieldUsesTheLooseURLTag(t *testing.T) {
	for _, file := range modelSourceFiles(t) {
		for _, s := range boundStructs(t, file) {
			for _, field := range s.fields {
				parts := splitBinding(field.binding)
				require.NotContains(t, parts, "url",
					"%s.%s uses validator's built-in `url` tag, which accepts javascript: and "+
						"data: URLs; use https_url", s.name, field.name)
				require.NotContains(t, parts, "uri",
					"%s.%s uses validator's built-in `uri` tag, which accepts any scheme; "+
						"use https_url", s.name, field.name)
			}
		}
	}
}

type sourceField struct {
	name    string
	binding string
}

type sourceStruct struct {
	name   string
	fields []sourceField
}

func modelSourceFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "internal", "models", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "found no model sources; the relative path is wrong")
	return files
}

// boundStructs returns the structs in file that are bound from a request body,
// identified by carrying at least one `binding:` tag. A struct with no binding
// tags is a response or a scan target and never sees user input directly.
func boundStructs(t *testing.T, file string) []sourceStruct {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)

	var out []sourceStruct
	ast.Inspect(parsed, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		s := sourceStruct{name: spec.Name.Name}
		bound := false
		for _, field := range structType.Fields.List {
			binding := ""
			if field.Tag != nil {
				if unquoted, unquoteErr := strconv.Unquote(field.Tag.Value); unquoteErr == nil {
					binding = reflect.StructTag(unquoted).Get("binding")
				}
			}
			if binding != "" {
				bound = true
			}
			for _, name := range field.Names {
				s.fields = append(s.fields, sourceField{name: name.Name, binding: binding})
			}
		}
		if bound {
			out = append(out, s)
		}
		return true
	})
	return out
}

func splitBinding(tag string) []string {
	parts := strings.Split(tag, ",")
	for i, p := range parts {
		// `max=500` and friends: compare on the tag name only.
		if name, _, found := strings.Cut(p, "="); found {
			parts[i] = name
		}
	}
	return parts
}
