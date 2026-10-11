package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// NativeSubscription contains only one installation's delegated gateway credentials.
type NativeSubscription struct {
	ID             string `json:"id"`
	InstallationID string `json:"installationId"`
	Label          string `json:"label"`
	BackendName    string `json:"backendName"`
	SMS            bool   `json:"sms"`
	Reminders      bool   `json:"reminders"`
	Calls          bool   `json:"calls"`
	Enabled        bool   `json:"enabled"`
	SendToken      string `json:"-"`
	EncryptionKey  []byte `json:"-"`
	AuthTokenHash  string `json:"-"`
}

// NativeSubscriptionUpdate changes selected preferences and renews backend access.
// Nil preferences preserve their stored values; gateway credentials are not editable here.
type NativeSubscriptionUpdate struct {
	SMS           *bool
	Reminders     *bool
	Calls         *bool
	Enabled       *bool
	Label         *string
	AuthTokenHash string
}

// PushContent is encrypted before it leaves the owning Sigmo backend.
type PushContent struct {
	CallID         string `json:"callId,omitempty"`
	CallUUID       string `json:"callUUID,omitempty"`
	ExpiresAt      int64  `json:"expiresAt,omitempty"`
	Kind           string `json:"kind"`
	BackendID      string `json:"backendId"`
	BackendName    string `json:"backendName"`
	ConversationID string `json:"conversationId,omitempty"`
	MessageID      int64  `json:"messageId,omitempty"`
	ModemID        string `json:"modemId,omitempty"`
	ModemName      string `json:"modemName,omitempty"`
	ProfileID      string `json:"profileId,omitempty"`
	ProfileType    string `json:"profileType,omitempty"`
	ProfileName    string `json:"profileName,omitempty"`
	SEID           string `json:"seId,omitempty"`
	Sender         string `json:"sender,omitempty"`
	Text           string `json:"text,omitempty"`
}

// NativeDelivery is one durable attempt; Revision protects a newer coalesced badge update.
type NativeDelivery struct {
	Subscription NativeSubscription
	EventID      string
	Revision     int64
	Content      PushContent
	Attempts     int
	ExpiresAt    time.Time
}

