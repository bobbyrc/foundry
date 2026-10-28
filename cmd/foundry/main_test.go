package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/client"
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAPI serves mux as the foundry API and points the CLI at it through the environment. The
// config file and HOME are empty temp dirs, so the developer's own config is never read.
func fakeAPI(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FOUNDRY_API", srv.URL)
	t.Setenv("FOUNDRY_TOKEN", "test-token")
	t.Setenv("FOUNDRY_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// runCLI runs the CLI and returns its exit code and output.
func runCLI(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTaskCreateSendsFlagsAsJSON(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-42", "title": got["title"]})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("task", "create", "--project", "foundry", "--kind", "feature",
		"--title", "Add x", "--body", "do it", "--role", "implementer", "--priority", "2", "--parent", "task-1")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "task-42\n", out)
	assert.Equal(t, map[string]any{
		"project": "foundry", "kind": "feature", "title": "Add x", "body": "do it",
		"role": "implementer", "priority": float64(2), "parent_id": "task-1", "source": "api",
	}, got)
}

func TestTaskCreateReadsBodyFromFile(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-1"})
	})
	fakeAPI(t, mux)
	bodyPath := filepath.Join(t.TempDir(), "body.md")
	require.NoError(t, os.WriteFile(bodyPath, []byte("from a file"), 0o600))

	code, _, errOut := runCLI("task", "create", "--project", "p", "--kind", "chore", "--title", "t", "--body-file", bodyPath)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "from a file", got["body"])
}

func TestTaskCreateNeedsRequiredFlags(t *testing.T) {
	code, _, errOut := runCLI("task", "create", "--project", "foundry")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "--kind and --title")
}

func TestTaskListRendersTable(t *testing.T) {
	long := strings.Repeat("x", 100)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "queued,running", r.URL.Query().Get("status"))
		assert.Equal(t, "foundry", r.URL.Query().Get("project"))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "task-1", "status": "queued", "kind": "feature", "project": "foundry", "title": "Short"},
			{"id": "task-2", "status": "running", "kind": "bugfix", "project": "foundry", "title": long},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("task", "list", "--project", "foundry", "--status", "queued,running")
	require.Equal(t, 0, code, errOut)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Regexp(t, `^ID\s+STATUS\s+KIND\s+PROJECT\s+TITLE$`, lines[0])
	assert.Contains(t, lines[1], "task-1")
	assert.Contains(t, lines[2], strings.Repeat("x", 57)+"...")
	assert.NotContains(t, out, long)
}

func TestApproveReject(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/approvals/ap-1/decide", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		writeJSON(w, http.StatusOK, map[string]any{"id": "ap-1", "task_id": "task-1", "status": "rejected"})
	})
	fakeAPI(t, mux)

	// The approval ID comes before the flags, which the standard flag package would not parse.
	code, out, errOut := runCLI("approve", "ap-1", "--reject", "--note", "not yet")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, false, got["approve"])
	assert.Equal(t, "not yet", got["note"])
	assert.Equal(t, "approval ap-1 rejected (task task-1)\n", out)
}

func TestStatusNeedsYou(t *testing.T) {
	var runQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"tasks_by_status": map[string]int{"blocked": 1, "queued": 2}, "active_runs": 1,
			"approvals_pending": 1, "cost_today_usd": 1.5,
		})
	})
	mux.HandleFunc("GET /api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": false})
	})
	mux.HandleFunc("GET /api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "pending", r.URL.Query().Get("status"))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "ap-1", "kind": "merge", "title": "Merge PR 7", "reason": "touches migrations"},
		})
	})
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "blocked", r.URL.Query().Get("status"))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "task-9", "title": "Fix login", "blocked_reason": "needs DB credentials"},
		})
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		runQuery = r.URL.Query().Get("status")
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "run-1", "role": "implementer", "project": "foundry", "task_id": "task-2", "status": "running"},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("status")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "starting,running", runQuery)
	assert.Contains(t, out, "Needs you:")
	assert.Contains(t, out, "approval ap-1")
	assert.Contains(t, out, "touches migrations")
	assert.Contains(t, out, "blocked  task-9")
	assert.Contains(t, out, "needs DB credentials")
	assert.Contains(t, out, "Running:")
	assert.Contains(t, out, "run run-1")
}

