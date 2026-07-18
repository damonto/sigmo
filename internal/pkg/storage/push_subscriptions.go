package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

func (s *Store) UpsertPushSubscription(ctx context.Context, subscription PushSubscription) (PushSubscription, error) {
	now := nowText()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO push_subscriptions (
			id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			label = excluded.label,
			user_agent = excluded.user_agent,
			platform = excluded.platform,
			updated_at = excluded.updated_at
		RETURNING id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at
	`, subscription.ID, subscription.Endpoint, subscription.P256DH, subscription.Auth, subscription.Label,
		subscription.UserAgent, subscription.Platform, now, now)
	stored, err := scanPushSubscription(row)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("save push subscription: %w", err)
	}
	return stored, nil
}

func (s *Store) ListPushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at
		FROM push_subscriptions
		ORDER BY label COLLATE NOCASE, created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	defer rows.Close()

	subscriptions := []PushSubscription{}
	for rows.Next() {
		subscription, err := scanPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	return subscriptions, nil
}

func (s *Store) RenamePushSubscription(ctx context.Context, id string, label string) (PushSubscription, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE push_subscriptions
		SET label = ?, updated_at = ?
		WHERE id = ?
		RETURNING id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at
	`, strings.TrimSpace(label), nowText(), strings.TrimSpace(id))
	stored, err := scanPushSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PushSubscription{}, ErrNotFound
	}
	if err != nil {
		return PushSubscription{}, fmt.Errorf("rename push subscription: %w", err)
	}
	return stored, nil
}

func (s *Store) DeletePushSubscription(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted push subscription count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type pushSubscriptionScanner interface {
	Scan(dest ...any) error
}

func scanPushSubscription(row pushSubscriptionScanner) (PushSubscription, error) {
	var subscription PushSubscription
	var createdAt, updatedAt string
	if err := row.Scan(
		&subscription.ID,
		&subscription.Endpoint,
		&subscription.P256DH,
		&subscription.Auth,
		&subscription.Label,
		&subscription.UserAgent,
		&subscription.Platform,
		&createdAt,
		&updatedAt,
	); err != nil {
		return PushSubscription{}, err
	}
	var err error
	subscription.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("parse push subscription created time: %w", err)
	}
	subscription.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("parse push subscription updated time: %w", err)
	}
	return subscription, nil
}
