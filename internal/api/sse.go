package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

const (
	// replayPage is how many stored events are read per store query during replay.
	replayPage = 500
	// pingInterval is how often a ": ping" comment keeps idle connections open.
	pingInterval = 15 * time.Second
)

// events serves GET /events as a Server-Sent Events stream. With ?after=<id> it first replays
// stored events with a greater ID, then streams live ones. The subscription is taken before the
// replay, so an event persisted during the replay is delivered at least once; live events with
// an ID already sent are skipped.
//
// Headers are sent before the replay starts and each page of stored events is written as it is
// read, so memory use does not grow with the size of the history. Once the stream has started,
// a store error cannot become a JSON 500; the stream ends and the client reconnects.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorMsg(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()
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
	flusher.Flush()

	// last is the highest event ID sent so far on this stream.
	var last int64
	if r.URL.Query().Get("after") != "" {
		cursor := after
		for {
			page, err := s.opts.Store.ListEvents(ctx, store.EventFilter{AfterID: cursor, FromStart: true, Limit: replayPage})
			if err != nil {
				s.log.Error("api: replay events", "err", err)
				return
			}
			for _, e := range page {
				if err := writeEvent(w, s.log, e); err != nil {
					return
				}
				last = e.ID
			}
			flusher.Flush()
			if len(page) < replayPage {
				break
			}
			cursor = page[len(page)-1].ID
		}
	}

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, open := <-ch:
			if !open {
				// The bus dropped this subscriber for falling behind; the client reconnects.
				return
			}
			if e.ID != 0 && e.ID <= last {
				continue
			}
			if err := writeEvent(w, s.log, e); err != nil {
				return
			}
			flusher.Flush()
			if e.ID != 0 {
				last = e.ID
			}
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
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
