package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
)

// sseFrame is one parsed SSE message.
type sseFrame struct {
	id    string
	event string
	data  domain.Event
}

// readFrames reads frames from an SSE body in a goroutine and sends each on the returned channel.
func readFrames(t *testing.T, body interface{ Read([]byte) (int, error) }) <-chan sseFrame {
	t.Helper()
	out := make(chan sseFrame, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		var cur sseFrame
		var haveData bool
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if haveData {
					out <- cur
				}
				cur, haveData = sseFrame{}, false
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				haveData = true
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data)
			}
		}
	}()
	return out
}

func nextFrame(t *testing.T, ch <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		require.True(t, ok, "stream ended")
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
		return sseFrame{}
	}
}

func openStream(t *testing.T, x *harness, target, token string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(x.h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+target, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestSSEHeadersAndTokenQuery(t *testing.T) {
	x := newHarness(t)
	resp := openStream(t, x, "/api/v1/events?token="+testToken, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "keep-alive", resp.Header.Get("Connection"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
}

func TestSSERejectsBadToken(t *testing.T) {
	x := newHarness(t)
	resp := openStream(t, x, "/api/v1/events?token=wrong", "")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSEReplayThenLive(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Message: "before"})
		require.NoError(t, err)
	}

	// Headers are written only after the subscription and replay, so the publishes below are live.
	resp := openStream(t, x, "/api/v1/events?after=1", testToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	frames := readFrames(t, resp.Body)

	replayed := nextFrame(t, frames)
	assert.Equal(t, "2", replayed.id, "replay starts after the given ID")
	assert.Equal(t, domain.EvTaskCreated, replayed.event)
	assert.Equal(t, "before", replayed.data.Message)

	_, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskUpdated, Message: "stored live"})
	require.NoError(t, err)
	live := nextFrame(t, frames)
	assert.Equal(t, "3", live.id)
	assert.Equal(t, "stored live", live.data.Message)

	x.bus.Emit(ctx, domain.EvRunLog, "foundry", "", "run-1", "log line", nil)
	logFrame := nextFrame(t, frames)
	assert.Empty(t, logFrame.id, "live-only events have no id line")
	assert.Equal(t, domain.EvRunLog, logFrame.event)
	assert.Equal(t, "log line", logFrame.data.Message)
}

func TestSSELiveOnlyWithoutAfter(t *testing.T) {
	x := newHarness(t)
	resp := openStream(t, x, "/api/v1/events", testToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	frames := readFrames(t, resp.Body)

	_, err := x.bus.Publish(context.Background(), domain.Event{Type: domain.EvSystem, Message: "hello"})
	require.NoError(t, err)
	f := nextFrame(t, frames)
	assert.Equal(t, "1", f.id)
	assert.Equal(t, domain.EvSystem, f.event)
	assert.Equal(t, "hello", f.data.Message)
}

func TestSSEClientDisconnectEndsHandler(t *testing.T) {
	x := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		x.h.ServeHTTP(rec, req)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the client disconnected")
	}
}
