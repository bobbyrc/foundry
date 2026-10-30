package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var conversations = entity[domain.Conversation]{
	name:  "conversation",
	table: "conversations",
	id:    func(c *domain.Conversation) *string { return &c.ID },
	stamp: func(c *domain.Conversation, now time.Time) { c.UpdatedAt = now },
	cols: func(c domain.Conversation) []col {
		return []col{
			{"id", c.ID},
			{"task_id", c.TaskID},
			{"updated_at", nanos(c.UpdatedAt)},
		}
	},
}

var chatMessages = entity[domain.ChatMessage]{
	name:  "chat message",
	table: "chat_messages",
	id:    func(m *domain.ChatMessage) *string { return &m.ID },
	cols: func(m domain.ChatMessage) []col {
		return []col{
			{"id", m.ID},
			{"conversation_id", m.ConversationID},
			{"created_at", nanos(m.CreatedAt)},
		}
	},
}

// CreateConversation stores c, assigning ID and timestamps when empty.
func (s *Store) CreateConversation(ctx context.Context, c domain.Conversation) (domain.Conversation, error) {
	if c.ID == "" {
		c.ID = newID()
	}
	now := nowUTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	if err := conversations.insert(ctx, s.db, c); err != nil {
		return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return c, nil
}

// GetConversation returns the conversation with id, or ErrNotFound.
func (s *Store) GetConversation(ctx context.Context, id string) (domain.Conversation, error) {
	return conversations.get(ctx, s.db, id)
}

// ListConversations returns orchestrator conversations most recently updated first.
func (s *Store) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	out, err := queryDocs[domain.Conversation](ctx, s.db,
		"SELECT data FROM conversations WHERE task_id = '' ORDER BY updated_at DESC, id DESC LIMIT ?",
		limitOr(limit, 200))
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return out, nil
}

// ListTaskConversations returns task agent conversations most recently updated first.
func (s *Store) ListTaskConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	out, err := queryDocs[domain.Conversation](ctx, s.db,
		"SELECT data FROM conversations WHERE task_id != '' ORDER BY updated_at DESC, id DESC LIMIT ?",
		limitOr(limit, 200))
	if err != nil {
		return nil, fmt.Errorf("list task conversations: %w", err)
	}
	return out, nil
}

// GetConversationByTask returns the conversation of the task agent for taskID, or ErrNotFound.
// An empty taskID never matches: that is the orchestrator's conversation, not a task's.
func (s *Store) GetConversationByTask(ctx context.Context, taskID string) (domain.Conversation, error) {
	if taskID == "" {
		return domain.Conversation{}, fmt.Errorf("conversation for task \"\": %w", store.ErrNotFound)
	}
	return queryOne[domain.Conversation](ctx, s.db, "conversation for task "+taskID,
		"SELECT data FROM conversations WHERE task_id = ? ORDER BY updated_at DESC, id DESC LIMIT 1",
		taskID)
}

// UpdateConversation applies fn to the conversation with id in one transaction.
func (s *Store) UpdateConversation(ctx context.Context, id string, fn func(*domain.Conversation) error) (domain.Conversation, error) {
	return conversations.update(ctx, s.db, id, fn)
}

// AppendChatMessage stores m, assigning ID and CreatedAt when empty.
func (s *Store) AppendChatMessage(ctx context.Context, m domain.ChatMessage) (domain.ChatMessage, error) {
	if m.ID == "" {
		m.ID = newID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = nowUTC()
	}
	if err := chatMessages.insert(ctx, s.db, m); err != nil {
		return domain.ChatMessage{}, fmt.Errorf("append chat message: %w", err)
	}
	return m, nil
}

// ListChatMessages returns a conversation's messages in ascending order.
func (s *Store) ListChatMessages(ctx context.Context, conversationID string) ([]domain.ChatMessage, error) {
	out, err := queryDocs[domain.ChatMessage](ctx, s.db,
		`SELECT data FROM chat_messages WHERE conversation_id = ? ORDER BY created_at ASC, id ASC`,
		conversationID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages %s: %w", conversationID, err)
	}
	return out, nil
}

// UpdateChatMessage applies fn to the message with id in one transaction.
func (s *Store) UpdateChatMessage(ctx context.Context, id string, fn func(*domain.ChatMessage) error) (domain.ChatMessage, error) {
	return chatMessages.update(ctx, s.db, id, fn)
}

// DeleteChatMessage removes the message with id, or returns ErrNotFound.
func (s *Store) DeleteChatMessage(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete chat message %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete chat message %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("chat message %s: %w", id, store.ErrNotFound)
	}
	return nil
}
