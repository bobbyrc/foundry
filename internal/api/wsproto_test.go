package api

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestWSHandleReplies(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		kind   string
		runID  string
		convID string
	}{
		{"subscribe run", `{"kind":"subscribe","run_id":"r1"}`, "subscribed", "r1", ""},
		{"subscribe conversation", `{"kind":"subscribe","conversation_id":"c1"}`, "subscribed", "", "c1"},
		{"unsubscribe unknown run is idempotent", `{"kind":"unsubscribe","run_id":"never"}`, "unsubscribed", "never", ""},
		{"both ids", `{"kind":"subscribe","run_id":"r1","conversation_id":"c1"}`, "error", "", ""},
		{"no id", `{"kind":"subscribe"}`, "error", "", ""},
		{"empty run id", `{"kind":"subscribe","run_id":""}`, "error", "", ""},
		{"unknown kind", `{"kind":"steer","run_id":"r1"}`, "error", "", ""},
		{"unknown field", `{"kind":"subscribe","run_id":"r1","x":1}`, "error", "", ""},
		{"not json", `hello`, "error", "", ""},
		{"empty object", `{}`, "error", "", ""},
		{"trailing data", `{"kind":"subscribe","run_id":"r1"} {}`, "error", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newWSSubscriptions()
			out := s.handle([]byte(tt.input))
			assert.Equal(t, tt.kind, out.Kind)
			assert.Equal(t, tt.runID, out.RunID)
			assert.Equal(t, tt.convID, out.ConversationID)
			if tt.kind == "error" {
				assert.NotEmpty(t, out.Message)
			}
		})
	}
}

func TestWSSubscriptionLimit(t *testing.T) {
	s := newWSSubscriptions()
	for i := 0; i < maxWSSubscriptions; i++ {
		var frame string
		if i%2 == 0 {
			frame = fmt.Sprintf(`{"kind":"subscribe","run_id":"r%d"}`, i)
		} else {
			frame = fmt.Sprintf(`{"kind":"subscribe","conversation_id":"c%d"}`, i)
		}
		require.Equal(t, "subscribed", s.handle([]byte(frame)).Kind, "subscribe %d", i)
	}

	assert.Equal(t, "error", s.handle([]byte(`{"kind":"subscribe","run_id":"one-too-many"}`)).Kind)
	assert.Equal(t, "subscribed", s.handle([]byte(`{"kind":"subscribe","run_id":"r0"}`)).Kind,
		"re-subscribing an existing id does not count against the limit")

	require.Equal(t, "unsubscribed", s.handle([]byte(`{"kind":"unsubscribe","run_id":"r0"}`)).Kind)
	assert.Equal(t, "subscribed", s.handle([]byte(`{"kind":"subscribe","run_id":"one-too-many"}`)).Kind,
		"freeing a slot lets a new id in")
}

func TestWSWants(t *testing.T) {
	s := newWSSubscriptions()
	require.Equal(t, "subscribed", s.handle([]byte(`{"kind":"subscribe","run_id":"r1"}`)).Kind)
	require.Equal(t, "subscribed", s.handle([]byte(`{"kind":"subscribe","conversation_id":"c1"}`)).Kind)

	tests := []struct {
		name  string
		event domain.Event
		want  bool
	}{
		{"run log for subscribed run", domain.Event{Type: domain.EvRunLog, RunID: "r1"}, true},
		{"run log for other run", domain.Event{Type: domain.EvRunLog, RunID: "r2"}, false},
		{"chat delta for subscribed conversation", domain.Event{Type: domain.EvChatDelta, Data: []byte(`{"conversation_id":"c1","text":"x"}`)}, true},
		{"chat delta for other conversation", domain.Event{Type: domain.EvChatDelta, Data: []byte(`{"conversation_id":"c2","text":"x"}`)}, false},
		{"chat delta with bad data", domain.Event{Type: domain.EvChatDelta, Data: []byte(`not json`)}, false},
		{"unscoped event type always goes", domain.Event{Type: "task.updated"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, s.wants(tt.event))
		})
	}

	require.Equal(t, "unsubscribed", s.handle([]byte(`{"kind":"unsubscribe","run_id":"r1"}`)).Kind)
	assert.False(t, s.wants(domain.Event{Type: domain.EvRunLog, RunID: "r1"}),
		"run log stops after unsubscribe")
}
