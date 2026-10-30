package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/bobbyrc/onett/internal/domain"
)

// wsWriteTimeout bounds one write or ping to the client. A client that stops reading fills its
// TCP buffer, and without a bound the write would block forever and hold the stream slot.
const wsWriteTimeout = 10 * time.Second

// eventsWS serves GET /ws, a WebSocket that carries the same events as GET /events. Run logs and
// chat deltas reach it only after the client subscribes to their run or conversation (see
// wsproto.go). It takes ?after=<id> with the same replay rules as GET /events.
//
// The WebSocket's Origin check is the protection against other websites. http.CrossOriginProtection
// lets every GET through, so websocket.Accept does the check here: it refuses an Origin whose host
// is not this server's host, and it is never told to skip that check.
func (s *server) eventsWS(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFrom(r.Context())
	if !ok {
		writeErrorMsg(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	after, err := queryInt64(r.URL.Query(), "after")
	if err != nil {
		s.fail(w, err)
		return
	}
	release, ok := s.tryAcquireStream()
	if !ok {
		w.Header().Set("Retry-After", "5")
		writeErrorMsg(w, http.StatusServiceUnavailable, "too many event streams")
		return
	}
	defer release()

	// Subscribed before Accept, as in events: an event published during the replay is delivered.
	ch, cancel := s.opts.Bus.Subscribe()
	defer cancel()

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has already written the refusal, for example 403 for a foreign Origin.
		return
	}
	c.SetReadLimit(wsReadLimit)

	// The request context is not cancelled when the client leaves, because the connection was
	// hijacked. Ending ctx from the reader's Read error is what lets the stream end.
	ctx, stop := context.WithCancel(r.Context())
	defer stop()

	subs := newWSSubscriptions()
	go s.wsReadLoop(ctx, stop, c, subs)

	sink := wsSink{ctx: ctx, c: c, log: s.log}
	s.streamEvents(ctx, p, ch, after, r.URL.Query().Get("after") != "", sink, subs.wants)
	stop()
	_ = c.Close(websocket.StatusGoingAway, "stream ended")
}

// wsReadLoop reads client frames until the socket fails or ctx ends, and answers each one. It
// is the only goroutine that calls Read, which the library requires. Its replies are written
// from here rather than from the stream loop: a reply has to go out after subs has changed,
// and it must not wait behind a burst of events.
func (s *server) wsReadLoop(ctx context.Context, stop context.CancelFunc, c *websocket.Conn, subs *wsSubscriptions) {
	defer stop()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			// The client left, sent a bad close, or the frame was over wsReadLimit.
			return
		}
		reply := wsOut{Kind: "error", Message: errWSFrame.Error()}
		if typ == websocket.MessageText {
			reply = subs.handle(data)
		}
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		err = wsjson.Write(wctx, c, reply)
		cancel()
		if err != nil {
			return
		}
	}
}

// wsSink is the eventSink for GET /ws. Each send is one text frame, so flush has nothing to do.
type wsSink struct {
	ctx context.Context
	c   *websocket.Conn
	log *slog.Logger
}

func (k wsSink) send(e domain.Event) error {
	data, err := json.Marshal(wsOut{Kind: "event", Event: &e})
	if err != nil {
		// Same policy as writeEvent: an event that cannot be encoded is logged and skipped.
		k.log.Error("api: encode ws event", "type", e.Type, "err", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(k.ctx, wsWriteTimeout)
	defer cancel()
	return k.c.Write(ctx, websocket.MessageText, data)
}

func (k wsSink) flush() error { return nil }

func (k wsSink) ping() error {
	ctx, cancel := context.WithTimeout(k.ctx, wsWriteTimeout)
	defer cancel()
	return k.c.Ping(ctx)
}
