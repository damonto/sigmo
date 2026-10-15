package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// Conversation identifies a correspondent on a SIM, independently of its activation.
type Conversation struct {
	ID                   string  `json:"id"`
	ProfileID            string  `json:"profileId"`
	Participant          string  `json:"participant"`
	ModemID              string  `json:"modemId"`
	LineLabel            string  `json:"lineLabel"`
	LineNumber           string  `json:"lineNumber"`
	ReadThroughMessageID int64   `json:"readThroughMessageId"`
	MarkedUnread         bool    `json:"markedUnread"`
	UnreadCount          int     `json:"unreadCount"`
	LastMessageID        int64   `json:"lastMessageId"`
	Latest               Message `json:"-"`
}

// ConversationFilter scopes history without requiring an active modem.
type ConversationFilter struct {
	ModemID    string
	ProfileID  string
	Query      string
	Unassigned bool
}

// MessageState is a versioned backend-wide unread snapshot.
type MessageState struct {
	BackendID   string `json:"backendId"`
	Revision    int64  `json:"revision"`
	UnreadCount int    `json:"unreadCount"`
}

// ConversationSnapshot keeps the list and its badge in one database snapshot.
type ConversationSnapshot struct {
	MessageState
	Items []Conversation
}

// ReadUpdate acknowledges only messages that the client actually displayed.
type ReadUpdate struct {
	ConversationID       string
	ReadThroughMessageID *int64 `json:"readThroughMessageId,omitempty"`
	MarkedUnread         *bool  `json:"markedUnread,omitempty"`
}

// ErrInvalidReadUpdate indicates an acknowledgment outside the selected conversation.
var ErrInvalidReadUpdate = errors.New("invalid conversation read update")

// ConversationID remains stable when the SIM moves or its modem is restarted.
func ConversationID(profileID, participant string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(profileID) + "\x00" + strings.TrimSpace(participant)))
	return hex.EncodeToString(sum[:16])
}

const unreadExpression = `MAX(c.marked_unread, (SELECT COUNT(*) FROM messages unread WHERE unread.conversation_id = c.id AND unread.incoming = 1 AND unread.is_read = 0))`

func readMessageState(ctx context.Context, tx *sql.Tx) (MessageState, error) {
	row, err := query.New(tx).ReadMessageState(ctx)
	if err != nil {
		return MessageState{}, fmt.Errorf("read message state: %w", err)
	}
	return MessageState{BackendID: row.BackendID, Revision: row.Revision, UnreadCount: int(row.UnreadCount)}, nil
}

