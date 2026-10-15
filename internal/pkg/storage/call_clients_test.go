package storage

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallEventOrderUsesTimePrecision(t *testing.T) {
	tests := []struct {
		name    string
		stored  time.Duration
		updated time.Duration
		wantNew bool
	}{
		{name: "new fractional second", updated: time.Millisecond, wantNew: true},
		{name: "new nanosecond", stored: 100 * time.Millisecond, updated: 100*time.Millisecond + time.Nanosecond, wantNew: true},
		{name: "old whole second", stored: time.Millisecond},
		{name: "old lower precision", stored: 100*time.Millisecond + time.Nanosecond, updated: 100 * time.Millisecond},
		{name: "same timestamp", stored: 100 * time.Millisecond, updated: 100 * time.Millisecond, wantNew: true},
		{name: "next second", stored: 900 * time.Millisecond, updated: time.Second, wantNew: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			start := time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)
			stored := Call{
				ID:            "incoming",
				ProfileID:     "SIM",
				ModemID:       "modem",
				Route:         "volte",
				Direction:     "incoming",
				State:         "ringing",
				StartedAt:     start,
				UpdatedAt:     start.Add(tt.stored),
				OwnerClientID: "phone",
			}
			if err := s.SaveCall(t.Context(), stored); err != nil {
				t.Fatal(err)
			}
			update := stored
			update.State = "active"
			update.UpdatedAt = start.Add(tt.updated)
			update.OwnerClientID = ""
			got, published, err := s.SaveCallPreservingTerminal(t.Context(), update)
			if err != nil {
				t.Fatal(err)
			}
			want := stored
			if tt.wantNew {
				want = update
			}
			if published != tt.wantNew || got.State != want.State || !got.UpdatedAt.Equal(want.UpdatedAt) {
				t.Fatalf("saved state = %q at %v, published = %t; want %q at %v, %t",
					got.State, got.UpdatedAt, published, want.State, want.UpdatedAt, tt.wantNew)
			}
			if got.OwnerClientID != stored.OwnerClientID {
				t.Fatal("carrier event overwrote the client owner")
			}
		})
	}
}

