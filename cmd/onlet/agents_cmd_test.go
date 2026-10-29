package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsHidesRemovedUnlessAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("project"), "the CLI asks for the global view")
		writeJSON(w, http.StatusOK, []map[string]any{
			{"name": "reviewer", "origin": "builtin", "disabled": false, "harness": "claude", "model": "sonnet", "kinds": []string{"review"}, "description": "reviews"},
			{"name": "improver", "origin": "builtin", "disabled": true, "harness": "codex", "kinds": []string{}, "description": "fixes friction"},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("agents")
	require.Equal(t, 0, code, errOut)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, `^ROLE\s+ORIGIN\s+HARNESS\s+MODEL\s+KINDS\s+DESCRIPTION$`, lines[0])
	assert.Regexp(t, `^reviewer\s+builtin\s+claude\s+sonnet\s+review\s+reviews$`, lines[1])
	assert.NotContains(t, out, "improver")

	code, out, errOut = runCLI("agents", "--json")
	require.Equal(t, 0, code, errOut)
	assert.NotContains(t, out, "improver")
	assert.Contains(t, out, `"name": "reviewer"`)

	code, out, errOut = runCLI("agents", "--all")
	require.Equal(t, 0, code, errOut)
	lines = strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Regexp(t, `^improver\s+removed\s+codex\s+-\s+-\s+fixes friction$`, lines[2])
}
