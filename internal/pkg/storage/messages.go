package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	MessageSourceModem  = "modem"
	MessageSourceRouted = "wifi_calling"
)

// Message retains SIM ownership and modem references alongside the stored SMS.
type Message struct {
	ID             int64
	ConversationID string
	ModemID        string
	ProfileID      string
	Source         string
	ExternalKey    string
	Fingerprint    string
	Sender         string
	Recipient      string
	Text           string
	Timestamp      time.Time
	Status         string
	Incoming       bool
	Routed         bool
	ModemRefs      []ModemMessageRef
}

const messageColumns = `id, profile_id, source, external_key, fingerprint, sender, recipient, text, timestamp, status, incoming, wifi_calling, modem_id, conversation_id`

type ModemMessageRef struct {
	ModemID    string
	Generation uint64
	Storage    uint8
	ID         uint32
}

type MessageStatusUpdate struct {
	ProfileID   string
	Source      string
	ExternalKey string
	Status      string
}

func (s *Store) InsertMessage(ctx context.Context, msg Message) (bool, error) {
	msg = normalizeMessage(msg)
	if err := validateMessage(msg); err != nil {
		return false, err
	}
	now := nowText()
	msg.ConversationID = ConversationID(msg.ProfileID, msg.Counterparty())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin message insert: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful commit.
		_ = tx.Rollback()
	}()
	var deleted bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM deleted_messages WHERE fingerprint = ?)`, msg.Fingerprint).Scan(&deleted); err != nil {
		return false, fmt.Errorf("check deleted message: %w", err)
	}
	if deleted {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO messages (
			profile_id, source, external_key, fingerprint, sender, recipient, text,
			timestamp, status, incoming, wifi_calling, created_at, updated_at, modem_id, conversation_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`, msg.ProfileID, msg.Source, msg.ExternalKey, msg.Fingerprint, msg.Sender, msg.Recipient, msg.Text,
		timeText(msg.Timestamp), msg.Status, boolInt(msg.Incoming), boolInt(msg.Routed), now, now, msg.ModemID, msg.ConversationID)
	if err != nil {
		return false, fmt.Errorf("insert message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read inserted message count: %w", err)
	}
	if affected == 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit existing message: %w", err)
		}
		return false, nil
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return false, fmt.Errorf("read inserted message id: %w", err)
	}
	msg.ID = messageID
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO conversations (id, profile_id, participant) VALUES (?, ?, ?)`, msg.ConversationID, msg.ProfileID, msg.Counterparty()); err != nil {
		return false, fmt.Errorf("create message conversation: %w", err)
	}
	lineNumber := msg.Sender
	if msg.Incoming {
		lineNumber = msg.Recipient
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO message_lines (profile_id, modem_id, number) VALUES (?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			modem_id = CASE WHEN excluded.modem_id <> '' THEN excluded.modem_id ELSE message_lines.modem_id END,
			number = CASE WHEN excluded.number <> '' THEN excluded.number ELSE message_lines.number END
	`, msg.ProfileID, msg.ModemID, lineNumber); err != nil {
		return false, fmt.Errorf("record message line: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return false, err
	}
	if err := enqueueMessagePush(ctx, tx, msg); err != nil {
		return false, err
	}
	for _, ref := range msg.ModemRefs {
		modemID := strings.TrimSpace(ref.ModemID)
		if modemID == "" {
			modemID = msg.ModemID
		}
		if modemID == "" {
			return false, errors.New("modem message ref requires modem id")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO modem_message_refs (message_id, modem_id, generation, storage, ref_id)
			VALUES (?, ?, ?, ?, ?)
		`, messageID, modemID, ref.Generation, ref.Storage, ref.ID); err != nil {
			return false, fmt.Errorf("insert modem message ref: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit message insert: %w", err)
	}
	return true, nil
}

func (s *Store) UpdateMessageStatus(ctx context.Context, update MessageStatusUpdate) (bool, error) {
	update.ProfileID = strings.TrimSpace(update.ProfileID)
	update.Source = strings.TrimSpace(update.Source)
	update.ExternalKey = strings.TrimSpace(update.ExternalKey)
	update.Status = strings.ToLower(strings.TrimSpace(update.Status))
	if update.ProfileID == "" || update.Source == "" || update.ExternalKey == "" || update.Status == "" {
		return false, errors.New("message status update requires profile id, source, external key, and status")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE messages
		SET status = ?, updated_at = ?
		WHERE profile_id = ? AND source = ? AND external_key = ?
	`, update.Status, nowText(), update.ProfileID, update.Source, update.ExternalKey)
	if err != nil {
		return false, fmt.Errorf("update message status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read updated message count: %w", err)
	}
	return affected > 0, nil
}

func (s *Store) ListConversations(ctx context.Context, profileID string, query string) ([]Message, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, nil
	}
	terms := searchTerms(query)
	args := []any{profileID}
	searchSQL := ""
	if len(terms) > 0 {
		clauses := make([]string, 0, len(terms))
		for _, term := range terms {
			pattern := likePattern(term.value)
			if term.phoneOnly {
				clauses = append(clauses, `(sender LIKE ? ESCAPE '\' OR recipient LIKE ? ESCAPE '\')`)
				args = append(args, pattern, pattern)
				continue
			}
			clauses = append(clauses, `(sender LIKE ? ESCAPE '\' OR recipient LIKE ? ESCAPE '\' OR text LIKE ? ESCAPE '\')`)
			args = append(args, pattern, pattern, pattern)
		}
		searchSQL = " AND (" + strings.Join(clauses, " OR ") + ")"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE profile_id = ?`+searchSQL+`
		ORDER BY timestamp DESC, id DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	latest := make(map[string]Message)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		key := msg.Counterparty()
		if key == "" {
			key = msg.Sender + "\x00" + msg.Recipient
		}
		if _, ok := latest[key]; !ok {
			latest[key] = msg
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	messages := slices.Collect(maps.Values(latest))
	slices.SortFunc(messages, func(a, b Message) int {
		if a.Timestamp.Equal(b.Timestamp) {
			return int(b.ID - a.ID)
		}
		return b.Timestamp.Compare(a.Timestamp)
	})
	if err := s.loadModemMessageRefs(ctx, messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *Store) ListByParticipant(ctx context.Context, profileID string, participant string) ([]Message, error) {
	profileID = strings.TrimSpace(profileID)
	participant = strings.TrimSpace(participant)
	if profileID == "" || participant == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE profile_id = ? AND (sender = ? OR recipient = ?)
		ORDER BY timestamp ASC, id ASC
	`, profileID, participant, participant)
	if err != nil {
		return nil, fmt.Errorf("list messages by participant: %w", err)
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if err := s.loadModemMessageRefs(ctx, messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *Store) DeleteByParticipant(ctx context.Context, profileID string, participant string) ([]Message, error) {
	messages, err := s.ListByParticipant(ctx, profileID, participant)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin message delete: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful commit.
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM modem_message_refs
		WHERE message_id IN (
			SELECT id FROM messages WHERE profile_id = ? AND (sender = ? OR recipient = ?)
		)
	`, strings.TrimSpace(profileID), strings.TrimSpace(participant), strings.TrimSpace(participant)); err != nil {
		return nil, fmt.Errorf("delete modem message refs: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM messages
		WHERE profile_id = ? AND (sender = ? OR recipient = ?)
	`, strings.TrimSpace(profileID), strings.TrimSpace(participant), strings.TrimSpace(participant))
	if err != nil {
		return nil, fmt.Errorf("delete messages by participant: %w", err)
	}
	conversationID := ConversationID(profileID, participant)
	if _, err := tx.ExecContext(ctx, `DELETE FROM native_push_outbox WHERE json_extract(content_json, '$.conversationId') = ?`, conversationID); err != nil {
		return nil, fmt.Errorf("delete pending conversation alerts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, conversationID); err != nil {
		return nil, fmt.Errorf("delete conversation state: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit message delete: %w", err)
	}
	return messages, nil
}

func (s *Store) DeleteModemMessageRefs(ctx context.Context, refs []ModemMessageRef) error {
	if len(refs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin modem message ref delete: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful commit.
		_ = tx.Rollback()
	}()
	for _, ref := range refs {
		modemID := strings.TrimSpace(ref.ModemID)
		if modemID == "" {
			return errors.New("modem message ref requires modem id")
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM modem_message_refs
			WHERE modem_id = ? AND generation = ? AND storage = ? AND ref_id = ?
		`, modemID, ref.Generation, ref.Storage, ref.ID); err != nil {
			return fmt.Errorf("delete modem message ref: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit modem message ref delete: %w", err)
	}
	return nil
}

func (s *Store) loadModemMessageRefs(ctx context.Context, messages []Message) error {
	for i := range messages {
		rows, err := s.db.QueryContext(ctx, `
			SELECT modem_id, generation, storage, ref_id
			FROM modem_message_refs
			WHERE message_id = ?
			ORDER BY generation, storage, ref_id
		`, messages[i].ID)
		if err != nil {
			return fmt.Errorf("list modem message refs: %w", err)
		}
		for rows.Next() {
			var ref ModemMessageRef
			if err := rows.Scan(&ref.ModemID, &ref.Generation, &ref.Storage, &ref.ID); err != nil {
				rows.Close()
				return fmt.Errorf("scan modem message ref: %w", err)
			}
			messages[i].ModemRefs = append(messages[i].ModemRefs, ref)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("list modem message refs: %w", err)
		}
	}
	return nil
}

func normalizeMessage(msg Message) Message {
	msg.ProfileID = strings.TrimSpace(msg.ProfileID)
	msg.ModemID = strings.TrimSpace(msg.ModemID)
	msg.Source = strings.TrimSpace(msg.Source)
	msg.ExternalKey = strings.TrimSpace(msg.ExternalKey)
	msg.Fingerprint = strings.TrimSpace(msg.Fingerprint)
	msg.Sender = strings.TrimSpace(msg.Sender)
	msg.Recipient = strings.TrimSpace(msg.Recipient)
	msg.Status = strings.ToLower(strings.TrimSpace(msg.Status))
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Fingerprint == "" {
		msg.Fingerprint = MessageFingerprint(msg)
	}
	return msg
}

func validateMessage(msg Message) error {
	if msg.ProfileID == "" {
		return errors.New("profile id is required")
	}
	if msg.Source == "" {
		return errors.New("message source is required")
	}
	if msg.ExternalKey == "" {
		return errors.New("message external key is required")
	}
	return nil
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan messages: %w", err)
	}
	return messages, nil
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row messageScanner) (Message, error) {
	var msg Message
	var timestamp string
	var incoming, routed int
	var err error
	if err := row.Scan(
		&msg.ID,
		&msg.ProfileID,
		&msg.Source,
		&msg.ExternalKey,
		&msg.Fingerprint,
		&msg.Sender,
		&msg.Recipient,
		&msg.Text,
		&timestamp,
		&msg.Status,
		&incoming,
		&routed,
		&msg.ModemID,
		&msg.ConversationID,
	); err != nil {
		return Message{}, fmt.Errorf("scan message: %w", err)
	}
	if msg.Timestamp, err = parseTime(timestamp); err != nil {
		return Message{}, fmt.Errorf("parse message timestamp: %w", err)
	}
	msg.Incoming = incoming != 0
	msg.Routed = routed != 0
	return msg, nil
}

func MessageFingerprint(msg Message) string {
	direction := "out"
	counterparty := msg.Recipient
	if msg.Incoming {
		direction = "in"
		counterparty = msg.Sender
	}
	parts := []string{
		strings.TrimSpace(msg.ModemID),
		strings.TrimSpace(msg.Source),
		direction,
		strings.TrimSpace(counterparty),
		msg.Text,
		timeText(msg.Timestamp),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (m Message) Counterparty() string {
	if m.Incoming {
		return strings.TrimSpace(m.Sender)
	}
	return strings.TrimSpace(m.Recipient)
}

func timeText(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return t, nil
	}
	return time.Time{}, err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
