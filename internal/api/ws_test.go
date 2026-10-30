package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

// wsURL turns an httptest server URL into the WebSocket URL of GET /api/v1/ws.
func wsURL(srv *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws" + query
}

// wsServer serves h and, at cleanup, waits until every handler has returned. httptest.Server.Close
// does not wait for hijacked connections, so a WebSocket handler could otherwise still be reading
// pingInterval when a later test sets it. Cleanups run last-in first-out, so the client
// connections a test registers after this call are closed before the wait.
func wsServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	var wg sync.WaitGroup
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wg.Add(1)
		defer wg.Done()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(wg.Wait)
	return srv
}

// dialWS opens a WebSocket with the given headers. The connection is closed when the test ends.
func dialWS(t *testing.T, url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, resp, err
}

// authHeader is the API token as a bearer header.
func authHeader() http.Header {
	return http.Header{"Authorization": []string{"Bearer " + testToken}}
}

// readWS reads one server frame, failing the test if none arrives within 5 seconds.
func readWS(t *testing.T, c *websocket.Conn) wsOut {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out wsOut
	require.NoError(t, wsjson.Read(ctx, c, &out))
	return out
}

// readEvent reads one frame and requires it to be an event.
func readEvent(t *testing.T, c *websocket.Conn) domain.Event {
	t.Helper()
	out := readWS(t, c)
	require.Equal(t, "event", out.Kind)
	require.NotNil(t, out.Event)
	return *out.Event
}

// sendWS writes one client frame as JSON.
func sendWS(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, wsjson.Write(ctx, c, v))
}

// runFrame is a client frame that subscribes to or unsubscribes from one run.
func runFrame(kind, runID string) map[string]string {
	return map[string]string{"kind": kind, "run_id": runID}
}

func TestWSReplayThenLive(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	first, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Message: "one"})
	require.NoError(t, err)
	second, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Message: "two"})
	require.NoError(t, err)

	srv := wsServer(t, x.h)
	c, resp, err := dialWS(t, wsURL(srv, fmt.Sprintf("?after=%d", first.ID)), authHeader())
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	got := readEvent(t, c)
	assert.Equal(t, second.ID, got.ID, "replay starts after the cursor")
	assert.Equal(t, "two", got.Message)

	_, err = x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskUpdated, Message: "live"})
	require.NoError(t, err)
	live := readEvent(t, c)
	assert.Equal(t, "live", live.Message)
	assert.Greater(t, live.ID, second.ID, "the live event is not a replayed duplicate")
}

func TestWSRunLogNeedsSubscription(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	srv := wsServer(t, x.h)
	c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.NoError(t, err)

	// Not subscribed: the run log is dropped, and the marker after it arrives first.
	x.bus.Emit(ctx, domain.EvRunLog, "onett", "", "r1", "unsubscribed line", nil)
	x.bus.Emit(ctx, domain.EvTaskUpdated, "onett", "", "", "marker-1", nil)
	assert.Equal(t, "marker-1", readEvent(t, c).Message)

	sendWS(t, c, runFrame("subscribe", "r1"))
	out := readWS(t, c)
	assert.Equal(t, "subscribed", out.Kind)
	assert.Equal(t, "r1", out.RunID)

	x.bus.Emit(ctx, domain.EvRunLog, "onett", "", "r1", "subscribed line", nil)
	assert.Equal(t, "subscribed line", readEvent(t, c).Message)

	sendWS(t, c, runFrame("unsubscribe", "r1"))
	out = readWS(t, c)
	assert.Equal(t, "unsubscribed", out.Kind)
	assert.Equal(t, "r1", out.RunID)

	x.bus.Emit(ctx, domain.EvRunLog, "onett", "", "r1", "after unsubscribe", nil)
	x.bus.Emit(ctx, domain.EvTaskUpdated, "onett", "", "", "marker-2", nil)
	assert.Equal(t, "marker-2", readEvent(t, c).Message)
}

func TestWSChatDeltaNeedsSubscription(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	srv := wsServer(t, x.h)
	c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.NoError(t, err)
	delta := map[string]string{"conversation_id": "c1", "text": "hi"}

	x.bus.Emit(ctx, domain.EvChatDelta, "onett", "", "", "unsubscribed delta", delta)
	x.bus.Emit(ctx, domain.EvTaskUpdated, "onett", "", "", "marker-1", nil)
	assert.Equal(t, "marker-1", readEvent(t, c).Message)

	sendWS(t, c, map[string]string{"kind": "subscribe", "conversation_id": "c1"})
	out := readWS(t, c)
	assert.Equal(t, "subscribed", out.Kind)
	assert.Equal(t, "c1", out.ConversationID)

	x.bus.Emit(ctx, domain.EvChatDelta, "onett", "", "", "subscribed delta", delta)
	assert.Equal(t, "subscribed delta", readEvent(t, c).Message)
}

