package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// startChatRun starts a chat turn on h and returns once its run is live.
func startChatRun(t *testing.T, h harness.Harness) (*ctRig, *Engine, string, string) {
	t.Helper()
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) { return h, nil }
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	_, err = e.SendChat(ctx, conv.ID, "go")
	require.NoError(t, err)
	return r, e, conv.ID, ctActiveRunID(t, e)
}

func TestDeliverToLiveSteerableRun(t *testing.T) {
	ctx := context.Background()
	got := make(chan string, 1)
	release := make(chan struct{})
	h := hfake.NewSteerable(func(ctx context.Context, _ harness.Spec, inbox <-chan string, _ func(harness.Event)) (harness.Result, error) {
		select {
		case m := <-inbox:
			got <- m
		case <-ctx.Done():
		}
		<-release
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID, runID := startChatRun(t, h)

	require.Eventually(t, func() bool {
		return e.deliverToRun(ctx, runID, "hi") == nil
	}, 5*time.Second, 5*time.Millisecond)

	assert.Equal(t, ownerPrompt([]string{"hi"}), <-got)
	logs, err := r.st.ListRunLogs(ctx, runID, 0, 100)
	require.NoError(t, err)
	var owner []string
	for _, l := range logs {
		if l.Kind == domain.LogOwner {
			owner = append(owner, l.Text)
		}
	}
	assert.Equal(t, []string{"hi"}, owner)
	close(release)
	ctWaitIdle(t, r, convID)
}

func TestDeliverToNonSteerableRun(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	h := hfake.New(func(ctx context.Context, _ harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		<-release
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID, runID := startChatRun(t, h)

	require.Eventually(t, func() bool {
		return !errors.Is(e.deliverToRun(ctx, runID, "hi"), errRunNotLive)
	}, 5*time.Second, 5*time.Millisecond)
	assert.ErrorIs(t, e.deliverToRun(ctx, runID, "hi"), harness.ErrInputClosed)

	logs, err := r.st.ListRunLogs(ctx, runID, 0, 100)
	require.NoError(t, err)
	for _, l := range logs {
		assert.NotEqual(t, domain.LogOwner, l.Kind)
	}
	close(release)
	ctWaitIdle(t, r, convID)
}

func TestDeliverToUnknownRun(t *testing.T) {
	r := ctNewRig(t)
	e := r.engine(nil)
	assert.ErrorIs(t, e.deliverToRun(context.Background(), "nope", "hi"), errRunNotLive)
}

func TestSendRacingRunEndIsQueuedOrDelivered(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var received []string
	enough := make(chan struct{})
	h := hfake.NewSteerable(func(ctx context.Context, _ harness.Spec, inbox <-chan string, _ func(harness.Event)) (harness.Result, error) {
		for {
			select {
			case m := <-inbox:
				mu.Lock()
				received = append(received, m)
				n := len(received)
				mu.Unlock()
				if n == 20 {
					close(enough)
				}
				if n >= 20 {
					return harness.Result{SessionID: "s", FinalText: "ok"}, nil
				}
			case <-ctx.Done():
				return harness.Result{}, ctx.Err()
			}
		}
	})
	r, e, convID, runID := startChatRun(t, h)

	var delivered []string
	sawEnd := false
	for i := 0; i < 100000 && !sawEnd; i++ {
		text := "m" + time.Duration(i).String()
		err := e.deliverToRun(ctx, runID, text)
		switch {
		case err == nil:
			delivered = append(delivered, text)
		case errors.Is(err, errRunNotLive) && len(delivered) == 0:
			// not started yet
		default:
			require.True(t, errors.Is(err, errRunNotLive) || errors.Is(err, harness.ErrInputClosed), "unexpected error: %v", err)
			sawEnd = true
		}
	}
	require.True(t, sawEnd, "run never ended")
	<-enough
	ctWaitIdle(t, r, convID)

	mu.Lock()
	defer mu.Unlock()
	set := map[string]bool{}
	for _, m := range received {
		set[m] = true
	}
	require.NotEmpty(t, delivered)
	for _, d := range delivered {
		assert.True(t, set[ownerPrompt([]string{d})], "delivered but never received: %s", d)
	}
}
