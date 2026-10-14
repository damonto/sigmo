package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// EnqueueNativeCall retains one short-lived invitation per installation and call.
func (s *Store) EnqueueNativeCall(ctx context.Context, call Call) error {
	if call.Direction != "incoming" || call.State != "ringing" {
		err := query.New(s.db).CancelNativeCall(ctx, call.ID)
		if err != nil {
			return fmt.Errorf("cancel call invitation: %w", err)
		}
		return nil
	}
	expires := call.StartedAt.Add(45 * time.Second)
	if time.Now().After(expires) {
		return nil
	}
	err := query.New(s.db).EnqueueNativeCall(ctx, query.EnqueueNativeCallParams{
		CallID: call.ID, NextAt: time.Now().UnixMilli(), ExpiresAt: expires.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("enqueue incoming call: %w", err)
	}
	return nil
}

// NativeCallDeliveries reads a separate queue so SMS retries never delay ringing.
func (s *Store) NativeCallDeliveries(ctx context.Context, now time.Time) ([]NativeDelivery, error) {
	queries := query.New(s.db)
	if err := queries.DeleteExpiredNativeCalls(ctx, now.UnixMilli()); err != nil {
		return nil, fmt.Errorf("expire call invitations: %w", err)
	}
	rows, err := queries.NativeCallDeliveries(ctx, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read call invitations: %w", err)
	}
	var result []NativeDelivery
	for _, row := range rows {
		content := PushContent{
			Kind:      "call",
			CallID:    row.CallID,
			ModemID:   row.ModemID,
			ProfileID: row.ProfileID,
			Sender:    row.Number,
			BackendID: row.BackendID,
			CallUUID:  CallUUID(row.BackendID, row.ModemID, row.CallID),
			ExpiresAt: row.ExpiresAt / 1000,
		}
		item := NativeDelivery{
			Subscription: nativeSubscriptionFromRow(row.NativePushSubscription),
			Content:      content,
			EventID:      "call:" + content.CallUUID,
			ExpiresAt:    time.UnixMilli(row.ExpiresAt),
		}
		result = append(result, item)
	}
	return result, nil
}

// FinishNativeCall retains a completed marker until expiry to deduplicate
// ringing events. A zero retryAt finishes the event; waits never extend its life.
func (s *Store) FinishNativeCall(ctx context.Context, item NativeDelivery, retryAt time.Time) error {
	retry := !retryAt.IsZero() && retryAt.Before(item.ExpiresAt)
	err := query.New(s.db).FinishNativeCall(ctx, query.FinishNativeCallParams{
		Delivered: !retry, NextAt: retryAt.UnixMilli(), SubscriptionID: item.Subscription.ID, CallID: item.Content.CallID,
	})
	if err != nil {
		return fmt.Errorf("finish call invitation: %w", err)
	}
	return nil
}
