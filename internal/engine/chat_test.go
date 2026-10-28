package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/domain"

	// Registers the "fake" harness type that the test config uses.
	_ "github.com/bobbyrc/foundry/internal/harness/fake"
)

// ctWaitIdle waits until the conversation is no longer busy and returns it.
func ctWaitIdle(t *testing.T, r *ctRig, convID string) domain.Conversation {
	t.Helper()
	var conv domain.Conversation
	require.Eventually(t, func() bool {
		c, err := r.st.GetConversation(context.Background(), convID)
		if err != nil {
			return false
		}
		conv = c
		return !c.Busy
	}, 10*time.Second, 10*time.Millisecond, "conversation stayed busy")
	return conv
}

// ctActiveRunID returns the ID of the one live chat run, once it is registered.
func ctActiveRunID(t *testing.T, e *Engine) string {
	t.Helper()
	var id string
	require.Eventually(t, func() bool {
		e.runsMu.Lock()
		defer e.runsMu.Unlock()
		for runID := range e.active {
			id = runID
			return true
		}
		return false
	}, 10*time.Second, 5*time.Millisecond, "no live chat run")
	return id
}

func TestChatSendReplyAndDeltas(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	events, unsubscribe := r.bus.Subscribe()
	defer unsubscribe()

	msg, err := e.SendChat(ctx, conv.ID, "  hello there  ")
	require.NoError(t, err)
	assert.Equal(t, "user", msg.Role)
	assert.Equal(t, "hello there", msg.Text)

	done := ctWaitIdle(t, r, conv.ID)
	assert.NotEmpty(t, done.SessionID, "the harness session is kept for the next turn")
	assert.Equal(t, "hello there", done.Title, "an untitled conversation takes its first message as title")

	msgs, err := r.st.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "fake: nothing to do", msgs[1].Text)
	assert.NotEmpty(t, msgs[1].RunID, "the reply names the run that produced it")

	run, err := r.st.GetRun(ctx, msgs[1].RunID)
	require.NoError(t, err)
	assert.Equal(t, domain.RunSucceeded, run.Status)
	assert.Equal(t, "orchestrator", run.Role)

	var sawDelta, sawReply bool
	deadline := time.After(5 * time.Second)
	for !(sawDelta && sawReply) {
		select {
		case ev := <-events:
			switch ev.Type {
			case domain.EvChatDelta:
				var data struct {
					ConversationID string `json:"conversation_id"`
					RunID          string `json:"run_id"`
					Text           string `json:"text"`
				}
				require.NoError(t, json.Unmarshal(ev.Data, &data))
				assert.Equal(t, conv.ID, data.ConversationID)
				assert.Equal(t, msgs[1].RunID, data.RunID)
				assert.NotEmpty(t, data.Text)
				sawDelta = true
			case domain.EvChatMessage:
				sawReply = sawReply || strings.Contains(string(ev.Data), "fake: nothing to do")
			}
		case <-deadline:
			t.Fatalf("missing events: delta=%v reply=%v", sawDelta, sawReply)
		}
	}
}

func TestChatSecondSendWhileBusyConflicts(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	// [fake:stall] keeps the reply running until it is cancelled.
	_, err = e.SendChat(ctx, conv.ID, "[fake:stall] think for a while")
	require.NoError(t, err)
	runID := ctActiveRunID(t, e)

	_, err = e.SendChat(ctx, conv.ID, "are you there?")
	assert.True(t, errors.Is(err, api.ErrConflict), "second send while busy: %v", err)

	busy, err := r.st.GetConversation(ctx, conv.ID)
	require.NoError(t, err)
	assert.True(t, busy.Busy)

	require.NoError(t, e.CancelRun(ctx, runID))
	done := ctWaitIdle(t, r, conv.ID)
	assert.False(t, done.Busy)

	msgs, err := r.st.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2, "the refused send is not stored")
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, runID, msgs[1].RunID)
	assert.Contains(t, msgs[1].Text, "_(The orchestrator run ended: cancelled")
}

func TestChatInstructionsListProjectsAndAgents(t *testing.T) {
	r := ctNewRig(t)
	r.cfg.Projects["demo"].Paused = true
	conv := domain.Conversation{Project: "demo"}

	text := chatInstructions(r.cfg, conv, "BASE PROMPT")
	assert.True(t, strings.HasPrefix(text, "BASE PROMPT\n\n## The factory right now\n\n"))
	assert.Contains(t, text, "- **demo** — /nonexistent/demo.git, "+r.cfg.Projects["demo"].Path+", paused: yes")
	assert.Contains(t, text, "- **implementer** (codex) — ")
	assert.Contains(t, text, "handles: bugfix, chore, feature, issue")
	assert.Contains(t, text, "\n\nDefault project for this conversation: demo.")

	none := chatInstructions(r.cfg, domain.Conversation{}, "BASE")
	assert.NotContains(t, none, "Default project for this conversation")
}

func TestChatRoutedKindsForUnknownAgent(t *testing.T) {
	r := ctNewRig(t)
	assert.Equal(t, "none", handledKinds(r.cfg, "nobody"))
}
