package storage

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpdateNativeSubscriptionPreservesRegistration(t *testing.T) {
	s := testStore(t)
	sub := NativeSubscription{
		ID:             "subscription",
		InstallationID: "phone",
		BackendName:    "backend",
		SendToken:      strings.Repeat("s", 32),
		EncryptionKey:  bytes.Repeat([]byte{1}, 32),
		AuthTokenHash:  "login",
		SMS:            true,
		Reminders:      true,
		Enabled:        true,
	}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	// Registration can rotate credentials after a client displays its preferences.
	sub.SendToken = strings.Repeat("r", 32)
	sub.EncryptionKey = bytes.Repeat([]byte{2}, 32)
	sub.Calls = true
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	sms, label := false, "renamed phone"
	update := NativeSubscriptionUpdate{SMS: &sms, Label: &label, AuthTokenHash: "renewed-login"}
	got, err := s.UpdateNativeSubscription(t.Context(), sub.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if got.SMS || got.Label != label || got.AuthTokenHash != update.AuthTokenHash {
		t.Fatal("preference update did not apply the requested fields")
	}
	if got.SendToken != sub.SendToken || !bytes.Equal(got.EncryptionKey, sub.EncryptionKey) ||
		got.InstallationID != sub.InstallationID || got.BackendName != sub.BackendName ||
		!got.Calls || !got.Reminders || !got.Enabled {
		t.Fatal("preference update overwrote registration or omitted preferences")
	}
	after, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("revision = %d, want %d", after.Revision, before.Revision+1)
	}
	if _, err := s.UpdateNativeSubscription(t.Context(), sub.ID, update); err != nil {
		t.Fatal(err)
	}
	repeated, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repeated != after {
		t.Fatalf("repeated update changed state: %+v, want %+v", repeated, after)
	}
}

func TestUpdateNativeSubscriptionDoesNotCreateGrants(t *testing.T) {
	for _, name := range []string{"missing", "deleted"} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			sub := NativeSubscription{
				ID:             "subscription",
				InstallationID: "phone",
				SendToken:      strings.Repeat("s", 32),
				EncryptionKey:  make([]byte, 32),
			}
			if name == "deleted" {
				if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
					t.Fatal(err)
				}
				if err := s.DeleteNativeSubscription(t.Context(), sub.ID); err != nil {
					t.Fatal(err)
				}
			}
			enabled := true
			if _, err := s.UpdateNativeSubscription(t.Context(), sub.ID, NativeSubscriptionUpdate{Enabled: &enabled}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("UpdateNativeSubscription() error = %v, want %v", err, ErrNotFound)
			}
			if _, err := s.NativeSubscription(t.Context(), sub.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("NativeSubscription() error = %v, want %v", err, ErrNotFound)
			}
		})
	}
}

