package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ntfyRequest struct {
	method string
	path   string
	header http.Header
	body   string
}

// newNtfyServer returns a server that records every request and answers with status.
func newNtfyServer(t *testing.T, status int, reply string) (*httptest.Server, *[]ntfyRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []ntfyRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, ntfyRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(b)})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestNtfyNotify(t *testing.T) {
	tests := []struct {
		name         string
		notification Notification
		wantBody     string
		wantPriority string
		wantTags     string
		wantClick    string
	}{
		{
			name:         "body and all headers",
			notification: Notification{Title: "Merged", Body: "PR 42 merged", Priority: PriorityHigh, URL: "https://onlet.local/t/1", Tags: []string{"merge", "ok"}},
			wantBody:     "PR 42 merged",
			wantPriority: "5",
			wantTags:     "merge,ok",
			wantClick:    "https://onlet.local/t/1",
		},
		{
			name:         "empty body falls back to title",
			notification: Notification{Title: "Deploy failed"},
			wantBody:     "Deploy failed",
			wantPriority: "3",
		},
		{
			name:         "low priority maps to 2",
			notification: Notification{Title: "x", Priority: PriorityLow},
			wantBody:     "x",
			wantPriority: "2",
		},
		{
			name:         "normal priority maps to 3",
			notification: Notification{Title: "x", Priority: PriorityNormal},
			wantBody:     "x",
			wantPriority: "3",
		},
		{
			name:         "line breaks in title are replaced",
			notification: Notification{Title: "a\nb"},
			wantBody:     "a\nb",
			wantPriority: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := newNtfyServer(t, http.StatusOK, "")
			n, err := newNtfy(map[string]any{"server": srv.URL + "/", "topic": "onlet-alerts"}, srv.Client())
			require.NoError(t, err)

			require.NoError(t, n.Notify(context.Background(), tt.notification))

			require.Len(t, *got, 1)
			req := (*got)[0]
			assert.Equal(t, http.MethodPost, req.method)
			assert.Equal(t, "/onlet-alerts", req.path)
			assert.Equal(t, tt.wantBody, req.body)
			assert.Equal(t, strings.ReplaceAll(tt.notification.Title, "\n", " "), req.header.Get("Title"))
			assert.Equal(t, tt.wantPriority, req.header.Get("Priority"))
			assert.Equal(t, tt.wantTags, req.header.Get("Tags"))
			assert.Equal(t, tt.wantClick, req.header.Get("Click"))
			assert.Empty(t, req.header.Get("Authorization"))
		})
	}
}

func TestNtfyBearerToken(t *testing.T) {
	t.Setenv("ONLET_TEST_NTFY_TOKEN", "s3cret")
	srv, got := newNtfyServer(t, http.StatusOK, "")
	n, err := newNtfy(map[string]any{"server": srv.URL, "topic": "t", "token_env": "ONLET_TEST_NTFY_TOKEN"}, srv.Client())
	require.NoError(t, err)

	require.NoError(t, n.Notify(context.Background(), Notification{Title: "hi"}))
	require.Len(t, *got, 1)
	assert.Equal(t, "Bearer s3cret", (*got)[0].header.Get("Authorization"))
}

func TestNtfyTokenEnvEmptyIsError(t *testing.T) {
	t.Setenv("ONLET_TEST_NTFY_EMPTY", "")
	_, err := newNtfy(map[string]any{"topic": "t", "token_env": "ONLET_TEST_NTFY_EMPTY"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ONLET_TEST_NTFY_EMPTY")
}

func TestNtfyOptionValidation(t *testing.T) {
	tests := []struct {
		name       string
		opts       map[string]any
		wantServer string
		wantErr    bool
	}{
		{name: "missing topic is an error", opts: map[string]any{}, wantErr: true},
		{name: "nil topic is an error", opts: map[string]any{"topic": nil}, wantErr: true},
		{name: "default server", opts: map[string]any{"topic": "t"}, wantServer: "https://ntfy.sh"},
		{name: "trailing slash trimmed", opts: map[string]any{"topic": "t", "server": "https://ntfy.example.com/"}, wantServer: "https://ntfy.example.com"},
		{name: "non-string topic is formatted", opts: map[string]any{"topic": 42}, wantServer: "https://ntfy.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := newNtfy(tt.opts, nil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantServer, n.server)
		})
	}
}

func TestNtfyNon2xxIsError(t *testing.T) {
	long := strings.Repeat("x", 500)
	srv, _ := newNtfyServer(t, http.StatusTooManyRequests, long)
	n, err := newNtfy(map[string]any{"server": srv.URL, "topic": "t"}, srv.Client())
	require.NoError(t, err)

	err = n.Notify(context.Background(), Notification{Title: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), strings.Repeat("x", ntfyErrBodyLimit))
	assert.NotContains(t, err.Error(), strings.Repeat("x", ntfyErrBodyLimit+1))
}

func TestNtfyRegisteredFactory(t *testing.T) {
	notifier, err := New(Config{Type: "ntfy", Options: map[string]any{"topic": "t", "server": "http://127.0.0.1:1"}})
	require.NoError(t, err)
	assert.IsType(t, &ntfy{}, notifier)

	_, err = New(Config{Type: "ntfy", Options: map[string]any{}})
	require.Error(t, err)
}
