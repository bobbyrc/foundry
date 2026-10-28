package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeDeployments(t *testing.T) {
	t.Helper()
	started := time.Date(2026, 10, 28, 9, 30, 0, 0, time.Local)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/deployments", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "foundry", r.URL.Query().Get("project"))
		assert.Equal(t, "5", r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "dep-1", "project": "foundry", "ref": "abcdef0123456789", "state": "failed",
				"notes": strings.Repeat("n", 100), "log": "line one\nline two\n", "started_at": started},
			{"id": "dep-2", "project": "foundry", "ref": "1234567890", "state": "healthy", "started_at": started},
		})
	})
	fakeAPI(t, mux)
}

func TestDeploysTable(t *testing.T) {
	fakeDeployments(t)
	code, out, errOut := runCLI("deploys", "--project", "foundry", "--limit", "5")
	require.Equal(t, 0, code, errOut)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"ID", "PROJECT", "REF", "STATE", "STARTED", "NOTES"},
		strings.Fields(lines[0]))
	assert.Contains(t, lines[1], "dep-1")
	assert.Contains(t, lines[1], "abcdef0 ")
	assert.NotContains(t, lines[1], "abcdef01")
	assert.Contains(t, lines[1], "2026-10-28 09:30")
	assert.Contains(t, lines[1], strings.Repeat("n", 80))
	assert.NotContains(t, lines[1], strings.Repeat("n", 81))
	assert.True(t, strings.HasSuffix(lines[2], " -"), lines[2])
}

func TestDeploysJSON(t *testing.T) {
	fakeDeployments(t)
	code, out, errOut := runCLI("deploys", "--project", "foundry", "--limit", "5", "--json")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, `"id": "dep-1"`)
}

func TestDeploysShow(t *testing.T) {
	fakeDeployments2 := func(id string) (int, string, string) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/deployments", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []map[string]any{
				{"id": "dep-1", "project": "foundry", "ref": "abc", "state": "failed", "log": "boom\nstack"},
				{"id": "dep-2", "project": "foundry", "ref": "def", "state": "healthy"},
			})
		})
		fakeAPI(t, mux)
		return runCLI("deploys", "show", id)
	}

	code, out, errOut := fakeDeployments2("dep-1")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "id: dep-1\n")
	assert.Contains(t, out, "state: failed\n")
	assert.True(t, strings.HasSuffix(out, "\n\nboom\nstack\n"), out)

	code, out, errOut = fakeDeployments2("dep-2")
	require.Equal(t, 0, code, errOut)
	assert.True(t, strings.HasSuffix(out, "\n\n(no log)\n"), out)

	code, _, errOut = fakeDeployments2("nope")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "no deployment")
}
