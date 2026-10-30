package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func TestConversationByTask(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, domain.Conversation{Title: "task agent", TaskID: "t1"})
	require.NoError(t, err)

	got, err := s.GetConversationByTask(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, "t1", got.TaskID)

	_, err = s.GetConversationByTask(ctx, "t2")
	assert.ErrorIs(t, err, store.ErrNotFound)

	orchestrator, err := s.ListConversations(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, orchestrator, "task agent conversations are not orchestrator conversations")

	taskConvs, err := s.ListTaskConversations(ctx, 0)
	require.NoError(t, err)
	require.Len(t, taskConvs, 1)
	assert.Equal(t, c.ID, taskConvs[0].ID)
}

func TestUpdateAndDeleteChatMessage(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	conv, err := s.CreateConversation(ctx, domain.Conversation{Title: "chat"})
	require.NoError(t, err)
	m, err := s.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: "hi", Queued: true,
	})
	require.NoError(t, err)

	updated, err := s.UpdateChatMessage(ctx, m.ID, func(msg *domain.ChatMessage) error {
		msg.Queued = false
		msg.RunID = "r1"
		return nil
	})
	require.NoError(t, err)
	assert.False(t, updated.Queued)

	msgs, err := s.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].Queued)
	assert.Equal(t, "r1", msgs[0].RunID)

	require.NoError(t, s.DeleteChatMessage(ctx, m.ID))
	msgs, err = s.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	assert.Empty(t, msgs)

	assert.ErrorIs(t, s.DeleteChatMessage(ctx, "no-such-message"), store.ErrNotFound)
}

func TestDeleteChatMessageIfRespectsCheck(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	conv, err := s.CreateConversation(ctx, domain.Conversation{})
	require.NoError(t, err)
	m, err := s.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: conv.ID, Role: "user", Text: "x", Queued: true})
	require.NoError(t, err)

	errNo := errors.New("not queued")
	needQueued := func(m domain.ChatMessage) error {
		if !m.Queued {
			return errNo
		}
		return nil
	}

	_, err = s.UpdateChatMessage(ctx, m.ID, func(m *domain.ChatMessage) error { m.Queued = false; return nil })
	require.NoError(t, err)
	_, err = s.DeleteChatMessageIf(ctx, m.ID, needQueued)
	assert.Equal(t, errNo, err, "the check's error comes back unchanged")
	msgs, err := s.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1, "a failed check leaves the row in place")

	_, err = s.UpdateChatMessage(ctx, m.ID, func(m *domain.ChatMessage) error { m.Queued = true; return nil })
	require.NoError(t, err)
	got, err := s.DeleteChatMessageIf(ctx, m.ID, needQueued)
	require.NoError(t, err)
	assert.Equal(t, m.ID, got.ID)
	assert.True(t, got.Queued)
	msgs, err = s.ListChatMessages(ctx, conv.ID)
	require.NoError(t, err)
	assert.Empty(t, msgs)

	_, err = s.DeleteChatMessageIf(ctx, m.ID, needQueued)
	assert.ErrorIs(t, err, store.ErrNotFound)
}
