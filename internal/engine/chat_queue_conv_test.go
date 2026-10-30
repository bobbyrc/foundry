package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// cqRig builds an engine whose orchestrator uses h, with one empty conversation.
func cqRig(t *testing.T, h harness.Harness) (*ctRig, *Engine, string) {
	t.Helper()
	r := ctNewRig(t)
	e := r.engine(nil)
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) { return h, nil }
	conv, err := r.st.CreateConversation(context.Background(), domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	return r, e, conv.ID
}

// cqWaitSession waits until the live run has a session, so a send can reach the agent.
func cqWaitSession(t *testing.T, e *Engine, runID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		e.runsMu.Lock()
		ar, ok := e.active[runID]
		e.runsMu.Unlock()
		return ok && ar.session() != nil
	}, 10*time.Second, 5*time.Millisecond, "run never got a session")
}

func TestChatMessageWhileBusyIsDeliveredToSteerableTurn(t *testing.T) {
	ctx := context.Background()
	got := make(chan string, 1)
	h := hfake.NewSteerable(func(ctx context.Context, _ harness.Spec, inbox <-chan string, _ func(harness.Event)) (harness.Result, error) {
		select {
		case m := <-inbox:
			got <- m
		case <-ctx.Done():
		}
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	_, err := e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	runID := ctActiveRunID(t, e)
	cqWaitSession(t, e, runID)

	msg, err := e.SendChat(ctx, convID, "also this")
	require.NoError(t, err)
	assert.Equal(t, runID, msg.RunID, "a message the live agent accepted names its run")
	assert.False(t, msg.Queued)
	assert.Equal(t, ownerPrompt([]string{"also this"}), <-got)
	ctWaitIdle(t, r, convID)
}

func TestChatMessageWhileBusyIsQueuedForNonSteerableTurn(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	var mu sync.Mutex
	var prompts []string
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		prompts = append(prompts, spec.Prompt)
		n := len(prompts)
		mu.Unlock()
		if n == 1 {
			<-release
		}
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	_, err := e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	runID := ctActiveRunID(t, e)
	cqWaitSession(t, e, runID)

	busy, err := r.st.GetConversation(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, runID, busy.ActiveRunID)

	m1, err := e.SendChat(ctx, convID, "first")
	require.NoError(t, err)
	m2, err := e.SendChat(ctx, convID, "second")
	require.NoError(t, err)
	assert.True(t, m1.Queued)
	assert.True(t, m2.Queued)
	assert.Empty(t, m1.RunID)

	close(release)
	// The successful turn is followed by one turn with both queued messages.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(prompts) == 2
	}, 10*time.Second, 10*time.Millisecond)
	done := ctWaitIdle(t, r, convID)
	assert.Empty(t, done.ActiveRunID)

	mu.Lock()
	assert.Equal(t, "first\n\nsecond", prompts[1])
	mu.Unlock()

	msgs, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	var nextRun string
	for _, m := range msgs {
		if m.ID == m1.ID || m.ID == m2.ID {
			assert.False(t, m.Queued)
			assert.NotEmpty(t, m.RunID)
			assert.NotEqual(t, runID, m.RunID)
			if nextRun != "" {
				assert.Equal(t, nextRun, m.RunID, "both ride one turn")
			}
			nextRun = m.RunID
		}
	}
	assert.NotEmpty(t, nextRun)
}

func TestFlushChatSendsQueuedMessages(t *testing.T) {
	// FlushChat from idle sends the queue as one turn and stamps the messages with its run; an
	// empty queue is a no-op.
	ctx := context.Background()
	prompts := make(chan string, 4)
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		prompts <- spec.Prompt
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	// Seed two queued messages on an idle conversation.
	var ids []string
	for _, text := range []string{"one", "two"} {
		m, err := r.st.AppendChatMessage(ctx, domain.ChatMessage{
			ConversationID: convID, Role: "user", Text: text, Queued: true,
		})
		require.NoError(t, err)
		ids = append(ids, m.ID)
	}
	require.NoError(t, e.FlushChat(ctx, convID))
	assert.Equal(t, "one\n\ntwo", <-prompts)
	ctWaitIdle(t, r, convID)

	msgs, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	var runID string
	for _, m := range msgs {
		if m.Role == "assistant" {
			runID = m.RunID
		}
	}
	require.NotEmpty(t, runID)
	for _, m := range msgs {
		if m.ID == ids[0] || m.ID == ids[1] {
			assert.False(t, m.Queued)
			assert.Equal(t, runID, m.RunID)
		}
	}

	// An empty queue is a no-op.
	require.NoError(t, e.FlushChat(ctx, convID))
	select {
	case p := <-prompts:
		t.Fatalf("flush of an empty queue started a turn: %q", p)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestQueuedChatHeldAfterCancelledTurn(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var prompts []string
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		prompts = append(prompts, spec.Prompt)
		n := len(prompts)
		mu.Unlock()
		if n == 1 {
			<-ctx.Done()
			return harness.Result{}, ctx.Err()
		}
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	_, err := e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	runID := ctActiveRunID(t, e)
	cqWaitSession(t, e, runID)
	m1, err := e.SendChat(ctx, convID, "queued one")
	require.NoError(t, err)
	require.True(t, m1.Queued)

	require.NoError(t, e.CancelRun(ctx, runID))
	ctWaitIdle(t, r, convID)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Len(t, prompts, 1, "a cancelled turn does not send the queue")
	mu.Unlock()
	got, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	for _, m := range got {
		if m.ID == m1.ID {
			assert.True(t, m.Queued)
		}
	}

	_, err = e.SendChat(ctx, convID, "new text")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(prompts) == 2
	}, 10*time.Second, 10*time.Millisecond)
	mu.Lock()
	assert.Equal(t, "queued one\n\nnew text", prompts[1])
	mu.Unlock()
	ctWaitIdle(t, r, convID)
}