func TestCostToday(t *testing.T) {
	assert.Equal(t, "$7.53", costToday(client.Stats{CostTodayUSD: 7.53}))
	assert.Equal(t, "$7.53 of $25.00 daily budget", costToday(client.Stats{CostTodayUSD: 7.53, DailyBudgetUSD: 25}))
	assert.Equal(t, "$25.00 of $25.00 daily budget (budget reached — no new runs until midnight)",
		costToday(client.Stats{CostTodayUSD: 25, DailyBudgetUSD: 25}))
}

func TestStatusJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": true})
	})
	mux.HandleFunc("GET /api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": true})
	})
	mux.HandleFunc("GET /api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("status", "--json")
	require.Equal(t, 0, code, errOut)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Contains(t, decoded, "approvals")
	assert.Contains(t, decoded, "running")
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, out, errOut := runCLI("bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, `unknown command "bogus"`)
	assert.Contains(t, errOut, "Commands:")
	for _, c := range commandList() {
		assert.Contains(t, errOut, "  "+c.name, "usage lists %s", c.name)
	}
}

func TestHelpExitsZero(t *testing.T) {
	code, out, _ := runCLI("help")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "serve")
	assert.Contains(t, out, "approve")
}

func TestMissingTokenIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FOUNDRY_API", "http://127.0.0.1:1")
	t.Setenv("FOUNDRY_TOKEN", "")
	t.Setenv("FOUNDRY_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	code, _, errOut := runCLI("status")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "no token: set FOUNDRY_TOKEN")
}

func TestInitWritesLoadableConfigAndRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "foundry.yaml")

	code, out, errOut := runCLI("init", "--config", path, "--project", "foundry",
		"--repo", "bobbyrc/foundry", "--path", "/src/foundry")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Wrote "+path)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "foundry", cfg.SelfProject)
	assert.Equal(t, "bobbyrc/foundry", cfg.Projects["foundry"].Repo)
	assert.Equal(t, "/src/foundry", cfg.Projects["foundry"].Path)
	assert.Equal(t, []config.Notifier{{Type: "log"}}, cfg.Notify)

	before, err := os.ReadFile(path)
	require.NoError(t, err)
	code, _, errOut = runCLI("init", "--config", path)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "refusing to overwrite")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestInitWithoutProjectLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foundry.yaml")
	code, _, errOut := runCLI("init", "--config", path)
	require.Equal(t, 0, code, errOut)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects)
	assert.Empty(t, cfg.SelfProject)
}

func TestInitProjectNeedsRepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foundry.yaml")
	code, _, errOut := runCLI("init", "--config", path, "--project", "app")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "go together")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no file is written on a usage error")
}

func TestParseSecrets(t *testing.T) {
	data := []byte(`# comment

FOO=bar
export BAZ="quoted value"
QUX='single'
EMPTY=
=novalue
NOEQUALS
  SPACED = padded  
`)
	assert.Equal(t, []secret{
		{key: "FOO", value: "bar"},
		{key: "BAZ", value: "quoted value"},
		{key: "QUX", value: "single"},
		{key: "EMPTY", value: ""},
		{key: "SPACED", value: "padded"},
	}, parseSecrets(data))
}

func TestLoadSecretsFileKeepsExistingEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("FOUNDRY_TEST_FROM_FILE=file-value\nFOUNDRY_TEST_ALREADY_SET=file-value\n"), 0o600))
	t.Setenv("FOUNDRY_TEST_ALREADY_SET", "env-value")
	t.Cleanup(func() { _ = os.Unsetenv("FOUNDRY_TEST_FROM_FILE") })

	require.NoError(t, loadSecretsFile(path))
	assert.Equal(t, "file-value", os.Getenv("FOUNDRY_TEST_FROM_FILE"))
	assert.Equal(t, "env-value", os.Getenv("FOUNDRY_TEST_ALREADY_SET"))
}