// MessagesState returns authoritative counts, including inactive SIMs.
func (s *Store) MessagesState(ctx context.Context) (MessageState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MessageState{}, fmt.Errorf("begin message state read: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // This transaction only reads a consistent snapshot.
	return readMessageState(ctx, tx)
}

// Conversations returns the latest message and shared read state for each thread.
func (s *Store) Conversations(ctx context.Context, filter ConversationFilter) (ConversationSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConversationSnapshot{}, fmt.Errorf("begin conversation read: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A read-only snapshot does not require a commit.
	state, err := readMessageState(ctx, tx)
	if err != nil {
		return ConversationSnapshot{}, err
	}
	result := ConversationSnapshot{MessageState: state, Items: []Conversation{}}
	query := `
		SELECT c.id, c.profile_id, c.participant,
			COALESCE(l.modem_id, ''), COALESCE(l.label, ''), COALESCE(l.number, ''),
			c.read_through_id, c.marked_unread, ` + unreadExpression + `,
			(SELECT MAX(id) FROM messages last WHERE last.conversation_id = c.id),
			m.id, m.sender, m.recipient, m.text, m.timestamp, m.status, m.incoming, m.wifi_calling
		FROM conversations c
		LEFT JOIN sim_profiles l ON l.profile_id = c.profile_id
		JOIN messages m ON m.id = (
			SELECT id FROM messages last WHERE last.conversation_id = c.id
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		WHERE 1 = 1`
	var args []any
	if filter.ModemID != "" {
		query += ` AND l.modem_id = ?`
		args = append(args, filter.ModemID)
	}
	if filter.ProfileID != "" {
		query += ` AND c.profile_id = ?`
		args = append(args, filter.ProfileID)
	}
	if filter.Unassigned {
		query += ` AND COALESCE(l.modem_id, '') = ''`
	}
	if terms := searchTerms(filter.Query); len(terms) > 0 {
		var clauses []string
		for _, term := range terms {
			pattern := likePattern(term.value)
			clause := `(matched.sender LIKE ? ESCAPE '\' OR matched.recipient LIKE ? ESCAPE '\'`
			args = append(args, pattern, pattern)
			if !term.phoneOnly {
				clause += ` OR matched.text LIKE ? ESCAPE '\'`
				args = append(args, pattern)
			}
			clauses = append(clauses, clause+`)`)
		}
		query += ` AND EXISTS(SELECT 1 FROM messages matched WHERE matched.conversation_id = c.id AND (` + strings.Join(clauses, ` OR `) + `))`
	}
	query += ` ORDER BY m.timestamp DESC, m.id DESC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return result, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Conversation
		var timestamp string
		if err := rows.Scan(
			&c.ID, &c.ProfileID, &c.Participant, &c.ModemID, &c.LineLabel, &c.LineNumber,
			&c.ReadThroughMessageID, &c.MarkedUnread, &c.UnreadCount, &c.LastMessageID,
			&c.Latest.ID, &c.Latest.Sender, &c.Latest.Recipient, &c.Latest.Text, &timestamp,
			&c.Latest.Status, &c.Latest.Incoming, &c.Latest.Routed,
		); err != nil {
			return result, fmt.Errorf("scan conversation: %w", err)
		}
		c.Latest.ConversationID, c.Latest.ProfileID, c.Latest.ModemID = c.ID, c.ProfileID, c.ModemID
		parsed, err := parseTime(timestamp)
		if err != nil {
			return result, fmt.Errorf("parse conversation timestamp: %w", err)
		}
		c.Latest.Timestamp = parsed
		result.Items = append(result.Items, c)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("list conversations: %w", err)
	}
	return result, nil
}

// Conversation finds a thread without consulting modem hardware.
func (s *Store) Conversation(ctx context.Context, id string) (Conversation, error) {
	row, err := query.New(s.db).ReadConversation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("read conversation: %w", err)
	}
	return conversationFromRow(row), nil
}

func conversationFromRow(row query.ReadConversationRow) Conversation {
	return Conversation{
		ID: row.ID, ProfileID: row.ProfileID, Participant: row.Participant,
		ModemID: row.ModemID, LineLabel: row.LineLabel, LineNumber: row.LineNumber,
		ReadThroughMessageID: row.ReadThroughID, MarkedUnread: row.MarkedUnread != 0,
		UnreadCount: int(row.UnreadCount),
	}
}

// UpdateConversationRead never acknowledges messages that arrived after the client's snapshot.
func (s *Store) UpdateConversationRead(ctx context.Context, update ReadUpdate) error {
	if update.ReadThroughMessageID == nil && update.MarkedUnread == nil {
		return ErrInvalidReadUpdate
	}
	if update.MarkedUnread != nil && *update.MarkedUnread && update.ReadThroughMessageID != nil {
		return ErrInvalidReadUpdate
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin read acknowledgment: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback after commit is a no-op.
	queries := query.New(tx)
	state, err := queries.ReadConversationAcknowledgment(ctx, update.ConversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read acknowledgment state: %w", err)
	}
	previous, marked := state.ReadThroughID, state.MarkedUnread != 0
	next, nextMarked := previous, marked
	if update.ReadThroughMessageID != nil {
		valid, err := queries.ValidConversationWatermark(ctx, query.ValidConversationWatermarkParams{ConversationID: update.ConversationID, ID: *update.ReadThroughMessageID})
		if err != nil {
			return fmt.Errorf("validate read watermark: %w", err)
		}
		if !valid {
			return ErrInvalidReadUpdate
		}
		next, nextMarked = max(previous, *update.ReadThroughMessageID), false
	}
	if update.MarkedUnread != nil {
		nextMarked = *update.MarkedUnread
	}
	if update.ReadThroughMessageID != nil {
		err := queries.AcknowledgeConversationMessages(ctx, query.AcknowledgeConversationMessagesParams{
			ConversationID: update.ConversationID, ReadThroughID: *update.ReadThroughMessageID,
		})
		if err != nil {
			return fmt.Errorf("acknowledge message reads: %w", err)
		}
	}
	if next == previous && nextMarked == marked && update.ReadThroughMessageID == nil {
		return nil
	}
	err = queries.AcknowledgeConversation(ctx, query.AcknowledgeConversationParams{
		ID: update.ConversationID, ReadThroughID: next, MarkedUnread: int64(boolInt(nextMarked)),
	})
	if err != nil {
		return fmt.Errorf("acknowledge conversation: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit read acknowledgment: %w", err)
	}
	return nil
}

// DeleteConversation removes stored history without activating its SIM.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin conversation deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed deletion needs no rollback.
	// Keep only the deduplication hashes. An inactive SIM can still contain these
	// messages when it is next enabled; deleting history must not re-import them.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deleted_messages (fingerprint) SELECT fingerprint FROM messages WHERE conversation_id = ? AND fingerprint <> ''`, id); err != nil {
		return fmt.Errorf("remember deleted messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, id); err != nil {
		return fmt.Errorf("delete conversation messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM native_push_outbox WHERE json_extract(content_json, '$.conversationId') = ?`, id); err != nil {
		return fmt.Errorf("delete conversation notifications: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete conversation state: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted conversations: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit conversation deletion: %w", err)
	}
	return nil
}
