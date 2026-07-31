package modem

import (
	"slices"
	"testing"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestSentSMSFromWWANCombinesMultipartResult(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Minute)
	refs := []uint32{41, 42}
	result := wwanmodem.SendResult{
		References: refs,
		Messages: []wwanmodem.Message{
			{
				Refs:      []wwanmodem.MessageRef{{Storage: wwanmodem.MessageStorageSIM, ID: 10}},
				State:     wwanmodem.MessageStateStoredSent,
				Timestamp: now,
			},
			{
				Refs: []wwanmodem.MessageRef{
					{Storage: wwanmodem.MessageStorageDevice, ID: 20},
					{Storage: wwanmodem.MessageStorageSIM, ID: 10},
				},
				State:     wwanmodem.MessageStateStoredSent,
				Number:    "+12025550199",
				Timestamp: earlier,
			},
		},
	}

	got := sentSMSFromWWAN(sentSMSConfig{
		modem:   &Modem{generation: 7},
		storage: wwanmodem.MessageStorageSIM,
		to:      "+12025550100",
		text:    "the complete original message",
		result:  result,
		now:     now.Add(time.Hour),
	})

	wantStoredRefs := []MessageRef{
		{Storage: wwanmodem.MessageStorageSIM, ID: 10},
		{Storage: wwanmodem.MessageStorageDevice, ID: 20},
	}
	if got.Generation != 7 || got.State != SMSStateSent || got.Storage != SMSStorageSM {
		t.Fatalf("SMS identity/state = %+v", got)
	}
	if got.Number != "+12025550199" || got.Text != "the complete original message" || !got.Timestamp.Equal(earlier) {
		t.Fatalf("SMS content = %+v", got)
	}
	if !slices.Equal(got.MessageReferences, refs) {
		t.Fatalf("message references = %v, want %v", got.MessageReferences, refs)
	}
	if !slices.Equal(got.Refs, wantStoredRefs) {
		t.Fatalf("stored refs = %v, want %v", got.Refs, wantStoredRefs)
	}

	refs[0] = 99
	result.Messages[0].Refs[0].ID = 99
	if got.MessageReferences[0] != 41 || got.Refs[0].ID != 10 {
		t.Fatalf("result aliases backend slices: references=%v refs=%v", got.MessageReferences, got.Refs)
	}
}

func TestSentSMSFromWWANBuildsFallbackResult(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	got := sentSMSFromWWAN(sentSMSConfig{
		storage: wwanmodem.MessageStorageDevice,
		to:      "+12025550100",
		text:    "hello",
		result:  wwanmodem.SendResult{References: []uint32{8}},
		now:     now,
	})

	if got.Generation != 0 || got.State != SMSStateSent || got.Storage != SMSStorageME || got.Number != "+12025550100" || got.Text != "hello" || !got.Timestamp.Equal(now) {
		t.Fatalf("fallback SMS = %+v", got)
	}
	if !slices.Equal(got.MessageReferences, []uint32{8}) {
		t.Fatalf("message references = %v, want [8]", got.MessageReferences)
	}
}
