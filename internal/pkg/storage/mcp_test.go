package storage

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestMCPAuditEventsPruneAndList(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	old := now.Add(-91 * 24 * time.Hour)

	if err := store.CreateMCPAuditEvent(ctx, MCPAuditEvent{
		KeyID: "old-key", KeyName: "Old", Tool: "send_sms", ModemIDs: []string{"imei-b"},
		Outcome: "success", Duration: 20 * time.Millisecond, CreatedAt: old,
	}, old.Add(-time.Hour)); err != nil {
		t.Fatalf("CreateMCPAuditEvent(old) error = %v", err)
	}
	if err := store.CreateMCPAuditEvent(ctx, MCPAuditEvent{
		KeyID: "key-1", KeyName: "Agent", Tool: "get_modem_status", ModemIDs: []string{"imei-b", "imei-a"},
		Outcome: "error", ErrorCode: "modem_not_found", Duration: 1250 * time.Microsecond, CreatedAt: now,
	}, now.Add(-90*24*time.Hour)); err != nil {
		t.Fatalf("CreateMCPAuditEvent(current) error = %v", err)
	}

	events, err := store.ListMCPAuditEvents(ctx, 0, 50)
	if err != nil {
		t.Fatalf("ListMCPAuditEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListMCPAuditEvents() length = %d, want 1", len(events))
	}
	got := events[0]
	if got.KeyID != "key-1" || got.Tool != "get_modem_status" || got.ErrorCode != "modem_not_found" {
		t.Fatalf("ListMCPAuditEvents()[0] = %+v", got)
	}
	if want := []string{"imei-a", "imei-b"}; !slices.Equal(got.ModemIDs, want) {
		t.Fatalf("modem IDs = %v, want %v", got.ModemIDs, want)
	}
	if got.Duration != time.Millisecond {
		t.Fatalf("duration = %v, want 1ms storage precision", got.Duration)
	}
}

func TestListMCPAuditEventsPagination(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	for index := range 3 {
		if err := store.CreateMCPAuditEvent(ctx, MCPAuditEvent{
			KeyID: "key", KeyName: "Agent", Tool: "tool", Outcome: "success", CreatedAt: now.Add(time.Duration(index) * time.Second),
		}, now.Add(-90*24*time.Hour)); err != nil {
			t.Fatalf("CreateMCPAuditEvent(%d) error = %v", index, err)
		}
	}

	first, err := store.ListMCPAuditEvents(ctx, 0, 2)
	if err != nil {
		t.Fatalf("ListMCPAuditEvents(first) error = %v", err)
	}
	if len(first) != 2 || first[0].ID <= first[1].ID {
		t.Fatalf("first page IDs = %v, want descending pair", eventIDs(first))
	}
	second, err := store.ListMCPAuditEvents(ctx, first[1].ID, 2)
	if err != nil {
		t.Fatalf("ListMCPAuditEvents(second) error = %v", err)
	}
	if len(second) != 1 || second[0].ID >= first[1].ID {
		t.Fatalf("second page IDs = %v, want one older event", eventIDs(second))
	}
}

func eventIDs(events []MCPAuditEvent) []int64 {
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}
