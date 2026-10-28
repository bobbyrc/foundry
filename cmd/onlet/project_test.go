package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bobbyrc/onlet/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectPauseAndResumePrintState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/projects/onlet/pause", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, client.ProjectControl{Paused: true, PausedBy: "owner"})
	})
	mux.HandleFunc("POST /api/v1/projects/onlet/resume", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, client.ProjectControl{})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("project", "pause", "onlet")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "project onlet paused (owner)\n", out)

	code, out, errOut = runCLI("project", "resume", "onlet")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "project onlet resumed\n", out)
}

func TestProjectPauseSurfacesServerErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/projects/nope/pause", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown project nope"})
	})
	fakeAPI(t, mux)

	code, _, errOut := runCLI("project", "pause", "nope")
	assert.Equal(t, 1, code)
	assert.Equal(t, "onlet: unknown project nope\n", errOut)
}

func TestProjectUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"project"},
		{"project", "pause"},
		{"project", "pause", "a", "b"},
		{"project", "stop", "a"},
	} {
		code, _, errOut := runCLI(args...)
		assert.Equal(t, 2, code, "args %v: %s", args, errOut)
	}
}

func TestProjectsShowsWhoPausedEachProject(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{
			{"name": "byconfig", "paused": true, "paused_by": "config"},
			{"name": "byowner", "paused": true, "paused_by": "owner"},
			{"name": "running", "paused": false, "paused_by": ""},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("projects")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "config", paused(t, out, "byconfig"))
	assert.Equal(t, "owner", paused(t, out, "byowner"))
	assert.Equal(t, "-", paused(t, out, "running"))
}

// paused returns the PAUSED cell of the projects table row for name.
func paused(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == name {
			return f[2]
		}
	}
	t.Fatalf("no row for %s in:\n%s", name, out)
	return ""
}

func TestPausedColumnWithoutPauser(t *testing.T) {
	assert.Equal(t, "yes", pausedColumn(client.ProjectView{Paused: true}))
	assert.Equal(t, "-", pausedColumn(client.ProjectView{}))
	assert.Equal(t, "owner", pausedColumn(client.ProjectView{Paused: true, PausedBy: "owner"}))
}