func migrateNativePush(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS native_push_subscriptions (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL,
			backend_name TEXT NOT NULL,
			send_token TEXT NOT NULL,
			encryption_key BLOB NOT NULL,
			auth_token_hash TEXT NOT NULL,
			sms INTEGER NOT NULL,
			reminders INTEGER NOT NULL,
			enabled INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS native_push_outbox (
			subscription_id TEXT NOT NULL REFERENCES native_push_subscriptions(id) ON DELETE CASCADE,
			event_id TEXT NOT NULL,
			revision INTEGER NOT NULL,
			content_json TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			PRIMARY KEY(subscription_id, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_native_push_due ON native_push_outbox(next_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create native push schema: %w", err)
		}
	}
	return nil
}

func advanceMessageState(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE message_state SET revision = revision + 1 WHERE id = 1`); err != nil {
		return fmt.Errorf("advance message state: %w", err)
	}
	return enqueueNativePush(ctx, tx, pushRecord{EventID: "badge", Content: PushContent{Kind: "badge"}})
}

type pushRecord struct {
	EventID string
	Content PushContent
}

func enqueueNativePush(ctx context.Context, tx *sql.Tx, record pushRecord) error {
	content, err := json.Marshal(record.Content)
	if err != nil {
		return fmt.Errorf("encode notification record: %w", err)
	}
	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO native_push_outbox (subscription_id, event_id, revision, content_json, next_at, expires_at)
		SELECT id, ?, (SELECT revision FROM message_state WHERE id = 1), ?, ?, ?
		FROM native_push_subscriptions
		WHERE ? = 'badge' OR (enabled = 1 AND ((? = 'sms' AND sms = 1) OR (? = 'reminder' AND reminders = 1)))
		ON CONFLICT(subscription_id, event_id) DO UPDATE SET
			revision = excluded.revision,
			content_json = excluded.content_json,
			next_at = MAX(native_push_outbox.next_at, excluded.next_at),
			expires_at = excluded.expires_at,
			attempts = 0
		WHERE excluded.event_id = 'badge'
	`, record.EventID, string(content), now.UnixMilli(), now.Add(24*time.Hour).UnixMilli(),
		record.Content.Kind, record.Content.Kind, record.Content.Kind)
	if err != nil {
		return fmt.Errorf("enqueue native push: %w", err)
	}
	return nil
}

func enqueueMessagePush(ctx context.Context, tx *sql.Tx, message Message) error {
	if !message.Incoming || time.Since(message.Timestamp).Abs() > 30*time.Minute {
		return nil
	}
	return enqueueNativePush(ctx, tx, pushRecord{
		EventID: "sms:" + strconv.FormatInt(message.ID, 10),
		Content: PushContent{
			Kind:           "sms",
			ConversationID: message.ConversationID,
			MessageID:      message.ID,
			ModemID:        message.ModemID,
			ProfileID:      message.ProfileID,
			Sender:         message.Sender,
			Text:           message.Text,
		},
	})
}

func enqueueReminderPush(ctx context.Context, tx *sql.Tx, reminder Reminder) error {
	sum := sha256.Sum256([]byte(reminder.ProfileType + "\x00" + reminder.ProfileID + "\x00" + reminderTimeText(reminder.NextAt)))
	return enqueueNativePush(ctx, tx, pushRecord{
		EventID: "reminder:" + hex.EncodeToString(sum[:16]),
		Content: PushContent{
			Kind:        "reminder",
			ModemID:     reminder.ModemID,
			ProfileID:   reminder.ProfileID,
			ProfileType: reminder.ProfileType,
			ProfileName: reminder.ProfileName,
			SEID:        reminder.SEID,
			Text:        reminder.Content,
		},
	})
}

// UpsertNativeSubscription replaces only this installation's subscription and queues its badge.
func (s *Store) UpsertNativeSubscription(ctx context.Context, sub NativeSubscription) error {
	if sub.ID == "" || sub.InstallationID == "" || len(sub.EncryptionKey) != 32 || len(sub.SendToken) < 32 {
		return errors.New("invalid native push subscription")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin push registration: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback after commit is harmless.
	if _, err := tx.ExecContext(ctx, `DELETE FROM native_push_subscriptions WHERE installation_id = ? AND id <> ?`, sub.InstallationID, sub.ID); err != nil {
		return fmt.Errorf("replace push grant: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO native_push_subscriptions (
			id, installation_id, label, backend_name, send_token, encryption_key,
			auth_token_hash, sms, reminders, enabled, calls
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			installation_id = excluded.installation_id,
			label = excluded.label,
			backend_name = excluded.backend_name,
			send_token = excluded.send_token,
			encryption_key = excluded.encryption_key,
			auth_token_hash = excluded.auth_token_hash,
			sms = excluded.sms,
			reminders = excluded.reminders,
			enabled = excluded.enabled,
			calls = excluded.calls
		WHERE installation_id <> excluded.installation_id OR label <> excluded.label
			OR backend_name <> excluded.backend_name OR send_token <> excluded.send_token
			OR encryption_key <> excluded.encryption_key OR auth_token_hash <> excluded.auth_token_hash
			OR sms <> excluded.sms OR reminders <> excluded.reminders
			OR enabled <> excluded.enabled OR calls <> excluded.calls
	`, sub.ID, sub.InstallationID, sub.Label, sub.BackendName, sub.SendToken, sub.EncryptionKey,
		sub.AuthTokenHash, sub.SMS, sub.Reminders, sub.Enabled, sub.Calls)
	if err != nil {
		return fmt.Errorf("register native push: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated subscriptions: %w", err)
	}
	if changed == 0 {
		return nil
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit push registration: %w", err)
	}
	return nil
}

const nativeSubscriptionColumns = `id, installation_id, label, backend_name, sms, reminders,
	enabled, send_token, encryption_key, auth_token_hash, calls`

// NativeSubscription reads the current grant before sending a queued invitation.
func (s *Store) NativeSubscription(ctx context.Context, id string) (NativeSubscription, error) {
	return scanNativeSubscription(s.db.QueryRowContext(ctx,
		`SELECT `+nativeSubscriptionColumns+` FROM native_push_subscriptions WHERE id = ?`, id))
}

type nativeSubscriptionScanner interface {
	Scan(...any) error
}

func scanNativeSubscription(row nativeSubscriptionScanner) (NativeSubscription, error) {
	var sub NativeSubscription
	err := row.Scan(
		&sub.ID, &sub.InstallationID, &sub.Label, &sub.BackendName, &sub.SMS, &sub.Reminders, &sub.Enabled,
		&sub.SendToken, &sub.EncryptionKey, &sub.AuthTokenHash, &sub.Calls)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeSubscription{}, ErrNotFound
	}
	if err != nil {
		return NativeSubscription{}, fmt.Errorf("read native subscription: %w", err)
	}
	return sub, nil
}

// UpdateNativeSubscription keeps preference changes and their badge in one transaction.
// It cannot restore a deleted grant or overwrite credentials from a concurrent registration.
func (s *Store) UpdateNativeSubscription(ctx context.Context, id string, update NativeSubscriptionUpdate) (NativeSubscription, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NativeSubscription{}, fmt.Errorf("begin push preference update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed update has nothing to roll back.
	sub, err := scanNativeSubscription(tx.QueryRowContext(ctx,
		`SELECT `+nativeSubscriptionColumns+` FROM native_push_subscriptions WHERE id = ?`, id))
	if err != nil {
		return NativeSubscription{}, err
	}
	previous := sub
	if update.SMS != nil {
		sub.SMS = *update.SMS
	}
	if update.Reminders != nil {
		sub.Reminders = *update.Reminders
	}
	if update.Calls != nil {
		sub.Calls = *update.Calls
	}
	if update.Enabled != nil {
		sub.Enabled = *update.Enabled
	}
	if update.Label != nil {
		sub.Label = *update.Label
	}
	sub.AuthTokenHash = update.AuthTokenHash
	if sub.SMS == previous.SMS && sub.Reminders == previous.Reminders &&
		sub.Calls == previous.Calls && sub.Enabled == previous.Enabled &&
		sub.Label == previous.Label && sub.AuthTokenHash == previous.AuthTokenHash {
		return sub, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE native_push_subscriptions
		SET sms = ?, reminders = ?, calls = ?, enabled = ?, label = ?, auth_token_hash = ?
		WHERE id = ?
	`, sub.SMS, sub.Reminders, sub.Calls, sub.Enabled, sub.Label, sub.AuthTokenHash, id); err != nil {
		return NativeSubscription{}, fmt.Errorf("update push preferences: %w", err)
	}
	if err := advanceMessageState(ctx, tx); err != nil {
		return NativeSubscription{}, err
	}
	if err := tx.Commit(); err != nil {
		return NativeSubscription{}, fmt.Errorf("commit push preference update: %w", err)
	}
	return sub, nil
}

// NativeSubscriptions lists registrations; its JSON representation excludes secrets.
func (s *Store) NativeSubscriptions(ctx context.Context) ([]NativeSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nativeSubscriptionColumns+` FROM native_push_subscriptions ORDER BY label, id`)
	if err != nil {
		return nil, fmt.Errorf("list native subscriptions: %w", err)
	}
	defer rows.Close()
	result := []NativeSubscription{}
	for rows.Next() {
		sub, err := scanNativeSubscription(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list native subscriptions: %w", err)
	}
	return result, nil
}

// DeleteNativeSubscription also cancels its pending deliveries.
func (s *Store) DeleteNativeSubscription(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM native_push_subscriptions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete native subscription: %w", err)
	}
	return nil
}

// DeleteNativeSubscriptionIfCurrent revokes only the credential used by an
// in-flight request. A later registration must survive an older HTTP response.
func (s *Store) DeleteNativeSubscriptionIfCurrent(ctx context.Context, sub NativeSubscription) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM native_push_subscriptions
		WHERE id = ? AND installation_id = ? AND send_token = ? AND auth_token_hash = ?
	`, sub.ID, sub.InstallationID, sub.SendToken, sub.AuthTokenHash)
	if err != nil {
		return false, fmt.Errorf("revoke current native subscription: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count revoked native subscriptions: %w", err)
	}
	return count != 0, nil
}

// NativeDeliveries returns a bounded batch; the server runs exactly one dispatcher.
func (s *Store) NativeDeliveries(ctx context.Context, now time.Time) ([]NativeDelivery, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM native_push_outbox WHERE expires_at <= ?`, now.UnixMilli()); err != nil {
		return nil, fmt.Errorf("expire native deliveries: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.installation_id, s.label, s.backend_name, s.sms, s.reminders,
			s.enabled, s.send_token, s.encryption_key, s.auth_token_hash, s.calls,
			o.event_id, o.revision, o.content_json, o.attempts, o.expires_at
		FROM native_push_outbox o
		JOIN native_push_subscriptions s ON s.id = o.subscription_id
		WHERE o.next_at <= ?
		ORDER BY o.next_at, o.event_id LIMIT 20
	`, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read native deliveries: %w", err)
	}
	defer rows.Close()
	var result []NativeDelivery
	for rows.Next() {
		var item NativeDelivery
		var raw string
		var expires int64
		sub := &item.Subscription
		if err := rows.Scan(
			&sub.ID, &sub.InstallationID, &sub.Label, &sub.BackendName, &sub.SMS, &sub.Reminders,
			&sub.Enabled, &sub.SendToken, &sub.EncryptionKey, &sub.AuthTokenHash, &sub.Calls,
			&item.EventID, &item.Revision, &raw, &item.Attempts, &expires,
		); err != nil {
			return nil, fmt.Errorf("scan native delivery: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &item.Content); err != nil {
			return nil, fmt.Errorf("decode native delivery: %w", err)
		}
		item.ExpiresAt = time.UnixMilli(expires)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read native deliveries: %w", err)
	}
	return result, nil
}

// FinishNativeDelivery never removes a newer badge that arrived during an HTTP request.
func (s *Store) FinishNativeDelivery(ctx context.Context, item NativeDelivery) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM native_push_outbox WHERE subscription_id = ? AND event_id = ? AND revision = ?`, item.Subscription.ID, item.EventID, item.Revision)
	if err != nil {
		return fmt.Errorf("finish native delivery: %w", err)
	}
	return nil
}

// RetryNativeDelivery respects both exponential backoff and the gateway's
// minimum retry time. Events that would expire while waiting are finished.
func (s *Store) RetryNativeDelivery(ctx context.Context, item NativeDelivery, retryAt time.Time) error {
	delay := time.Duration(1<<min(item.Attempts+1, 7)) * time.Second
	now := time.Now()
	next := now.Add(delay)
	if retryAt.After(now) {
		// A newer badge can arrive before or after this response. Preserve the
		// gateway's wait in either order without finishing the newer revision.
		if _, err := s.db.ExecContext(ctx, `UPDATE native_push_outbox SET next_at = MAX(next_at, ?) WHERE subscription_id = ? AND event_id = ?`, retryAt.UnixMilli(), item.Subscription.ID, item.EventID); err != nil {
			return fmt.Errorf("defer native delivery: %w", err)
		}
	}
	if retryAt.After(next) {
		next = retryAt
	}
	if !next.Before(item.ExpiresAt) {
		return s.FinishNativeDelivery(ctx, item)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE native_push_outbox SET attempts = attempts + 1, next_at = MAX(next_at, ?) WHERE subscription_id = ? AND event_id = ? AND revision = ?`, next.UnixMilli(), item.Subscription.ID, item.EventID, item.Revision)
	if err != nil {
		return fmt.Errorf("retry native delivery: %w", err)
	}
	return nil
}