func TestCallOwnershipUUIDAndLateEvents(t *testing.T) {
	store := testStore(t)
	call := Call{ID: "opaque/call@carrier", ProfileID: "sim", ModemID: "modem", Route: "wifi_calling", Direction: "incoming",
		Number: "10086", State: "ringing", StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.SaveCall(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	first, err := store.Call(t.Context(), call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.CallUUID) != 36 || first.BackendID == "" {
		t.Fatalf("missing stable call identity: %+v", first)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for index := range 10 {
		wg.Go(func() {
			claimed, err := store.ClaimCall(t.Context(), call.ID, fmt.Sprintf("client-%d", index))
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if claimed {
				winners.Add(1)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("claim winners = %d", winners.Load())
	}
	call.State = "active"
	call.UpdatedAt = call.UpdatedAt.Add(time.Second)
	active, _, err := store.SaveCallPreservingTerminal(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	if active.OwnerClientID == "" || active.CallUUID != first.CallUUID {
		t.Fatalf("carrier snapshot lost ownership or UUID: %+v", active)
	}
	if err := store.ReleaseCallClaim(t.Context(), call.ID, active.OwnerClientID); err != nil {
		t.Fatal(err)
	}
	call.State = "ended"
	call.UpdatedAt = call.UpdatedAt.Add(time.Second)
	if _, _, err := store.SaveCallPreservingTerminal(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	late, published, err := store.SaveCallPreservingTerminal(t.Context(), first)
	if err != nil || published || late.State != "ended" || late.OwnerClientID != active.OwnerClientID {
		t.Fatalf("late event: %+v, published=%t, error=%v", late, published, err)
	}
}

func TestCallInvitationsDeduplicateExpireAndCancel(t *testing.T) {
	for _, next := range []string{"delivered", "active", "ended", "expired"} {
		t.Run(next, func(t *testing.T) {
			store := testStore(t)
			sub := NativeSubscription{ID: "sub", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Calls: true, Enabled: true}
			if err := store.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			call := Call{ID: "incoming", ProfileID: "sim", ModemID: "modem", Route: "volte", Direction: "incoming", Number: "10086",
				State: "ringing", StartedAt: time.Now(), UpdatedAt: time.Now()}
			if err := store.SaveCall(t.Context(), call); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := store.EnqueueNativeCall(t.Context(), call); err != nil {
					t.Fatal(err)
				}
			}
			items, err := store.NativeCallDeliveries(t.Context(), time.Now())
			if err != nil || len(items) != 1 {
				t.Fatalf("invitations = %v, error=%v", items, err)
			}
			if items[0].Content.CallUUID == "" || items[0].Content.ProfileID != "sim" {
				t.Fatalf("invite identity = %+v", items[0].Content)
			}
			now := time.Now()
			switch next {
			case "delivered":
				if err := store.FinishNativeCall(t.Context(), items[0], time.Time{}); err != nil {
					t.Fatal(err)
				}
				if err := store.EnqueueNativeCall(t.Context(), call); err != nil {
					t.Fatal(err)
				}
			case "expired":
				now = now.Add(time.Minute)
			default:
				call.State = next
				if err := store.EnqueueNativeCall(t.Context(), call); err != nil {
					t.Fatal(err)
				}
			}
			items, err = store.NativeCallDeliveries(t.Context(), now)
			if err != nil || len(items) != 0 {
				t.Fatalf("remaining invitations = %v, error=%v", items, err)
			}
		})
	}
}

func TestRestartRetiresLiveCallsAndPendingInvitations(t *testing.T) {
	store := testStore(t)
	sub := NativeSubscription{ID: "sub", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Calls: true, Enabled: true}
	if err := store.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ringing", "answering", "dialing", "early_media", "active", "confirmed", "ended", "failed"} {
		call := Call{ID: state, ProfileID: "sim", ModemID: "modem", Route: "volte", Direction: "incoming", State: state,
			StartedAt: time.Now(), Reason: "original", OwnerClientID: "ios"}
		if err := store.SaveCall(t.Context(), call); err != nil {
			t.Fatal(err)
		}
		if err := store.EnqueueNativeCall(t.Context(), call); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.EndInterruptedCalls(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ringing", "answering", "dialing", "early_media", "active", "confirmed", "ended", "failed"} {
		call, err := store.Call(t.Context(), state)
		if err != nil {
			t.Fatal(err)
		}
		if state == "ended" || state == "failed" {
			if call.State != state || call.Reason != "original" {
				t.Fatalf("terminal history changed: %+v", call)
			}
			continue
		}
		if call.State != "ended" || call.Reason != "server_restarted" || call.EndedAt.IsZero() || call.OwnerClientID != "ios" {
			t.Fatalf("live call survived restart: %+v", call)
		}
		late := call
		late.State = state
		late.UpdatedAt = time.Now()
		if _, published, err := store.SaveCallPreservingTerminal(t.Context(), late); err != nil || published {
			t.Fatalf("late event after restart: published=%t, error=%v", published, err)
		}
	}
	var pending int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM native_call_outbox WHERE delivered = 0`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending invitations after restart = %d", pending)
	}
}

func TestRestartCallCleanupRollsBackAndCanRetry(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	sub := NativeSubscription{ID: "sub", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Calls: true, Enabled: true}
	if err := store.UpsertNativeSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	call := Call{ID: "incoming", ProfileID: "sim", ModemID: "modem", Route: "volte", Direction: "incoming", State: "ringing", StartedAt: time.Now()}
	if err := store.SaveCall(ctx, call); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueNativeCall(ctx, call); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_call_cleanup BEFORE DELETE ON native_call_outbox
		BEGIN SELECT RAISE(ABORT, 'injected cleanup failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.EndInterruptedCalls(ctx); err == nil {
		t.Fatal("expected cleanup failure")
	}
	stored, err := store.Call(ctx, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "ringing" || !stored.EndedAt.IsZero() || stored.Reason != "" {
		t.Fatalf("failed cleanup changed call: %+v", stored)
	}
	items, err := store.NativeCallDeliveries(ctx, time.Now())
	if err != nil || len(items) != 1 {
		t.Fatalf("invitations after rollback = %v, error = %v", items, err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER reject_call_cleanup`); err != nil {
		t.Fatal(err)
	}
	if err := store.EndInterruptedCalls(ctx); err != nil {
		t.Fatal(err)
	}
	retired, err := store.Call(ctx, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retired.State != "ended" || retired.Reason != "server_restarted" || retired.EndedAt.IsZero() {
		t.Fatalf("retry did not retire call: %+v", retired)
	}
	if err := store.EndInterruptedCalls(ctx); err != nil {
		t.Fatal(err)
	}
	repeated, err := store.Call(ctx, call.ID)
	if err != nil || repeated != retired {
		t.Fatalf("repeated cleanup changed terminal history: %+v, error = %v", repeated, err)
	}
	var pending int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_call_outbox WHERE delivered = 0`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending invitations after retry = %d", pending)
	}
}

func TestWithdrawnInvitationsDoNotDelayNewCalls(t *testing.T) {
	store := testStore(t)
	sub := NativeSubscription{ID: "sub", InstallationID: "phone", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Calls: true, Enabled: true}
	if err := store.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	for index := range 25 {
		call := Call{ID: fmt.Sprintf("call-%d", index), ProfileID: "SIM", ModemID: "modem", Route: "volte", Direction: "incoming", State: "ringing", Number: "10086", StartedAt: time.Now()}
		if err := store.SaveCall(t.Context(), call); err != nil {
			t.Fatal(err)
		}
		if err := store.EnqueueNativeCall(t.Context(), call); err != nil {
			t.Fatal(err)
		}
		if index < 24 {
			// The dispatcher can observe the terminal row before queue cleanup.
			call.State = "ended"
			if err := store.SaveCall(t.Context(), call); err != nil {
				t.Fatal(err)
			}
		}
	}
	items, err := store.NativeCallDeliveries(t.Context(), time.Now())
	if err != nil || len(items) != 1 || items[0].Content.CallID != "call-24" {
		t.Fatalf("deliverable calls=%+v, error=%v", items, err)
	}
}
