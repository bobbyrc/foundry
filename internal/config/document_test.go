package config

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocumentTreeKeepsEnvReferencesUnexpanded(t *testing.T) {
	t.Setenv("ONETT_TEST_TOKEN", "should-not-appear")
	v, err := DocumentTree([]byte("server:\n  addr: ${ONETT_TEST_TOKEN}\n"))
	require.NoError(t, err)
	server := v.(map[string]any)["server"].(map[string]any)
	assert.Equal(t, "${ONETT_TEST_TOKEN}", server["addr"])
}

func TestDocumentTreeReturnsJSONTypes(t *testing.T) {
	v, err := DocumentTree([]byte(`
limits:
  max_attempts: 3
  daily_budget_usd: 2.5
  paused: true
  note: none
  nothing: ~
projects:
  web:
    routing: [feature, bugfix]
`))
	require.NoError(t, err)
	root := v.(map[string]any)
	limits := root["limits"].(map[string]any)
	assert.Equal(t, int64(3), limits["max_attempts"])
	assert.Equal(t, 2.5, limits["daily_budget_usd"])
	assert.Equal(t, true, limits["paused"])
	assert.Equal(t, "none", limits["note"])
	assert.Nil(t, limits["nothing"])
	web := root["projects"].(map[string]any)["web"].(map[string]any)
	assert.Equal(t, []any{"feature", "bugfix"}, web["routing"])
}

func TestDocumentTreeEmptyFileIsEmptyMapping(t *testing.T) {
	v, err := DocumentTree(nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, v)
}

func TestApplyChangesSetScalarKeepsComments(t *testing.T) {
	in := []byte(`limits:
  # how many times a task may run
  max_attempts: 3 # keep it low
`)
	out, err := ApplyChanges(in, []Change{{Op: "set", Path: []string{"limits", "max_attempts"}, Value: float64(5)}})
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "max_attempts: 5")
	assert.Contains(t, s, "# keep it low", "line comment beside the value must survive")
	assert.Contains(t, s, "# how many times a task may run", "comment above the key must survive")
}

func TestApplyChangesAddsProjectAsMapping(t *testing.T) {
	out, err := ApplyChanges([]byte("projects:\n  web:\n    repo: bob/web\n"), []Change{
		{Op: "set", Path: []string{"projects", "api"}, Value: map[string]any{"repo": "bob/api"}},
	})
	require.NoError(t, err)
	v, err := DocumentTree(out)
	require.NoError(t, err)
	projects := v.(map[string]any)["projects"].(map[string]any)
	assert.Equal(t, map[string]any{"repo": "bob/api"}, projects["api"])
	assert.Equal(t, map[string]any{"repo": "bob/web"}, projects["web"])
}

