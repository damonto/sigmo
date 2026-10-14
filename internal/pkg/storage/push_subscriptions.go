package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// PushSubscription retains the browser endpoint and keys needed to deliver notifications.
type PushSubscription struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	P256DH    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	Label     string    `json:"label"`
	UserAgent string    `json:"userAgent"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UpsertPushSubscription preserves the identity of an already registered endpoint.
func (s *Store) UpsertPushSubscription(ctx context.Context, subscription PushSubscription) (PushSubscription, error) {
	now := nowText()
	row, err := query.New(s.db).UpsertPushSubscription(ctx, query.UpsertPushSubscriptionParams{
		ID:        subscription.ID,
		Endpoint:  subscription.Endpoint,
		P256DH:    subscription.P256DH,
		Auth:      subscription.Auth,
		Label:     subscription.Label,
		UserAgent: subscription.UserAgent,
		Platform:  subscription.Platform,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return PushSubscription{}, fmt.Errorf("save push subscription: %w", err)
	}
	return pushSubscriptionFromRow(row)
}

// ListPushSubscriptions returns subscriptions ordered by their display labels.
func (s *Store) ListPushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := query.New(s.db).ListPushSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	subscriptions := make([]PushSubscription, 0, len(rows))
	for _, row := range rows {
		subscription, err := pushSubscriptionFromRow(row)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, nil
}

// RenamePushSubscription returns ErrNotFound when the subscription no longer exists.
func (s *Store) RenamePushSubscription(ctx context.Context, id string, label string) (PushSubscription, error) {
	row, err := query.New(s.db).RenamePushSubscription(ctx, query.RenamePushSubscriptionParams{
		ID:        strings.TrimSpace(id),
		Label:     strings.TrimSpace(label),
		UpdatedAt: nowText(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return PushSubscription{}, ErrNotFound
	}
	if err != nil {
		return PushSubscription{}, fmt.Errorf("rename push subscription: %w", err)
	}
	return pushSubscriptionFromRow(row)
}

// DeletePushSubscription returns ErrNotFound when the subscription no longer exists.
func (s *Store) DeletePushSubscription(ctx context.Context, id string) error {
	affected, err := query.New(s.db).DeletePushSubscription(ctx, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func pushSubscriptionFromRow(row query.PushSubscription) (PushSubscription, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("parse push subscription created time: %w", err)
	}
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("parse push subscription updated time: %w", err)
	}
	return PushSubscription{
		ID:        row.ID,
		Endpoint:  row.Endpoint,
		P256DH:    row.P256DH,
		Auth:      row.Auth,
		Label:     row.Label,
		UserAgent: row.UserAgent,
		Platform:  row.Platform,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}
