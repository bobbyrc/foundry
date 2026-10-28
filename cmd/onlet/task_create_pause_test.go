package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePauseAPI serves a created task plus the stats and projects the pause notice reads.
func fakePauseAPI(t *testing.T, factoryPaused bool, projects string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-7", "title": "t"})
	})
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": factoryPaused})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(projects))
	})
	fakeAPI(t, mux)
}

func createArgs(extra ...string) []string {
	return append([]string{"task", "create", "--project", "onlet", "--kind", "chore", "--title", "t"}, extra...)
}

func TestTaskCreateNoticesPausedFactory(t *testing.T) {
	fakePauseAPI(t, true, `[{"name":"onlet","paused":false}]`)

	code, out, errOut := runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "task-7\n", out)
	assert.Equal(t, "note: the factory is paused; this task waits until \"onlet resume\"\n", errOut)
}

func TestTaskCreateNoticesOwnerPausedProject(t *testing.T) {
	fakePauseAPI(t, false, `[{"name":"onlet","paused":true,"paused_by":"owner"}]`)

	code, out, errOut := runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "task-7\n", out)
	assert.Equal(t, "note: project onlet is paused (by owner); this task waits until \"onlet project resume onlet\"\n", errOut)
}

func TestTaskCreateNoticesConfigPausedProject(t *testing.T) {
	fakePauseAPI(t, false, `[{"name":"onlet","paused":true,"paused_by":"config"}]`)

	code, out, errOut := runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "task-7\n", out)
	assert.Equal(t, "note: project onlet is paused in onlet.yaml (paused: true); this task waits until that is removed\n", errOut)
}

func TestTaskCreateNoNoticeWhenNotPaused(t *testing.T) {
	fakePauseAPI(t, false, `[{"name":"onlet","paused":false}]`)

	code, out, errOut := runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "task-7\n", out)
	assert.Empty(t, errOut)
}

func TestTaskCreateJSONHasNoPauseNotice(t *testing.T) {
	fakePauseAPI(t, true, `[{"name":"onlet","paused":true,"paused_by":"owner"}]`)

	code, out, errOut := runCLI(createArgs("--json")...)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "task-7")
	assert.Empty(t, errOut)
}
