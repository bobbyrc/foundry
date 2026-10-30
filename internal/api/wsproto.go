package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/bobbyrc/onett/internal/domain"
)

const (
	// wsReadLimit caps one client frame. Subscribe frames are a few dozen bytes, so a
	// larger frame is a mistake or an attack, and the socket drops it.
	wsReadLimit = 4096
	// maxWSSubscriptions caps how many runs and conversations one socket can follow, so a
	// single client cannot make the server filter against an unbounded list.
	maxWSSubscriptions = 64
)

// wsOut is one server-to-client frame. Kind is "event", "subscribed", "unsubscribed" or "error".
type wsOut struct {
	Kind           string        `json:"kind"`
	Event          *domain.Event `json:"event,omitempty"`
	RunID          string        `json:"run_id,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Message        string        `json:"message,omitempty"`
}

// wsIn is one client-to-server frame.
type wsIn struct {
	Kind           string `json:"kind"`
	RunID          string `json:"run_id"`
	ConversationID string `json:"conversation_id"`
}

// wsSubscriptions records which runs and conversations one socket follows. The reader
// goroutine changes it and the writer goroutine reads it, so every access takes the lock.
type wsSubscriptions struct {
	mu    sync.Mutex
	runs  map[string]bool
	convs map[string]bool
}

func newWSSubscriptions() *wsSubscriptions {
	return &wsSubscriptions{runs: map[string]bool{}, convs: map[string]bool{}}
}

// handle decodes one client frame strictly, applies it, and returns the reply to send.
func (s *wsSubscriptions) handle(raw []byte) wsOut {
	in, err := decodeWSIn(raw)
	if err != nil {
		return wsOut{Kind: "error", Message: err.Error()}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	subscribe := in.Kind == "subscribe"
	if in.RunID != "" {
		if !subscribe {
			delete(s.runs, in.RunID)
			return wsOut{Kind: "unsubscribed", RunID: in.RunID}
		}
		if !s.runs[in.RunID] {
			if s.count() >= maxWSSubscriptions {
				return wsOut{Kind: "error", Message: errWSLimit.Error()}
			}
			s.runs[in.RunID] = true
		}
		return wsOut{Kind: "subscribed", RunID: in.RunID}
	}

	if !subscribe {
		delete(s.convs, in.ConversationID)
		return wsOut{Kind: "unsubscribed", ConversationID: in.ConversationID}
	}
	if !s.convs[in.ConversationID] {
		if s.count() >= maxWSSubscriptions {
			return wsOut{Kind: "error", Message: errWSLimit.Error()}
		}
		s.convs[in.ConversationID] = true
	}
	return wsOut{Kind: "subscribed", ConversationID: in.ConversationID}
}

// wants reports whether a live event goes to this socket. Run logs and chat deltas are
// scoped to the run or conversation they belong to; every other event type goes to all
// sockets.
func (s *wsSubscriptions) wants(e domain.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch e.Type {
	case domain.EvRunLog:
		return s.runs[e.RunID]
	case domain.EvChatDelta:
		var d struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return false
		}
		return s.convs[d.ConversationID]
	}
	return true
}

// count is the number of distinct subscriptions. The caller must hold s.mu.
func (s *wsSubscriptions) count() int {
	return len(s.runs) + len(s.convs)
}

var (
	errWSLimit = errors.New("too many subscriptions on this socket; unsubscribe from one first")
	errWSFrame = errors.New(`frame must be one JSON object with "kind" set to "subscribe" or "unsubscribe" and exactly one of "run_id" or "conversation_id"`)
)

// decodeWSIn parses one client frame. Unknown fields and trailing data are errors, so a
// typo in a field name is reported instead of silently ignored.
func decodeWSIn(raw []byte) (wsIn, error) {
	var in wsIn
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return wsIn{}, errWSFrame
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return wsIn{}, errWSFrame
	}
	if in.Kind != "subscribe" && in.Kind != "unsubscribe" {
		return wsIn{}, errWSFrame
	}
	if (in.RunID == "") == (in.ConversationID == "") {
		return wsIn{}, errWSFrame
	}
	return in, nil
}