func TestUpdateNativeSubscriptionRollsBackWithBadge(t *testing.T) {
	s := testStore(t)
	sub := NativeSubscription{
		ID:             "subscription",
		InstallationID: "phone",
		SendToken:      strings.Repeat("s", 32),
		EncryptionKey:  make([]byte, 32),
		SMS:            true,
		Enabled:        true,
	}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `
		CREATE TRIGGER reject_badge BEFORE INSERT ON native_push_outbox
		BEGIN SELECT RAISE(ABORT, 'test badge failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	sms := false
	if _, err := s.UpdateNativeSubscription(t.Context(), sub.ID, NativeSubscriptionUpdate{SMS: &sms}); err == nil {
		t.Fatal("UpdateNativeSubscription() succeeded despite a badge write failure")
	}
	got, err := s.NativeSubscription(t.Context(), sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SMS {
		t.Fatal("preference change survived a rolled-back transaction")
	}
	after, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("failed update changed state: %+v, want %+v", after, before)
	}
}

func TestConcurrentNativePreferenceUpdatesPreserveBothChanges(t *testing.T) {
	s := testStore(t)
	sub := NativeSubscription{
		ID:             "subscription",
		InstallationID: "phone",
		SendToken:      strings.Repeat("s", 32),
		EncryptionKey:  make([]byte, 32),
		SMS:            true,
		Enabled:        true,
	}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	sms, calls := false, true
	var wg sync.WaitGroup
	for _, update := range []NativeSubscriptionUpdate{{SMS: &sms}, {Calls: &calls}} {
		wg.Go(func() {
			if _, err := s.UpdateNativeSubscription(t.Context(), sub.ID, update); err != nil {
				t.Errorf("UpdateNativeSubscription() error = %v", err)
			}
		})
	}
	wg.Wait()
	got, err := s.NativeSubscription(t.Context(), sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SMS || !got.Calls {
		t.Fatalf("SMS, Calls = %t, %t; want false, true", got.SMS, got.Calls)
	}
}

func TestDeleteNativeSubscriptionIfCurrent(t *testing.T) {
	tests := []struct {
		name   string
		rotate func(*NativeSubscription)
	}{
		{name: "same credential"},
		{name: "send token rotated", rotate: func(s *NativeSubscription) { s.SendToken = strings.Repeat("n", 32) }},
		{name: "backend login replaced", rotate: func(s *NativeSubscription) { s.AuthTokenHash = "new-login" }},
		{name: "installation replaced", rotate: func(s *NativeSubscription) { s.InstallationID = "new-phone" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			old := NativeSubscription{ID: "subscription", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), AuthTokenHash: "old-login", Enabled: true}
			if err := s.UpsertNativeSubscription(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			current := old
			if tt.rotate != nil {
				tt.rotate(&current)
				if err := s.UpsertNativeSubscription(t.Context(), current); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := s.DeleteNativeSubscriptionIfCurrent(t.Context(), old)
			if err != nil || removed != (tt.rotate == nil) {
				t.Fatalf("revoke old credential: removed=%v, error=%v", removed, err)
			}
			got, err := s.NativeSubscription(t.Context(), old.ID)
			if removed {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("read revoked subscription: %v", err)
				}
			} else if err != nil || got.SendToken != current.SendToken || got.AuthTokenHash != current.AuthTokenHash || got.InstallationID != current.InstallationID {
				t.Fatalf("rotated credential was changed: %v", err)
			}
			items, err := s.NativeDeliveries(t.Context(), time.Now())
			if err != nil || (len(items) == 0) != removed {
				t.Fatalf("pending deliveries=%d, removed=%v, error=%v", len(items), removed, err)
			}
		})
	}
}

func TestNativeRetryPreservesNewBadgeAndServerDeadline(t *testing.T) {
	for _, beforeResponse := range []bool{true, false} {
		name := "badge arrives after response"
		if beforeResponse {
			name = "badge arrives during request"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			sub := NativeSubscription{ID: "subscription", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Enabled: true}
			if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			items, err := s.NativeDeliveries(t.Context(), time.Now())
			if err != nil || len(items) != 1 {
				t.Fatalf("initial deliveries=%d, error=%v", len(items), err)
			}
			old := items[0]
			coalesce := func() {
				t.Helper()
				sub.BackendName = "renamed backend"
				if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
					t.Fatal(err)
				}
			}
			if beforeResponse {
				coalesce()
			}
			retryAt := time.Now().Add(10 * time.Minute).Truncate(time.Millisecond)
			if err := s.RetryNativeDelivery(t.Context(), old, retryAt); err != nil {
				t.Fatal(err)
			}
			if !beforeResponse {
				coalesce()
			}
			if err := s.FinishNativeDelivery(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			items, err = s.NativeDeliveries(t.Context(), retryAt.Add(-time.Millisecond))
			if err != nil || len(items) != 0 {
				t.Fatalf("retried before server deadline: deliveries=%d, error=%v", len(items), err)
			}
			items, err = s.NativeDeliveries(t.Context(), retryAt)
			if err != nil || len(items) != 1 {
				t.Fatalf("deliveries at deadline=%d, error=%v", len(items), err)
			}
			if items[0].Revision <= old.Revision || items[0].Attempts != 0 {
				t.Fatalf("new badge lost revision or inherited old attempts: revision=%d, attempts=%d", items[0].Revision, items[0].Attempts)
			}
		})
	}
}

func TestNativeRetryDoesNotExtendEventLifetime(t *testing.T) {
	s := testStore(t)
	sub := NativeSubscription{ID: "subscription", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Enabled: true}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	items, err := s.NativeDeliveries(t.Context(), time.Now())
	if err != nil || len(items) != 1 {
		t.Fatalf("initial deliveries=%d, error=%v", len(items), err)
	}
	if err := s.RetryNativeDelivery(t.Context(), items[0], items[0].ExpiresAt); err != nil {
		t.Fatal(err)
	}
	items, err = s.NativeDeliveries(t.Context(), time.Now().Add(time.Hour))
	if err != nil || len(items) != 0 {
		t.Fatalf("event survived an unusable retry deadline: deliveries=%d, error=%v", len(items), err)
	}
	if _, err := s.NativeSubscription(t.Context(), sub.ID); err != nil {
		t.Fatalf("expiring an event removed its subscription: %v", err)
	}
}
