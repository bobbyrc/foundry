package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestsCarryBearerTokenAndPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/control", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(ControlState{Paused: true})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "tok")
	got, err := c.Control(context.Background())
	require.NoError(t, err)
	assert.True(t, got.Paused)
}

func TestCreateTaskSendsBodyAndDecodesTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/tasks", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var in NewTask
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		assert.Equal(t, "onett", in.Project)
		assert.Equal(t, "feature", in.Kind)
		assert.Equal(t, 2, in.Priority)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain.Task{ID: "t-1", Title: in.Title, Status: domain.TaskQueued})
	}))
	defer srv.Close()

	task, err := New(srv.URL, "tok").CreateTask(context.Background(), NewTask{
		Project: "onett", Kind: "feature", Title: "Add thing", Priority: 2, Source: "api",
	})
	require.NoError(t, err)
	assert.Equal(t, "t-1", task.ID)
	assert.Equal(t, domain.TaskQueued, task.Status)
}

func TestErrorBodyBecomesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"conversation is busy"}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").SendMessage(context.Background(), "c-1", "hi")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Status)
	assert.Equal(t, "conversation is busy", apiErr.Message)
	assert.Equal(t, "conversation is busy", err.Error())
}

func TestNoContentIsNilError(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, New(srv.URL, "tok").CancelTask(context.Background(), "a/b"))
	assert.Equal(t, "/api/v1/tasks/a%2Fb/cancel", gotPath)
}

func TestListTasksQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "queued,running", q.Get("status"))
		assert.Equal(t, "onett", q.Get("project"))
		assert.Equal(t, "25", q.Get("limit"))
		assert.Empty(t, q.Get("kind"))
		_ = json.NewEncoder(w).Encode([]domain.Task{{ID: "t-1"}})
	}))
	defer srv.Close()

	tasks, err := New(srv.URL, "tok").ListTasks(context.Background(), TaskFilter{
		Project: "onett", Status: []string{"queued", "running"}, Limit: 25,
	})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

func TestEventsParsesSSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "7", r.URL.Query().Get("after"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": ping\n\n"+
			"id: 8\nevent: task.updated\ndata: {\"id\":8,\"type\":\"task.updated\",\"task_id\":\"t-1\",\"message\":\"one\"}\n\n"+
			"event: run.log\ndata: {\"type\":\"run.log\",\"message\":\"live\"}\n\n"+
			"id: 9\nevent: future.kind\ndata: not json\n\n"+
			"id: 10\ndata: {\"type\":\"task.created\",\"message\":\"two\"}\n\n")
	}))
	defer srv.Close()

	var got []domain.Event
	err := New(srv.URL, "tok").Events(context.Background(), 7, func(e domain.Event) {
		got = append(got, e)
	})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, int64(8), got[0].ID)
	assert.Equal(t, "task.updated", got[0].Type)
	assert.Equal(t, "run.log", got[1].Type)
	assert.Equal(t, int64(0), got[1].ID, "live events carry no id")
	assert.Equal(t, int64(10), got[2].ID)
	assert.Equal(t, "two", got[2].Message)
}

func TestEventsReturnsNilWhenContextEnds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := New(srv.URL, "tok").Events(ctx, 0, func(domain.Event) {})
	assert.NoError(t, err)
}

func TestEventsReportsStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad token"}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "wrong").Events(context.Background(), 0, func(domain.Event) {})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.Status)
}

func TestReadEventStreamIgnoresTrailingPartialEvent(t *testing.T) {
	var n int
	err := readEventStream(context.Background(),
		strings.NewReader("data: {\"type\":\"a\"}\n\ndata: {\"type\":\"b\"}"),
		func(domain.Event) { n++ })
	require.NoError(t, err)
	assert.Equal(t, 1, n, "an event without its terminating blank line is dropped at EOF")
}

func TestPauseAndResumeProjectCallProjectRoutes(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		gotPaths = append(gotPaths, r.URL.EscapedPath())
		paused := r.URL.EscapedPath() == "/api/v1/projects/a%2Fb/pause"
		_ = json.NewEncoder(w).Encode(ProjectControl{Paused: paused, PausedBy: map[bool]string{true: "owner", false: ""}[paused]})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	got, err := c.PauseProject(context.Background(), "a/b")
	require.NoError(t, err)
	assert.True(t, got.Paused)
	assert.Equal(t, "owner", got.PausedBy)

	resumed, err := c.ResumeProject(context.Background(), "a/b")
	require.NoError(t, err)
	assert.False(t, resumed.Paused)
	assert.Empty(t, resumed.PausedBy)
	assert.Equal(t, []string{"/api/v1/projects/a%2Fb/pause", "/api/v1/projects/a%2Fb/resume"}, gotPaths)
}

func TestProjectsDecodesPausedBy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/projects", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"onett","paused":true,"paused_by":"config"}]`)
	}))
	defer srv.Close()

	projects, err := New(srv.URL, "tok").Projects(context.Background())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.True(t, projects[0].Paused)
	assert.Equal(t, "config", projects[0].PausedBy)
}