func TestWSBadMessagesKeepSocketOpen(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)
	c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte("hello")))
	assert.Equal(t, "error", readWS(t, c).Kind)

	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}))
	assert.Equal(t, "error", readWS(t, c).Kind)

	sendWS(t, c, runFrame("subscribe", "r1"))
	assert.Equal(t, "subscribed", readWS(t, c).Kind)
}

func TestWSRefusesOtherOrigins(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)

	for _, origin := range []string{"http://evil.example", "http://127.0.0.1:1"} {
		h := authHeader()
		h.Set("Origin", origin)
		_, resp, err := dialWS(t, wsURL(srv, ""), h)
		require.Error(t, err, "origin %s", origin)
		require.NotNil(t, resp, "origin %s", origin)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "origin %s", origin)
	}

	h := authHeader()
	h.Set("Origin", srv.URL)
	_, resp, err := dialWS(t, wsURL(srv, ""), h)
	require.NoError(t, err, "the server's own origin connects")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestWSAuth(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)

	_, resp, err := dialWS(t, wsURL(srv, ""), nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no credential")

	// ?token= is accepted only on GET /events, so it is not a credential here.
	_, resp, err = dialWS(t, wsURL(srv, "?token="+testToken), nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "query token")

	value := x.newUserSession(t, "ann", domain.RoleViewer)
	cookie := http.Header{"Cookie": []string{auth.SessionCookie + "=" + value}}
	_, resp, err = dialWS(t, wsURL(srv, ""), cookie)
	require.NoError(t, err, "a signed-in viewer connects")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestWSSignedOutLosesSocket(t *testing.T) {
	x := newHarness(t)
	old := pingInterval
	pingInterval = 50 * time.Millisecond
	t.Cleanup(func() { pingInterval = old })
	s := newProbeServer(x)
	srv := wsServer(t, s.principal(http.HandlerFunc(s.eventsWS)))

	value := x.newUserSession(t, "ann", domain.RoleViewer)
	cookie := http.Header{"Cookie": []string{auth.SessionCookie + "=" + value}}
	c, _, err := dialWS(t, wsURL(srv, ""), cookie)
	require.NoError(t, err)

	require.NoError(t, x.st.DeleteSession(context.Background(), auth.SessionID(value)))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out wsOut
	require.Error(t, wsjson.Read(ctx, c, &out), "the socket should close after sign-out")
}

func TestWSSharesStreamCap(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)

	// The SSE streams hold slots until their cancel runs. Registered after srv.Close, so the
	// cancels run first and the server can shut down.
	var cancels []context.CancelFunc
	t.Cleanup(func() {
		for _, c := range cancels {
			c()
		}
	})
	for i := 0; i < maxEventStreams; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		require.Equal(t, http.StatusOK, resp.StatusCode, "stream %d", i+1)
	}

	_, resp, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, "5", resp.Header.Get("Retry-After"))

	cancels[0]()
	require.Eventually(t, func() bool {
		c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
		if err != nil {
			return false
		}
		_ = c.CloseNow()
		return true
	}, 2*time.Second, 10*time.Millisecond, "a freed SSE slot admits a WebSocket")
}

func TestWSOversizeFrameCloses(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)
	c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The server may close before the whole frame is written, so the write error is not checked.
	_ = c.Write(ctx, websocket.MessageText, bytes.Repeat([]byte("a"), 5000))

	var out wsOut
	require.Error(t, wsjson.Read(ctx, c, &out), "an oversize frame closes the socket")
}

func TestWSClientVanishFreesSlot(t *testing.T) {
	x := newHarness(t)
	srv := wsServer(t, x.h)

	// The first dial to succeed proves the slot was freed after the vanished client.
	c, _, err := dialWS(t, wsURL(srv, ""), authHeader())
	require.NoError(t, err)
	require.NoError(t, c.CloseNow(), "client drops the TCP connection without a close frame")

	var open []*websocket.Conn
	t.Cleanup(func() {
		for _, o := range open {
			_ = o.CloseNow()
		}
	})
	require.Eventually(t, func() bool {
		o, _, err := dialWS(t, wsURL(srv, ""), authHeader())
		if err != nil {
			return false
		}
		open = append(open, o)
		return true
	}, 2*time.Second, 10*time.Millisecond, "the vanished client's slot is released")

	for len(open) < maxEventStreams {
		o, _, err := dialWS(t, wsURL(srv, ""), authHeader())
		require.NoError(t, err, "dial %d of %d", len(open)+1, maxEventStreams)
		open = append(open, o)
	}
}
