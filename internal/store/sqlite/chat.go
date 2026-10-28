package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

var conversations = entity[domain.Conversation]{
	name:  "conversation",
	table: "conversations",
	id:    func(c *domain.Conversation) *string { return &c.ID },
	stamp: func(c *domain.Conversation, now time.Time) { c.UpdatedAt = now },
	cols: func(c domain.Conversation) []col {
		return []col{
			{"id", c.ID},
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

// ListConversations returns conversations most recently updated first.
func (s *Store) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	out, err := queryDocs[domain.Conversation](ctx, s.db,
		"SELECT data FROM conversations ORDER BY updated_at DESC, id DESC LIMIT ?", limitOr(limit, 200))
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return out, nil
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
