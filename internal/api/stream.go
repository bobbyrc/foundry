package api

import (
	"context"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// eventSink writes to one open event stream: SSE for the CLI, a WebSocket for the UI.
type eventSink interface {
	send(e domain.Event) error // write one event
	flush() error              // push what was written to the client
	ping() error               // keep-alive; also flushes
}

// tryAcquireStream takes one of the maxEventStreams slots shared by every stream transport.
// The caller must call release when the stream ends, or the slot is lost for good.
func (s *server) tryAcquireStream() (release func(), ok bool) {
	select {
	case s.streams <- struct{}{}:
		return func() { <-s.streams }, true
	default:
		return nil, false
	}
}

// streamEvents replays stored events with an ID above after (only when replay is true), then
// sends live events from ch that keep accepts, skipping IDs already sent. Every pingInterval it
// re-checks p with stillAllowed and pings. It returns when ctx ends, a write fails, the bus
// closes ch, or p may no longer read.
func (s *server) streamEvents(ctx context.Context, p principal, ch <-chan domain.Event,
	after int64, replay bool, sink eventSink, keep func(domain.Event) bool) {
	// last is the highest event ID sent so far on this stream.
	var last int64
	if replay {
		cursor := after
		for {
			page, err := s.opts.Store.ListEvents(ctx, store.EventFilter{AfterID: cursor, FromStart: true, Limit: replayPage})
			if err != nil {
				s.log.Error("api: replay events", "err", err)
				return
			}
			for _, e := range page {
				if err := sink.send(e); err != nil {
					return
				}
				last = e.ID
			}
			if err := sink.flush(); err != nil {
				return
			}
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
			if !keep(e) {
				continue
			}
			if e.ID != 0 && e.ID <= last {
				continue
			}
			if err := sink.send(e); err != nil {
				return
			}
			if err := sink.flush(); err != nil {
				return
			}
			if e.ID != 0 {
				last = e.ID
			}
		case <-ticker.C:
			// A stream outlives the request that opened it, so the credential is checked again
			// on every ping. A signed-out or demoted user loses the stream within one interval.
			if !s.stillAllowed(ctx, p) {
				return
			}
			if err := sink.ping(); err != nil {
				return
			}
		}
	}
}
