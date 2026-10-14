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

	"github.com/damonto/sigmo/internal/pkg/storage/query"
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
	AuthSessionID  string `json:"-"`
}

// NativeSubscriptionUpdate changes selected preferences and renews backend access.
// Nil preferences preserve their stored values; gateway credentials are not editable here.
type NativeSubscriptionUpdate struct {
	SMS           *bool
	Reminders     *bool
	Calls         *bool
	Enabled       *bool
	Label         *string
	AuthSessionID string
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

func advanceMessageState(ctx context.Context, tx *sql.Tx) error {
	if err := query.New(tx).AdvanceMessageState(ctx); err != nil {
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
	err = query.New(tx).EnqueueNativePush(ctx, query.EnqueueNativePushParams{
		EventID:     record.EventID,
		ContentJSON: string(content),
		NextAt:      now.UnixMilli(),
		ExpiresAt:   now.Add(24 * time.Hour).UnixMilli(),
		Kind:        record.Content.Kind,
	})
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
	queries := query.New(tx)
	if err := queries.DeleteReplacedNativeSubscription(ctx, query.DeleteReplacedNativeSubscriptionParams{
		InstallationID: sub.InstallationID, ID: sub.ID,
	}); err != nil {
		return fmt.Errorf("replace push grant: %w", err)
	}
	changed, err := queries.UpsertNativeSubscription(ctx, query.UpsertNativeSubscriptionParams{
		ID:             sub.ID,
		InstallationID: sub.InstallationID,
		Label:          sub.Label,
		BackendName:    sub.BackendName,
		SendToken:      sub.SendToken,
		EncryptionKey:  sub.EncryptionKey,
		AuthSessionID:  sub.AuthSessionID,
		Sms:            sub.SMS,
		Reminders:      sub.Reminders,
		Enabled:        sub.Enabled,
		Calls:          sub.Calls,
	})
	if err != nil {
		return fmt.Errorf("register native push: %w", err)
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

// NativeSubscription reads the current grant before sending a queued invitation.
func (s *Store) NativeSubscription(ctx context.Context, id string) (NativeSubscription, error) {
	row, err := query.New(s.db).NativeSubscription(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeSubscription{}, ErrNotFound
	}
	if err != nil {
		return NativeSubscription{}, fmt.Errorf("read native subscription: %w", err)
	}
	return nativeSubscriptionFromRow(row), nil
}

func nativeSubscriptionFromRow(row query.NativePushSubscription) NativeSubscription {
	return NativeSubscription{
		ID:             row.ID,
		InstallationID: row.InstallationID,
		Label:          row.Label,
		BackendName:    row.BackendName,
		SMS:            row.Sms,
		Reminders:      row.Reminders,
		Calls:          row.Calls,
		Enabled:        row.Enabled,
		SendToken:      row.SendToken,
		EncryptionKey:  row.EncryptionKey,
		AuthSessionID:  row.AuthSessionID,
	}
}

// UpdateNativeSubscription keeps preference changes and their badge in one transaction.
// It cannot restore a deleted grant or overwrite credentials from a concurrent registration.
func (s *Store) UpdateNativeSubscription(ctx context.Context, id string, update NativeSubscriptionUpdate) (NativeSubscription, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NativeSubscription{}, fmt.Errorf("begin push preference update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed update has nothing to roll back.
	queries := query.New(tx)
	row, err := queries.NativeSubscription(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeSubscription{}, ErrNotFound
	}
	if err != nil {
		return NativeSubscription{}, fmt.Errorf("read native subscription: %w", err)
	}
	sub := nativeSubscriptionFromRow(row)
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
	sub.AuthSessionID = update.AuthSessionID
	if sub.SMS == previous.SMS && sub.Reminders == previous.Reminders &&
		sub.Calls == previous.Calls && sub.Enabled == previous.Enabled &&
		sub.Label == previous.Label && sub.AuthSessionID == previous.AuthSessionID {
		return sub, nil
	}
	if err := queries.UpdateNativeSubscription(ctx, query.UpdateNativeSubscriptionParams{
		Sms: sub.SMS, Reminders: sub.Reminders, Calls: sub.Calls, Enabled: sub.Enabled,
		Label: sub.Label, AuthSessionID: sub.AuthSessionID, ID: id,
	}); err != nil {
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
	rows, err := query.New(s.db).NativeSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list native subscriptions: %w", err)
	}
	result := make([]NativeSubscription, 0, len(rows))
	for _, row := range rows {
		result = append(result, nativeSubscriptionFromRow(row))
	}
	return result, nil
}

// DeleteNativeSubscription also cancels its pending deliveries.
func (s *Store) DeleteNativeSubscription(ctx context.Context, id string) error {
	if err := query.New(s.db).DeleteNativeSubscription(ctx, id); err != nil {
		return fmt.Errorf("delete native subscription: %w", err)
	}
	return nil
}

// DeleteNativeSubscriptionIfCurrent revokes only the credential used by an
// in-flight request. A later registration must survive an older HTTP response.
func (s *Store) DeleteNativeSubscriptionIfCurrent(ctx context.Context, sub NativeSubscription) (bool, error) {
	count, err := query.New(s.db).DeleteNativeSubscriptionIfCurrent(ctx, query.DeleteNativeSubscriptionIfCurrentParams{
		ID: sub.ID, InstallationID: sub.InstallationID, SendToken: sub.SendToken, AuthSessionID: sub.AuthSessionID,
	})
	if err != nil {
		return false, fmt.Errorf("revoke current native subscription: %w", err)
	}
	return count != 0, nil
}

// NativeDeliveries returns a bounded batch; the server runs exactly one dispatcher.
func (s *Store) NativeDeliveries(ctx context.Context, now time.Time) ([]NativeDelivery, error) {
	queries := query.New(s.db)
	if err := queries.DeleteExpiredNativeDeliveries(ctx, now.UnixMilli()); err != nil {
		return nil, fmt.Errorf("expire native deliveries: %w", err)
	}
	rows, err := queries.NativeDeliveries(ctx, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read native deliveries: %w", err)
	}
	var result []NativeDelivery
	for _, row := range rows {
		item := NativeDelivery{
			Subscription: nativeSubscriptionFromRow(row.NativePushSubscription),
			EventID:      row.EventID,
			Revision:     row.Revision,
			Attempts:     int(row.Attempts),
			ExpiresAt:    time.UnixMilli(row.ExpiresAt),
		}
		if err := json.Unmarshal([]byte(row.ContentJSON), &item.Content); err != nil {
			return nil, fmt.Errorf("decode native delivery: %w", err)
		}
		result = append(result, item)
	}
	return result, nil
}

// FinishNativeDelivery never removes a newer badge that arrived during an HTTP request.
func (s *Store) FinishNativeDelivery(ctx context.Context, item NativeDelivery) error {
	err := query.New(s.db).FinishNativeDelivery(ctx, query.FinishNativeDeliveryParams{
		SubscriptionID: item.Subscription.ID, EventID: item.EventID, Revision: item.Revision,
	})
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
	queries := query.New(s.db)
	if retryAt.After(now) {
		// A newer badge can arrive before or after this response. Preserve the
		// gateway's wait in either order without finishing the newer revision.
		if err := queries.DeferNativeDelivery(ctx, query.DeferNativeDeliveryParams{
			NextAt: retryAt.UnixMilli(), SubscriptionID: item.Subscription.ID, EventID: item.EventID,
		}); err != nil {
			return fmt.Errorf("defer native delivery: %w", err)
		}
	}
	if retryAt.After(next) {
		next = retryAt
	}
	if !next.Before(item.ExpiresAt) {
		return s.FinishNativeDelivery(ctx, item)
	}
	err := queries.RetryNativeDelivery(ctx, query.RetryNativeDeliveryParams{
		NextAt: next.UnixMilli(), SubscriptionID: item.Subscription.ID, EventID: item.EventID, Revision: item.Revision,
	})
	if err != nil {
		return fmt.Errorf("retry native delivery: %w", err)
	}
	return nil
}
