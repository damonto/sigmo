package storage

import (
	"context"
	"fmt"
	"time"
)

// EnqueueNativeCall retains one short-lived invitation per installation and call.
func (s *Store) EnqueueNativeCall(ctx context.Context, call Call) error {
	if call.Direction != "incoming" || call.State != "ringing" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM native_call_outbox WHERE call_id = ? AND delivered = 0`, call.ID)
		if err != nil {
			return fmt.Errorf("cancel call invitation: %w", err)
		}
		return nil
	}
	expires := call.StartedAt.Add(45 * time.Second)
	if time.Now().After(expires) {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO native_call_outbox (subscription_id, call_id, next_at, expires_at) SELECT id, ?, ?, ? FROM native_push_subscriptions WHERE enabled = 1 AND calls = 1`, call.ID, time.Now().UnixMilli(), expires.UnixMilli())
	if err != nil {
		return fmt.Errorf("enqueue incoming call: %w", err)
	}
	return nil
}

// NativeCallDeliveries reads a separate queue so SMS retries never delay ringing.
func (s *Store) NativeCallDeliveries(ctx context.Context, now time.Time) ([]NativeDelivery, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM native_call_outbox WHERE expires_at <= ?`, now.UnixMilli()); err != nil {
		return nil, fmt.Errorf("expire call invitations: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.installation_id, s.label, s.backend_name, s.sms, s.reminders,
			s.enabled, s.send_token, s.encryption_key, s.auth_token_hash, s.calls,
			c.id, c.modem_id, c.profile_id, c.number, o.expires_at,
			(SELECT backend_id FROM message_state WHERE id = 1)
		FROM native_call_outbox o
		JOIN native_push_subscriptions s ON s.id = o.subscription_id
		JOIN calls c ON c.id = o.call_id
		WHERE o.delivered = 0 AND o.next_at <= ? AND s.enabled = 1 AND s.calls = 1
		AND c.state = 'ringing' AND c.direction = 'incoming' AND c.owner_client_id = ''
		ORDER BY o.next_at LIMIT 20
	`, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read call invitations: %w", err)
	}
	// Read each invitation and its grant from the same snapshot. Besides avoiding
	// per-call queries, this excludes calls or subscriptions deleted before the read.
	defer func() { _ = rows.Close() }() // Rows.Err reports iteration errors; preserve earlier scan errors.
	var result []NativeDelivery
	for rows.Next() {
		var item NativeDelivery
		var expires int64
		sub := &item.Subscription
		content := &item.Content
		if err := rows.Scan(
			&sub.ID, &sub.InstallationID, &sub.Label, &sub.BackendName, &sub.SMS, &sub.Reminders,
			&sub.Enabled, &sub.SendToken, &sub.EncryptionKey, &sub.AuthTokenHash, &sub.Calls,
			&content.CallID, &content.ModemID, &content.ProfileID, &content.Sender, &expires,
			&content.BackendID,
		); err != nil {
			return nil, fmt.Errorf("scan call invitation: %w", err)
		}
		content.Kind = "call"
		content.CallUUID = CallUUID(content.BackendID, content.ModemID, content.CallID)
		content.ExpiresAt = expires / 1000
		item.EventID = "call:" + content.CallUUID
		item.ExpiresAt = time.UnixMilli(expires)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read call invitations: %w", err)
	}
	return result, nil
}

// FinishNativeCall retains a completed marker until expiry to deduplicate
// ringing events. A zero retryAt finishes the event; waits never extend its life.
func (s *Store) FinishNativeCall(ctx context.Context, item NativeDelivery, retryAt time.Time) error {
	retry := !retryAt.IsZero() && retryAt.Before(item.ExpiresAt)
	_, err := s.db.ExecContext(ctx, `UPDATE native_call_outbox SET delivered = ?, next_at = ? WHERE subscription_id = ? AND call_id = ?`, !retry, retryAt.UnixMilli(), item.Subscription.ID, item.Content.CallID)
	if err != nil {
		return fmt.Errorf("finish call invitation: %w", err)
	}
	return nil
}
