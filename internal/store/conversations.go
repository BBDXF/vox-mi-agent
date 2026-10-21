package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Channels a conversation may originate from.
const (
	ChannelWeb     = "web"
	ChannelCLI     = "cli"
	ChannelSpeaker = "speaker"
)

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             int64           `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	ToolCalls      json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	Name           string          `json:"name,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// CreateConversation creates a conversation on the given channel
// (web|cli|speaker; enforced by the schema).
func (s *Store) CreateConversation(ctx context.Context, title, channel string) (*Conversation, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	c := &Conversation{ID: newID(), Title: title, Channel: channel, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations(id,title,channel,created_at,updated_at) VALUES(?,?,?,?,?)`,
		c.ID, c.Title, c.Channel, ms(now), ms(now))
	if err != nil {
		return nil, fmt.Errorf("store: create conversation: %w", err)
	}
	return c, nil
}

// ListConversations returns conversations ordered by updated_at desc.
// before (if non-zero) restricts to updated_at < before, for pagination.
// limit <= 0 defaults to 50.
func (s *Store) ListConversations(ctx context.Context, limit int, before time.Time) ([]Conversation, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id,title,channel,created_at,updated_at FROM conversations`
	var args []any
	if !before.IsZero() {
		q += ` WHERE updated_at < ?`
		args = append(args, ms(before))
	}
	q += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var cr, up int64
		if err := rows.Scan(&c.ID, &c.Title, &c.Channel, &cr, &up); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = fromMS(cr), fromMS(up)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConversation returns ErrNotFound if id does not exist.
func (s *Store) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	var c Conversation
	var cr, up int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,title,channel,created_at,updated_at FROM conversations WHERE id=?`, id).
		Scan(&c.ID, &c.Title, &c.Channel, &cr, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt, c.UpdatedAt = fromMS(cr), fromMS(up)
	return &c, nil
}

// AppendMessage stores m under convID (m.ConversationID is ignored), bumps the
// conversation's updated_at, and returns the stored message with ID/CreatedAt set.
func (s *Store) AppendMessage(ctx context.Context, convID string, m Message) (*Message, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	var tc any
	if len(m.ToolCalls) > 0 {
		if !json.Valid(m.ToolCalls) {
			return nil, errors.New("store: tool_calls is not valid JSON")
		}
		tc = string(m.ToolCalls)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages(conversation_id,role,content,tool_calls,tool_call_id,name,created_at) VALUES(?,?,?,?,?,?,?)`,
		convID, m.Role, m.Content, tc, m.ToolCallID, m.Name, ms(now))
	if err != nil {
		return nil, fmt.Errorf("store: append message: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, ms(now), convID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	m.ID, m.ConversationID, m.CreatedAt = id, convID, now
	return &m, nil
}

// ListMessages returns all messages of a conversation in insertion order.
func (s *Store) ListMessages(ctx context.Context, convID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,conversation_id,role,content,tool_calls,tool_call_id,name,created_at
		 FROM messages WHERE conversation_id=? ORDER BY id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var tc sql.NullString
		var cr int64
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &tc, &m.ToolCallID, &m.Name, &cr); err != nil {
			return nil, err
		}
		if tc.Valid {
			m.ToolCalls = json.RawMessage(tc.String)
		}
		m.CreatedAt = fromMS(cr)
		out = append(out, m)
	}
	return out, rows.Err()
}