func TestApplyChangesCreatesMissingMappings(t *testing.T) {
	out, err := ApplyChanges(nil, []Change{{Op: "set", Path: []string{"server", "addr"}, Value: "127.0.0.1:9000"}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "server:\n  addr: 127.0.0.1:9000")
}

func TestApplyChangesWritesWholeNumbersAsIntegers(t *testing.T) {
	out, err := ApplyChanges([]byte("limits:\n  max_attempts: 3\n"), []Change{
		{Op: "set", Path: []string{"limits", "max_attempts"}, Value: float64(15)},
	})
	require.NoError(t, err)
	assert.Contains(t, string(out), "max_attempts: 15\n")
	assert.NotContains(t, string(out), "15.0")
}

func TestApplyChangesAppendsToListAtLength(t *testing.T) {
	in := []byte("projects:\n  web:\n    repo: bob/web\n    setup:\n      - name: install\n        run: make deps\n")
	out, err := ApplyChanges(in, []Change{{Op: "set", Path: []string{"projects", "web", "setup", "1"}, Value: map[string]any{"name": "lint", "run": "make lint"}}})
	require.NoError(t, err)
	v, err := DocumentTree(out)
	require.NoError(t, err)
	setup := v.(map[string]any)["projects"].(map[string]any)["web"].(map[string]any)["setup"].([]any)
	require.Len(t, setup, 2)
	assert.Equal(t, "lint", setup[1].(map[string]any)["name"])
}

func TestApplyChangesDeletesKeyAndListElement(t *testing.T) {
	in := []byte("projects:\n  web:\n    repo: bob/web\n    instructions: be brief\n    checks:\n      - name: a\n        run: a\n      - name: b\n        run: b\n")
	out, err := ApplyChanges(in, []Change{
		{Op: "delete", Path: []string{"projects", "web", "instructions"}},
		{Op: "delete", Path: []string{"projects", "web", "checks", "0"}},
	})
	require.NoError(t, err)
	v, err := DocumentTree(out)
	require.NoError(t, err)
	web := v.(map[string]any)["projects"].(map[string]any)["web"].(map[string]any)
	assert.NotContains(t, web, "instructions")
	checks := web["checks"].([]any)
	require.Len(t, checks, 1)
	assert.Equal(t, "b", checks[0].(map[string]any)["name"])
}

func TestApplyChangesErrors(t *testing.T) {
	in := []byte("projects:\n  web:\n    repo: bob/web\n")
	_, err := ApplyChanges(in, []Change{{Op: "delete", Path: []string{"projects", "web", "missing"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.web.missing: not found")

	_, err = ApplyChanges(in, []Change{{Op: "rename", Path: []string{"projects"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown op")

	_, err = ApplyChanges(in, []Change{{Op: "set", Path: nil, Value: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty path")
}

func TestApplyChangesRoundTripParses(t *testing.T) {
	example, err := os.ReadFile("../../onett.example.yaml")
	require.NoError(t, err)
	out, err := ApplyChanges(example, []Change{{Op: "set", Path: []string{"limits", "max_attempts"}, Value: float64(5)}})
	require.NoError(t, err)
	cfg, err := Parse(out, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Limits.MaxAttempts)
}

func TestProblemsFromValidateErrorHasPaths(t *testing.T) {
	_, err := Parse([]byte(`
limits:
  daily_budget_usd: -1
projects:
  web:
    repo: not a repo
`), "")
	require.Error(t, err)
	problems := ProblemsFrom(err)
	paths := map[string]string{}
	for _, p := range problems {
		paths[p.Path] = p.Message
	}
	require.Contains(t, paths, "limits.daily_budget_usd")
	assert.Contains(t, paths["limits.daily_budget_usd"], "must be 0 (no limit) or greater")
	require.Contains(t, paths, "projects.web.repo")
	assert.True(t, strings.HasPrefix(paths["projects.web.repo"], "want owner/name"))
}

func TestProblemsFromSyntaxErrorHasNoPath(t *testing.T) {
	_, err := Parse([]byte("limits: [\n"), "")
	require.Error(t, err)
	problems := ProblemsFrom(err)
	require.Len(t, problems, 1)
	assert.Equal(t, "", problems[0].Path)
	assert.NotEmpty(t, problems[0].Message)
}

func TestProblemsFromNilIsEmpty(t *testing.T) {
	assert.Nil(t, ProblemsFrom(nil))
}

func TestRestartRequired(t *testing.T) {
	base, err := Parse([]byte("server:\n  addr: 127.0.0.1:7777\n"), "")
	require.NoError(t, err)
	same, err := Parse([]byte("server:\n  addr: 127.0.0.1:7777\nlimits:\n  max_attempts: 9\n"), "")
	require.NoError(t, err)
	assert.Empty(t, RestartRequired(base, same), "live settings do not need a restart")

	changed, err := Parse([]byte("server:\n  addr: 127.0.0.1:8888\n  data_dir: /tmp/x\n  allowed_hosts: [box.local]\nnotify:\n  - type: ntfy\n"), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"server.addr", "server.data_dir", "server.allowed_hosts", "notify"}, RestartRequired(base, changed))
}

// aliasBomb builds YAML with levels+1 lists (l0 to l<levels>), where each list after l0 is ten aliases
// to the list below it. Expanded, l4 describes about 120,000 nodes from a few hundred bytes of text.
func aliasBomb(levels int) string {
	var b strings.Builder
	b.WriteString("l0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i <= levels; i++ {
		b.WriteString("l" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*a" + strconv.Itoa(i-1))
		}
		b.WriteString("]\n")
	}
	return b.String()
}

func TestDocumentTreeRefusesAliasExpansionBomb(t *testing.T) {
	_, err := DocumentTree([]byte(aliasBomb(4)))
	require.Error(t, err)
	assert.Equal(t, "config document is too large or has too many aliases", err.Error())
}

func TestDocumentTreeAcceptsModestAliasReuse(t *testing.T) {
	// Three levels (l0 to l3) expand to about 12,000 nodes, well inside the limit.
	v, err := DocumentTree([]byte(aliasBomb(3)))
	require.NoError(t, err)
	assert.Contains(t, v.(map[string]any), "l3")
}
