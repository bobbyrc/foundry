// Package events records what happens in onlet and fans it out to live subscribers (the SSE
// stream behind every client).
package events

import (
	"context"

	"github.com/bobbyrc/onlet/internal/domain"
)

// Bus persists events and delivers them to subscribers.
type Bus interface {
	// Publish persists e (unless its type is live-only: domain.EvRunLog, domain.EvChatDelta)
	// and delivers it to every subscriber. It never blocks on a slow subscriber: a subscriber
	// whose buffer is full is dropped and its channel closed, and the client reconnects with
	// its last seen ID.
	Publish(ctx context.Context, e domain.Event) (domain.Event, error)
	// Emit is Publish with Data marshalled from v and errors logged rather than returned.
	Emit(ctx context.Context, typ, project, taskID, runID, message string, v any)
	// Subscribe returns a channel of events published after the call, and a cancel func.
	Subscribe() (<-chan domain.Event, func())
}