func TestLoadSecretsFileMissingIsNotAnError(t *testing.T) {
	assert.NoError(t, loadSecretsFile(filepath.Join(t.TempDir(), "nope.env")))
}

func TestChatPrintsNewestAssistantReply(t *testing.T) {
	old := chatPollInterval
	chatPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { chatPollInterval = old })

	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		assert.Equal(t, "foundry", in["project"])
		writeJSON(w, http.StatusCreated, map[string]any{"id": "conv-1", "busy": false})
	})
	mux.HandleFunc("POST /api/v1/conversations/conv-1/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": "m-1", "role": "user", "text": "what is up"})
	})
	mux.HandleFunc("GET /api/v1/conversations/conv-1", func(w http.ResponseWriter, r *http.Request) {
		polls++
		busy := polls < 2
		msgs := []map[string]any{{"role": "user", "text": "what is up"}}
		if !busy {
			msgs = append(msgs,
				map[string]any{"role": "assistant", "text": "first draft", "created_at": "2026-10-27T10:00:00Z"},
				map[string]any{"role": "assistant", "text": "the reply", "created_at": "2026-10-27T10:00:05Z"})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"conversation": map[string]any{"id": "conv-1", "busy": busy}, "messages": msgs,
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("chat", "--project", "foundry", "what", "is", "up")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "the reply\n", out)
	assert.Contains(t, errOut, "conversation: conv-1")
	assert.GreaterOrEqual(t, polls, 2, "the CLI waits while the conversation is busy")
}

func TestAPICommandsWorkWithoutConfigFile(t *testing.T) {
	// The API commands must work with no config file at all: FOUNDRY_API and FOUNDRY_TOKEN are
	// enough. This guards the fallback path of apiClient.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{{"name": "foundry", "repo": "bobbyrc/foundry", "counts": map[string]int{"queued": 1}}})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("projects")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "foundry")
	assert.Contains(t, out, "queued=1")
}

// followStream serves /api/v1/events. The first connection gets one event and closes; the CLI
// reconnects, and the second connection is refused so that events --follow returns. It records
// the query of every connection.
func followStream(t *testing.T) *[]string {
	t.Helper()
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if len(queries) > 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "stop"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: 9\nevent: task.created\ndata: {\"id\":9,\"type\":\"task.created\"}\n\n"))
	})
	fakeAPI(t, mux)
	return &queries
}

func TestEventsFollowWithoutAfterStartsFromNow(t *testing.T) {
	queries := followStream(t)
	code, _, _ := runCLI("events", "--follow")
	require.NotEqual(t, 0, code)
	require.NotEmpty(t, *queries)
	assert.Equal(t, "", (*queries)[0], "no after cursor: the stream sends no history replay")
}

func TestEventsFollowWithAfterReplaysFromCursor(t *testing.T) {
	queries := followStream(t)
	code, _, _ := runCLI("events", "--follow", "--after", "4")
	require.NotEqual(t, 0, code)
	require.NotEmpty(t, *queries)
	assert.Equal(t, "after=4", (*queries)[0])
}

func TestServeEnvsKeepTokenOutOfAgents(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	agent, orchestrator := serveEnvs("/opt/foundry/bin/foundry", "127.0.0.1:7777", "secret-token")

	assert.Equal(t, map[string]string{"PATH": "/opt/foundry/bin" + string(os.PathListSeparator) + "/usr/bin:/bin"}, agent)
	assert.Equal(t, map[string]string{
		"FOUNDRY_API":   "http://127.0.0.1:7777",
		"FOUNDRY_TOKEN": "secret-token",
	}, orchestrator)
}