func TestEditQueuedMessageAfterDeliveryConflicts(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	queued, err := r.st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: "draft", Queued: true,
	})
	require.NoError(t, err)
	edited, err := e.EditQueuedMessage(ctx, queued.ID, "  better  ")
	require.NoError(t, err)
	assert.Equal(t, "better", edited.Text)
	assert.True(t, edited.Queued)

	_, err = e.EditQueuedMessage(ctx, queued.ID, "  ")
	assert.ErrorIs(t, err, api.ErrInvalid)

	delivered, err := r.st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: "sent", RunID: "run-1",
	})
	require.NoError(t, err)
	_, err = e.EditQueuedMessage(ctx, delivered.ID, "changed")
	assert.True(t, errors.Is(err, api.ErrConflict), "edit after delivery: %v", err)
	err = e.CancelQueuedMessage(ctx, delivered.ID)
	assert.True(t, errors.Is(err, api.ErrConflict), "cancel after delivery: %v", err)

	require.NoError(t, e.CancelQueuedMessage(ctx, queued.ID))
	msgs, err := r.st.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, delivered.ID, msgs[0].ID)
}

func TestQueuedChatSentAfterSuccessfulTurn(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	prompts := make(chan string, 4)
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		prompts <- spec.Prompt
		if spec.Prompt == "go" {
			<-release
		}
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	_, err := e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	assert.Equal(t, "go", <-prompts)
	runID := ctActiveRunID(t, e)
	cqWaitSession(t, e, runID)
	queued, err := e.SendChat(ctx, convID, "later")
	require.NoError(t, err)
	require.True(t, queued.Queued)

	close(release)
	assert.Equal(t, "later", <-prompts, "the success is followed by a turn with the queue")
	ctWaitIdle(t, r, convID)
	msgs, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	for _, m := range msgs {
		if m.ID == queued.ID {
			assert.False(t, m.Queued)
			assert.NotEmpty(t, m.RunID)
			assert.NotEqual(t, runID, m.RunID)
		}
	}
}

func TestQueuedChatHeldAfterFailedTurn(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	var mu sync.Mutex
	n := 0
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		n++
		mu.Unlock()
		<-release
		return harness.Result{}, errors.New("boom")
	})
	r, e, convID := cqRig(t, h)

	_, err := e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	cqWaitSession(t, e, ctActiveRunID(t, e))
	queued, err := e.SendChat(ctx, convID, "wait for me")
	require.NoError(t, err)
	require.True(t, queued.Queued)

	close(release)
	ctWaitIdle(t, r, convID)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 1, n, "a failed turn does not send the queue")
	mu.Unlock()
	msgs, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	found := false
	for _, m := range msgs {
		if m.ID == queued.ID {
			found = true
			assert.True(t, m.Queued)
		}
	}
	assert.True(t, found)
}

func TestCancelQueuedMessageAfterClaimConflicts(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	m, err := r.st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: "sent", Queued: true,
	})
	require.NoError(t, err)

	claimed, err := e.claimQueued(ctx, conv.ID)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	err = e.CancelQueuedMessage(ctx, m.ID)
	assert.True(t, errors.Is(err, api.ErrConflict), "cancel after a turn took it: %v", err)
	msgs, err := r.st.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1, "the taken message is not deleted")
}

func TestClaimedMessagesRestoredWhenTurnCannotStart(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
		return nil, errors.New("no such harness")
	}
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	old, err := r.st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: "old", Queued: true,
	})
	require.NoError(t, err)

	fresh, err := e.SendChat(ctx, conv.ID, "new")
	require.NoError(t, err)
	ctWaitIdle(t, r, conv.ID)

	msgs, err := r.st.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	byID := map[string]domain.ChatMessage{}
	var reply string
	for _, m := range msgs {
		byID[m.ID] = m
		if m.Role == "assistant" {
			reply = m.Text
		}
	}
	assert.Contains(t, reply, "I can't run")
	assert.True(t, byID[old.ID].Queued, "the old message waits for the next send")
	assert.True(t, byID[fresh.ID].Queued, "so does the one that rode with it")
	assert.Empty(t, byID[old.ID].RunID)
}
