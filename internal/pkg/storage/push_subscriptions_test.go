package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestPushSubscriptionStorage(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, store *Store)
	}{
		{
			name: "duplicate endpoint updates existing device",
			run: func(t *testing.T, ctx context.Context, store *Store) {
				first, err := store.UpsertPushSubscription(ctx, PushSubscription{
					ID: "first", Endpoint: "https://push.example/subscription", P256DH: "key-1", Auth: "auth-1", Label: "Phone",
				})
				if err != nil {
					t.Fatalf("UpsertPushSubscription() first error = %v", err)
				}
				updated, err := store.UpsertPushSubscription(ctx, PushSubscription{
					ID: "second", Endpoint: first.Endpoint, P256DH: "key-2", Auth: "auth-2", Label: "Laptop",
				})
				if err != nil {
					t.Fatalf("UpsertPushSubscription() update error = %v", err)
				}
				if updated.ID != first.ID || updated.P256DH != "key-2" || updated.Label != "Laptop" {
					t.Fatalf("updated subscription = %+v", updated)
				}
				items, err := store.ListPushSubscriptions(ctx)
				if err != nil {
					t.Fatalf("ListPushSubscriptions() error = %v", err)
				}
				if len(items) != 1 {
					t.Fatalf("subscriptions = %d, want 1", len(items))
				}
			},
		},
		{
			name: "rename and delete device",
			run: func(t *testing.T, ctx context.Context, store *Store) {
				item, err := store.UpsertPushSubscription(ctx, PushSubscription{
					ID: "device", Endpoint: "https://push.example/device", P256DH: "key", Auth: "auth", Label: "Phone",
				})
				if err != nil {
					t.Fatalf("UpsertPushSubscription() error = %v", err)
				}
				renamed, err := store.RenamePushSubscription(ctx, item.ID, " Laptop ")
				if err != nil {
					t.Fatalf("RenamePushSubscription() error = %v", err)
				}
				if renamed.Label != "Laptop" {
					t.Fatalf("renamed label = %q, want Laptop", renamed.Label)
				}
				if err := store.DeletePushSubscription(ctx, item.ID); err != nil {
					t.Fatalf("DeletePushSubscription() error = %v", err)
				}
				if err := store.DeletePushSubscription(ctx, item.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("DeletePushSubscription() second error = %v, want %v", err, ErrNotFound)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer store.Close()
			tt.run(t, ctx, store)
		})
	}
}
