package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	MessageSourceModem       = "modem"
	MessageSourceWiFiCalling = "wifi_calling"
)

type Message struct {
	ID          int64
	ProfileID   string
	Source      string
	ExternalKey string
	Sender      string
	Recipient   string
	Text        string
	Timestamp   time.Time
	Status      string
	Incoming    bool
	WiFiCalling bool
}

func (s *Store) InsertMessage(ctx context.Context, msg Message) (bool, error) {
	msg = normalizeMessage(msg)
	if err := validateMessage(msg); err != nil {
		return false, err
	}
	now := nowText()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (
			profile_id, source, external_key, sender, recipient, text,
			timestamp, status, incoming, wifi_calling, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, source, external_key) DO NOTHING
	`, msg.ProfileID, msg.Source, msg.ExternalKey, msg.Sender, msg.Recipient, msg.Text,
		timeText(msg.Timestamp), msg.Status, boolInt(msg.Incoming), boolInt(msg.WiFiCalling), now, now)
	if err != nil {
		return false, fmt.Errorf("insert message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read inserted message count: %w", err)
	}
	return affected > 0, nil
}

func (s *Store) ListConversations(ctx context.Context, profileID string) ([]Message, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, profile_id, source, external_key, sender, recipient, text, timestamp, status, incoming, wifi_calling
		FROM messages
		WHERE profile_id = ?
		ORDER BY timestamp DESC, id DESC
	`, profileID)
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
	messages := make([]Message, 0, len(latest))
	for _, msg := range latest {
		messages = append(messages, msg)
	}
	slices.SortFunc(messages, func(a, b Message) int {
		if a.Timestamp.Equal(b.Timestamp) {
			return int(b.ID - a.ID)
		}
		return b.Timestamp.Compare(a.Timestamp)
	})
	return messages, nil
}

func (s *Store) ListByParticipant(ctx context.Context, profileID string, participant string) ([]Message, error) {
	profileID = strings.TrimSpace(profileID)
	participant = strings.TrimSpace(participant)
	if profileID == "" || participant == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, profile_id, source, external_key, sender, recipient, text, timestamp, status, incoming, wifi_calling
		FROM messages
		WHERE profile_id = ? AND (sender = ? OR recipient = ?)
		ORDER BY timestamp ASC, id ASC
	`, profileID, participant, participant)
	if err != nil {
		return nil, fmt.Errorf("list messages by participant: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) DeleteByParticipant(ctx context.Context, profileID string, participant string) ([]Message, error) {
	messages, err := s.ListByParticipant(ctx, profileID, participant)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM messages
		WHERE profile_id = ? AND (sender = ? OR recipient = ?)
	`, strings.TrimSpace(profileID), strings.TrimSpace(participant), strings.TrimSpace(participant))
	if err != nil {
		return nil, fmt.Errorf("delete messages by participant: %w", err)
	}
	return messages, nil
}

func normalizeMessage(msg Message) Message {
	msg.ProfileID = strings.TrimSpace(msg.ProfileID)
	msg.Source = strings.TrimSpace(msg.Source)
	msg.ExternalKey = strings.TrimSpace(msg.ExternalKey)
	msg.Sender = strings.TrimSpace(msg.Sender)
	msg.Recipient = strings.TrimSpace(msg.Recipient)
	msg.Status = strings.ToLower(strings.TrimSpace(msg.Status))
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
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
	var incoming, wifiCalling int
	if err := row.Scan(
		&msg.ID,
		&msg.ProfileID,
		&msg.Source,
		&msg.ExternalKey,
		&msg.Sender,
		&msg.Recipient,
		&msg.Text,
		&timestamp,
		&msg.Status,
		&incoming,
		&wifiCalling,
	); err != nil {
		return Message{}, fmt.Errorf("scan message: %w", err)
	}
	msg.Timestamp = parseTime(timestamp)
	msg.Incoming = incoming != 0
	msg.WiFiCalling = wifiCalling != 0
	return msg, nil
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

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return t
	}
	return time.Time{}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
