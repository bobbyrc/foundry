package fake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

func TestFakeSteerableDeliversToInbox(t *testing.T) {
	h := NewSteerable(func(ctx context.Context, spec harness.Spec, inbox <-chan string, emit func(harness.Event)) (harness.Result, error) {
		select {
		case m := <-inbox:
			emit(harness.Event{Kind: domain.LogText, Text: m})
		case <-ctx.Done():
		}
		return harness.Result{}, nil
	})
	sess, err := h.Start(context.Background(), harness.Spec{})
	require.NoError(t, err)
	st, ok := sess.(harness.Steerable)
	require.True(t, ok)
	require.NoError(t, st.Send("hi"))

	ev := <-sess.Events()
	assert.Equal(t, "hi", ev.Text)
	_, err = sess.Wait()
	require.NoError(t, err)
	assert.ErrorIs(t, st.Send("again"), harness.ErrInputClosed)
}
