package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentryIssuesJSON = `[
 {"id":"111","title":"TypeError: nil","culprit":"app.handler","level":"error","count":"12",
  "permalink":"https://sentry.example/issues/111/","firstSeen":"2026-01-01T00:00:00Z","lastSeen":"2026-01-02T12:00:00Z"},
 {"id":"222","title":"old","culprit":"x","level":"warning","count":"nope",
  "permalink":"https://sentry.example/issues/222/","firstSeen":"2025-12-01T00:00:00Z","lastSeen":"2025-12-02T00:00:00Z"}
]`

func TestSentryPollMapsIssues(t *testing.T) {
	var gotAuth, gotPath string
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sentryIssuesJSON))
	}))
	defer srv.Close()

	o, err := newSentry(Config{
		Name: "sentry",
		Type: "sentry",
		Options: map[string]any{
			"org":       "acme",
			"project":   "web",
			"base_url":  srv.URL + "/",
			"token_env": "ONLET_TEST_SENTRY_TOKEN",
		},
		Env: map[string]string{"ONLET_TEST_SENTRY_TOKEN": "tok-123"},
	}, srv.Client())
	require.NoError(t, err)

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	obs, err := o.Poll(context.Background(), since)
	require.NoError(t, err)

	assert.Equal(t, "Bearer tok-123", gotAuth)
	assert.Equal(t, "/api/0/projects/acme/web/issues/", gotPath)
	assert.Equal(t, []string{"is:unresolved"}, gotQuery["query"])
	assert.Equal(t, []string{"24h"}, gotQuery["statsPeriod"])
	assert.Equal(t, []string{"100"}, gotQuery["limit"])

	// Issue 222 was last seen before since, so only 111 remains.
	require.Len(t, obs, 1)
	assert.Equal(t, "sentry-111", obs[0].Fingerprint)
	assert.Equal(t, "TypeError: nil", obs[0].Title)
	assert.Equal(t, "error", obs[0].Level)
	assert.Equal(t, int64(12), obs[0].Count)
	assert.Equal(t, "https://sentry.example/issues/111/", obs[0].URL)
	assert.Equal(t, "app.handler", obs[0].Culprit)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), obs[0].FirstSeen)
	assert.Equal(t, time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC), obs[0].LastSeen)
}

func TestSentryPollCountAndSinceMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sentryIssuesJSON))
	}))
	defer srv.Close()

	o, err := newSentry(Config{
		Name:    "sentry",
		Options: map[string]any{"org": "acme", "project": "web", "base_url": srv.URL},
		Env:     map[string]string{"SENTRY_AUTH_TOKEN": "t"},
	}, srv.Client())
	require.NoError(t, err)

	// Since is after both issues, so nothing is reported.
	obs, err := o.Poll(context.Background(), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Empty(t, obs)

	// Since is before both, so both are reported and the unparseable count becomes 0.
	obs, err = o.Poll(context.Background(), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, obs, 2)
	assert.Equal(t, int64(0), obs[1].Count)
}

func TestSentryPollCustomQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	o, err := newSentry(Config{
		Name: "sentry",
		Options: map[string]any{
			"org": "acme", "project": "web", "base_url": srv.URL, "query": "level:fatal is:unresolved",
		},
		Env: map[string]string{"SENTRY_AUTH_TOKEN": "t"},
	}, srv.Client())
	require.NoError(t, err)
	_, err = o.Poll(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "level:fatal is:unresolved", gotQuery)
}

func TestSentryPollNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden: token lacks project:read"))
	}))
	defer srv.Close()

	o, err := newSentry(Config{
		Name:    "sentry",
		Options: map[string]any{"org": "acme", "project": "web", "base_url": srv.URL},
		Env:     map[string]string{"SENTRY_AUTH_TOKEN": "t"},
	}, srv.Client())
	require.NoError(t, err)
	_, err = o.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden: token lacks project:read")
}

func TestSentryTokenFromProcessEnv(t *testing.T) {
	t.Setenv("ONLET_TEST_PROCESS_TOKEN", "from-process")
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	o, err := newSentry(Config{
		Name: "sentry",
		Options: map[string]any{
			"org": "acme", "project": "web", "base_url": srv.URL, "token_env": "ONLET_TEST_PROCESS_TOKEN",
		},
	}, srv.Client())
	require.NoError(t, err)
	_, err = o.Poll(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "Bearer from-process", gotAuth)
}

func TestSentryMissingTokenIsFactoryError(t *testing.T) {
	t.Setenv("ONLET_TEST_UNSET_TOKEN", "")
	_, err := New(Config{
		Name: "sentry",
		Type: "sentry",
		Options: map[string]any{
			"org": "acme", "project": "web", "token_env": "ONLET_TEST_UNSET_TOKEN",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ONLET_TEST_UNSET_TOKEN")
}

func TestSentryFactoryRequiresOrgAndProject(t *testing.T) {
	_, err := New(Config{
		Name:    "sentry",
		Type:    "sentry",
		Options: map[string]any{"project": "web"},
		Env:     map[string]string{"SENTRY_AUTH_TOKEN": "t"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"org" and "project"`)
}
