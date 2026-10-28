package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/auth"
	"github.com/bobbyrc/foundry/internal/domain"
)

// streamServerWithPing serves only the principal check and GET /events, with the ping interval
// shortened. The interval is a package variable, so it is set before the server starts and put
// back when the test ends.
func streamServerWithPing(t *testing.T, x *harness, every time.Duration) *httptest.Server {
	t.Helper()
	old := pingInterval
	pingInterval = every
	t.Cleanup(func() { pingInterval = old })
	s := newProbeServer(x)
	srv := httptest.NewServer(s.principal(http.HandlerFunc(s.events)))
	t.Cleanup(srv.Close)
	return srv
}

// getStreamWithCookie opens GET url with the session cookie value, and returns the response.
func getStreamWithCookie(t *testing.T, url, value string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: value})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestEventStreamEndsWhenItsSessionIsDeleted(t *testing.T) {
	x := newHarness(t)
	value := x.newUserSession(t, "alice", domain.RoleOperator)
	srv := streamServerWithPing(t, x, 10*time.Millisecond)

	resp := getStreamWithCookie(t, srv.URL+"/api/v1/events", value)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, x.st.DeleteSession(context.Background(), auth.SessionID(value)))

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body)
		done <- err
	}()
	select {
	case err := <-done:
		assert.NoError(t, err, "the stream should end cleanly")
	case <-time.After(5 * time.Second):
		t.Fatal("event stream stayed open after its session was deleted")
	}
}

func TestEventStreamKeepsPingingWhileSessionIsLive(t *testing.T) {
	x := newHarness(t)
	value := x.newUserSession(t, "alice", domain.RoleOperator)
	srv := streamServerWithPing(t, x, 10*time.Millisecond)

	resp := getStreamWithCookie(t, srv.URL+"/api/v1/events", value)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	seen := make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ": ping") {
				seen <- struct{}{}
				return
			}
		}
	}()
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("a live session's stream received no ping")
	}
}
