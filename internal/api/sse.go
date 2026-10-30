package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

// pingInterval is how often a ": ping" comment keeps idle connections open. It is a variable so
// tests can shorten it; production never changes it.
var pingInterval = 15 * time.Second

const (
	// replayPage is how many stored events are read per store query during replay.
	replayPage = 500
	// maxEventStreams caps concurrent /events streams. Each holds a bus subscription and a
	// connection, so a client that opens streams without closing them must not exhaust the server.
	maxEventStreams = 32
)

// events serves GET /events as a Server-Sent Events stream. With ?after=<id> it first replays
// stored events with a greater ID, then streams live ones. The subscription is taken before the
// replay, so an event persisted during the replay is delivered at least once; live events with
// an ID already sent are skipped.
//
// Headers and a ": connected" comment are sent before the replay starts, and each page of stored events is written as it is
// read, so memory use does not grow with the size of the history. Once the stream has started,
// a store error cannot become a JSON 500; the stream ends and the client reconnects.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorMsg(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	release, ok := s.tryAcquireStream()
	if !ok {
		w.Header().Set("Retry-After", "5")
		writeErrorMsg(w, http.StatusServiceUnavailable, "too many event streams")
		return
	}
	defer release()
	ctx := r.Context()
	p, ok := principalFrom(ctx)
	if !ok {
		writeErrorMsg(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	after, err := queryInt64(r.URL.Query(), "after")
	if err != nil {
		s.fail(w, err)
		return
	}

	ch, cancel := s.opts.Bus.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Firefox fires EventSource "open" only once body bytes arrive, not on the headers, so a
	// stream with nothing to replay would otherwise look disconnected until the first ping.
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	sink := sseSink{w: w, f: flusher, log: s.log}
	s.streamEvents(ctx, p, ch, after, r.URL.Query().Get("after") != "", sink,
		func(domain.Event) bool { return true })
}

// sseSink is the eventSink for GET /events: it writes SSE messages to the response and flushes
// them so the client sees each one at once.
type sseSink struct {
	w   io.Writer
	f   http.Flusher
	log *slog.Logger
}

func (k sseSink) send(e domain.Event) error { return writeEvent(k.w, k.log, e) }

func (k sseSink) flush() error {
	k.f.Flush()
	return nil
}

func (k sseSink) ping() error {
	if _, err := io.WriteString(k.w, ": ping\n\n"); err != nil {
		return err
	}
	return k.flush()
}

// stillAllowed reports whether the principal behind an open stream may still read. The API token
// always may. A signed-in user must still have a live session, still exist, and still have a role
// that allows reading; the role is read fresh, so a demotion takes effect too.
func (s *server) stillAllowed(ctx context.Context, p principal) bool {
	if p.token {
		return true
	}
	if _, err := s.opts.Store.GetSession(ctx, p.sessionID, s.now()); err != nil {
		return false
	}
	u, err := s.opts.Store.GetUser(ctx, p.user.Username)
	if err != nil {
		return false
	}
	return auth.Allows(u.Role, auth.Read)
}

// writeEvent writes one SSE message. The id line is omitted for live-only events (ID 0). An
// event that cannot be encoded is logged and skipped; only a write error is returned.
func writeEvent(w io.Writer, log *slog.Logger, e domain.Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		log.Error("api: encode event", "type", e.Type, "err", err)
		return nil
	}
	if e.ID != 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", e.ID); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
	return err
}
