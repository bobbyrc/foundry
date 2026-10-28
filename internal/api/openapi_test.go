package api

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

const openAPIPath = "../../docs/openapi.yaml"

// httpMethods are the keys of a path item that are operations.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// chiParam matches a chi URL parameter with a regex, such as {id:[0-9]+}.
var chiParam = regexp.MustCompile(`\{(\w+):[^}]*\}`)

// TestOpenAPISpecMatchesRoutes keeps docs/openapi.yaml in step with the routes NewServer mounts
// under /api/v1, and checks that every operation has an operationId and every $ref resolves.
func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	x := newHarness(t)
	routes, ok := x.h.(chi.Routes)
	require.True(t, ok, "NewServer must return a chi router so its routes can be walked")

	actual := map[string]bool{}
	require.NoError(t, chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path, ok := strings.CutPrefix(route, "/api/v1")
		if !ok {
			return nil
		}
		path = chiParam.ReplaceAllString(path, "{$1}")
		actual[strings.ToUpper(method)+" "+path] = true
		return nil
	}))
	require.NotEmpty(t, actual, "no /api/v1 routes found")

	doc := loadOpenAPI(t)
	require.Equal(t, "3.1.0", doc["openapi"], "openapi version")

	paths, ok := doc["paths"].(map[string]any)
	require.True(t, ok, "paths must be a map")

	documented := map[string]bool{}
	opIDs := map[string]string{}
	for path, item := range paths {
		pathItem, ok := item.(map[string]any)
		require.True(t, ok, "path item %s must be a map", path)
		for _, m := range httpMethods {
			raw, ok := pathItem[m]
			if !ok {
				continue
			}
			key := strings.ToUpper(m) + " " + path
			documented[key] = true

			op, ok := raw.(map[string]any)
			require.True(t, ok, "operation %s must be a map", key)
			id, ok := op["operationId"].(string)
			if assert.True(t, ok && id != "", "%s has no operationId", key) {
				if other, dup := opIDs[id]; dup {
					t.Errorf("operationId %q used by both %s and %s", id, other, key)
				}
				opIDs[id] = key
			}
			responses, ok := op["responses"].(map[string]any)
			assert.True(t, ok && len(responses) > 0, "%s has no responses", key)
		}
	}

	missing := setDiff(actual, documented)
	extra := setDiff(documented, actual)
	assert.Empty(t, missing, "routes served by NewServer but missing from docs/openapi.yaml")
	assert.Empty(t, extra, "operations in docs/openapi.yaml that no route serves")

	var refs []string
	collectRefs(doc, &refs)
	for _, ref := range refs {
		_, found := resolveRef(doc, ref)
		assert.True(t, found, "$ref %s does not resolve", ref)
	}
}

func loadOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(openAPIPath)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(data, &doc))
	return doc
}

// setDiff returns the sorted keys of a that are not in b.
func setDiff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// collectRefs appends every $ref value in the document tree.
func collectRefs(node any, out *[]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			collectRefs(child, out)
		}
	case map[any]any:
		for k, child := range v {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			collectRefs(child, out)
		}
	case []any:
		for _, child := range v {
			collectRefs(child, out)
		}
	}
}

// resolveRef follows a local JSON pointer such as #/components/schemas/Task.
func resolveRef(doc map[string]any, ref string) (any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var cur any = doc
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}
