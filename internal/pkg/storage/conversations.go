package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

func (s *Store) migrateConversations(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin conversation migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing to roll back.
	if err := migrateSIMProfiles(ctx, tx); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS message_state (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			backend_id TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			participant TEXT NOT NULL,
			read_through_id INTEGER NOT NULL DEFAULT 0,
			marked_unread INTEGER NOT NULL DEFAULT 0,
			UNIQUE(profile_id, participant)
		)`,
		`CREATE TABLE IF NOT EXISTS deleted_messages (fingerprint TEXT PRIMARY KEY)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create conversation schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO message_state (id, backend_id) VALUES (1, ?)`, rand.Text()); err != nil {
		return fmt.Errorf("create backend identity: %w", err)
	}
	var migrated bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('messages') WHERE name = 'conversation_id')`).Scan(&migrated); err != nil {
		return fmt.Errorf("inspect message schema: %w", err)
	}
	if !migrated {
		for _, statement := range []string{
			`ALTER TABLE messages ADD COLUMN conversation_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE messages ADD COLUMN modem_id TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("extend message schema: %w", err)
			}
		}
		history, err := readHistoricalConversations(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range history {
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversations (id, profile_id, participant, read_through_id) VALUES (?, ?, ?, ?)`, c.ID, c.ProfileID, c.Participant, c.ReadThroughMessageID); err != nil {
				return fmt.Errorf("backfill conversation: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE messages
				SET conversation_id = ?, modem_id = COALESCE((
					SELECT CASE WHEN COUNT(DISTINCT modem_id) = 1 THEN MAX(modem_id) ELSE '' END
					FROM modem_message_refs WHERE message_id = messages.id
				), '')
				WHERE profile_id = ? AND CASE WHEN incoming = 1 THEN sender ELSE recipient END = ?
			`, c.ID, c.ProfileID, c.Participant); err != nil {
				return fmt.Errorf("backfill message identity: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO sim_profiles (profile_id, modem_id)
			SELECT profile_id, CASE WHEN COUNT(DISTINCT NULLIF(modem_id, '')) = 1 THEN MAX(modem_id) ELSE '' END
			FROM messages GROUP BY profile_id
		`); err != nil {
			return fmt.Errorf("backfill SIM profiles: %w", err)
		}
		if err := backfillSIMProfileNumbers(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sim_profiles (profile_id, modem_id, label)
		SELECT profile_id, modem_id, profile_name FROM reminders WHERE modem_id <> ''
		ON CONFLICT(profile_id) DO UPDATE SET modem_id = excluded.modem_id, label = excluded.label WHERE sim_profiles.modem_id = ''`); err != nil {
		return fmt.Errorf("backfill reminder line ownership: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, id)`); err != nil {
		return fmt.Errorf("index conversations: %w", err)
	}
	if err := migrateNativePush(ctx, tx); err != nil {
		return err
	}
	if err := migrateNativeCalls(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit conversation migration: %w", err)
	}
	return nil
}

// readHistoricalConversations closes the cursor before migration writes reuse
// the transaction's connection. Existing messages establish the read watermark.
func readHistoricalConversations(ctx context.Context, tx *sql.Tx) ([]Conversation, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT profile_id, CASE WHEN incoming = 1 THEN sender ELSE recipient END, MAX(id)
		FROM messages
		GROUP BY profile_id, CASE WHEN incoming = 1 THEN sender ELSE recipient END
	`)
	if err != nil {
		return nil, fmt.Errorf("read historical conversations: %w", err)
	}
	defer func() { _ = rows.Close() }() // Rows.Err reports iteration errors; preserve earlier scan errors.
	var history []Conversation
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ProfileID, &conversation.Participant, &conversation.ReadThroughMessageID); err != nil {
			return nil, fmt.Errorf("scan historical conversation: %w", err)
		}
		conversation.ID = ConversationID(conversation.ProfileID, conversation.Participant)
		history = append(history, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read historical conversations: %w", err)
	}
	return history, nil
}

const unreadExpression = `MAX(c.marked_unread, (SELECT COUNT(*) FROM messages unread WHERE unread.conversation_id = c.id AND unread.incoming = 1 AND unread.id > c.read_through_id))`

func readMessageState(ctx context.Context, tx *sql.Tx) (MessageState, error) {
	var state MessageState
	err := tx.QueryRowContext(ctx, `
		SELECT backend_id, revision, COALESCE((SELECT SUM(`+unreadExpression+`) FROM conversations c), 0)
		FROM message_state WHERE id = 1
	`).Scan(&state.BackendID, &state.Revision, &state.UnreadCount)
	if err != nil {
		return state, fmt.Errorf("read message state: %w", err)
	}
	return state, nil
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
	var c Conversation
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.profile_id, c.participant,
			COALESCE(l.modem_id, ''), COALESCE(l.label, ''), COALESCE(l.number, ''),
			c.read_through_id, c.marked_unread, `+unreadExpression+`
		FROM conversations c
		LEFT JOIN sim_profiles l ON l.profile_id = c.profile_id
		WHERE c.id = ?
	`, id).Scan(
		&c.ID, &c.ProfileID, &c.Participant, &c.ModemID, &c.LineLabel, &c.LineNumber,
		&c.ReadThroughMessageID, &c.MarkedUnread, &c.UnreadCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("read conversation: %w", err)
	}
	return c, nil
}

// ConversationMessages also works for an inactive or removed SIM.
func (s *Store) ConversationMessages(ctx context.Context, id string) ([]Message, error) {
	if _, err := s.Conversation(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE conversation_id = ? ORDER BY timestamp ASC, id ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("read conversation messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
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
	var previous int64
	var marked bool
	err = tx.QueryRowContext(ctx, `SELECT read_through_id, marked_unread FROM conversations WHERE id = ?`, update.ConversationID).Scan(&previous, &marked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read acknowledgment state: %w", err)
	}
	next, nextMarked := previous, marked
	if update.ReadThroughMessageID != nil {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id = ? AND id = ?)`, update.ConversationID, *update.ReadThroughMessageID).Scan(&valid); err != nil {
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
	if next == previous && nextMarked == marked {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET read_through_id = ?, marked_unread = ? WHERE id = ?`, next, nextMarked, update.ConversationID); err != nil {
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
