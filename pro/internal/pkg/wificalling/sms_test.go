//go:build wifi_calling

package wificalling

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
	vowifi "github.com/damonto/vowifi-go"
	imssms "github.com/damonto/vowifi-go/ims/sms"
)

func TestIncomingMessageKey(t *testing.T) {
	tests := []struct {
		name string
		msg  vowifi.SMS
		want string
	}{
		{
			name: "uses SIP call id",
			msg: vowifi.SMS{
				CallID: " sms-call-id ",
			},
			want: "sms-call-id",
		},
		{
			name: "falls back to stable content hash",
			msg: vowifi.SMS{
				From:          "+100",
				To:            "+200",
				ServiceCenter: "+300",
				Text:          "hello",
				ReceivedAt:    time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC),
			},
			want: "incoming:43fb1fcec1abb693537998196debdb7c282d9b7136c646bbdd7ac549bd2a7774",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := incomingMessageKey(tt.msg); got != tt.want {
				t.Fatalf("incomingMessageKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewOutgoingMessageKey(t *testing.T) {
	first, err := newOutgoingMessageKey()
	if err != nil {
		t.Fatalf("newOutgoingMessageKey() first error = %v", err)
	}
	second, err := newOutgoingMessageKey()
	if err != nil {
		t.Fatalf("newOutgoingMessageKey() second error = %v", err)
	}

	tests := []struct {
		name string
		key  string
	}{
		{name: "first key", key: first},
		{name: "second key", key: second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.key, "outgoing:") {
				t.Fatalf("key = %q, want outgoing prefix", tt.key)
			}
			if len(tt.key) != len("outgoing:")+32 {
				t.Fatalf("key length = %d, want %d", len(tt.key), len("outgoing:")+32)
			}
		})
	}
	if first == second {
		t.Fatalf("newOutgoingMessageKey() returned duplicate keys %q", first)
	}
}

func TestSMSSubmissionUpdateStatus(t *testing.T) {
	tests := []struct {
		name   string
		update vowifi.SMSSubmissionUpdate
		want   string
	}{
		{
			name:   "submitted to IMS keeps sent",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSSubmittedToIMS},
		},
		{
			name:   "accepted by SMSC keeps sent",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSAcceptedBySMSC},
		},
		{
			name:   "submit unknown keeps sent",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSSubmitUnknown},
		},
		{
			name:   "delivery completed keeps sent",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSDeliveryCompleted},
		},
		{
			name:   "rejected by SMSC fails",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSRejectedBySMSC},
			want:   "failed",
		},
		{
			name:   "delivery failed fails",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSDeliveryFailed},
			want:   "failed",
		},
		{
			name:   "delivered",
			update: vowifi.SMSSubmissionUpdate{State: vowifi.SMSDelivered},
			want:   "delivered",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := smsSubmissionUpdateStatus(tt.update); got != tt.want {
				t.Fatalf("smsSubmissionUpdateStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSMSDeliveryReportTimeoutMatchesVowifiDefault(t *testing.T) {
	tests := []struct {
		name string
		want time.Duration
	}{
		{name: "default", want: imssms.DefaultDeliveryReportTimeout()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := smsDeliveryReportTimeout()
			if got <= 0 {
				t.Fatalf("smsDeliveryReportTimeout() = %v, want positive duration", got)
			}
			if got != tt.want {
				t.Fatalf("smsDeliveryReportTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSMSSubmissionTrackingAggregatesSegments(t *testing.T) {
	tests := []struct {
		name        string
		submission  vowifi.SMSSubmission
		updates     []vowifi.SMSSubmissionUpdate
		wantUpdates []string
	}{
		{
			name: "single segment delivered",
			submission: vowifi.SMSSubmission{ID: "sms-1", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-1", SegmentIndex: 0, State: vowifi.SMSDelivered},
			},
			wantUpdates: []string{"delivered"},
		},
		{
			name: "multipart waits for all delivered reports",
			submission: vowifi.SMSSubmission{ID: "sms-2", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
				{Index: 1},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-2", SegmentIndex: 0, State: vowifi.SMSDelivered},
				{SubmissionID: "sms-2", SegmentIndex: 1, State: vowifi.SMSDelivered},
			},
			wantUpdates: []string{"delivered"},
		},
		{
			name: "multipart failed segment wins",
			submission: vowifi.SMSSubmission{ID: "sms-3", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
				{Index: 1},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-3", SegmentIndex: 0, State: vowifi.SMSDelivered},
				{SubmissionID: "sms-3", SegmentIndex: 1, State: vowifi.SMSDeliveryFailed},
			},
			wantUpdates: []string{"failed"},
		},
		{
			name: "SMSC rejection fails",
			submission: vowifi.SMSSubmission{ID: "sms-4", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-4", SegmentIndex: 0, State: vowifi.SMSRejectedBySMSC},
			},
			wantUpdates: []string{"failed"},
		},
		{
			name: "unknown states keep sent",
			submission: vowifi.SMSSubmission{ID: "sms-5", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-5", SegmentIndex: 0, State: vowifi.SMSSubmitUnknown},
				{SubmissionID: "sms-5", SegmentIndex: 0, State: vowifi.SMSDeliveryCompleted},
			},
		},
		{
			name: "missing final delivery keeps sent",
			submission: vowifi.SMSSubmission{ID: "sms-6", Segments: []vowifi.SMSSubmissionSegment{
				{Index: 0},
				{Index: 1},
			}},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-6", SegmentIndex: 0, State: vowifi.SMSDelivered},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{}
			msg := storage.Message{
				ModemID:     "modem-1",
				ProfileID:   "profile-a",
				Source:      storage.MessageSourceRouted,
				ExternalKey: "outgoing-1",
				Recipient:   "+200",
				Status:      "sent",
			}

			if got := c.trackOutgoingSMSSubmission(msg, tt.submission); got != "" {
				t.Fatalf("trackOutgoingSMSSubmission() = %q, want empty initial status", got)
			}
			var gotUpdates []string
			for _, submissionUpdate := range tt.updates {
				status := smsSubmissionUpdateStatus(submissionUpdate)
				if status == "" {
					continue
				}
				update, ok := c.recordSMSSubmissionUpdate(msg.ModemID, msg.ProfileID, submissionUpdate, status)
				if ok {
					gotUpdates = append(gotUpdates, update.status)
				}
			}
			if !slices.Equal(gotUpdates, tt.wantUpdates) {
				t.Fatalf("updates = %v, want %v", gotUpdates, tt.wantUpdates)
			}
		})
	}
}

func TestWatchSMSSubmissionUpdatesStopsOnTimeout(t *testing.T) {
	tests := []struct {
		name           string
		submission     vowifi.SMSSubmission
		updates        []vowifi.SMSSubmissionUpdate
		pendingStore   string
		wantSubmission int
	}{
		{
			name: "idle open channel returns",
			submission: vowifi.SMSSubmission{
				ID: "sms-1",
			},
		},
		{
			name: "partial delivery tracker is cleaned up",
			submission: vowifi.SMSSubmission{
				ID: "sms-2",
				Segments: []vowifi.SMSSubmissionSegment{
					{Index: 0},
					{Index: 1},
				},
			},
			updates: []vowifi.SMSSubmissionUpdate{
				{SubmissionID: "sms-2", SegmentIndex: 0, State: vowifi.SMSDelivered},
			},
		},
		{
			name: "pending store status survives cleanup",
			submission: vowifi.SMSSubmission{
				ID: "sms-3",
				Segments: []vowifi.SMSSubmissionSegment{
					{Index: 0},
				},
			},
			pendingStore:   "delivered",
			wantSubmission: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{}
			msg := storage.Message{
				ModemID:     "modem-1",
				ProfileID:   "profile-a",
				Source:      storage.MessageSourceRouted,
				ExternalKey: "outgoing-1",
				Recipient:   "+200",
				Status:      "sent",
			}
			if len(tt.submission.Segments) > 0 {
				c.trackOutgoingSMSSubmission(msg, tt.submission)
			}
			if tt.pendingStore != "" {
				key := outgoingSMSSubmissionKey(msg.ModemID, msg.ProfileID, tt.submission.ID)
				c.smsSubmissions[key].pendingStore = tt.pendingStore
			}

			updates := make(chan vowifi.SMSSubmissionUpdate, len(tt.updates))
			for _, update := range tt.updates {
				updates <- update
			}
			tt.submission.Updates = updates

			done := make(chan struct{})
			go func() {
				c.watchSMSSubmissionUpdatesWithTimeout(msg.ModemID, msg.ProfileID, tt.submission, 10*time.Millisecond)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("watchSMSSubmissionUpdatesWithTimeout() did not return")
			}
			if got := len(c.smsSubmissions); got != tt.wantSubmission {
				t.Fatalf("smsSubmissions = %d, want %d", got, tt.wantSubmission)
			}
		})
	}
}

func TestApplyPendingSMSStatusAfterStoreMiss(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	c := &coordinator{store: store}
	msg := storage.Message{
		ModemID:     "modem-1",
		ProfileID:   "profile-a",
		Source:      storage.MessageSourceRouted,
		ExternalKey: "outgoing-1",
		Sender:      "+100",
		Recipient:   "+200",
		Text:        "hello",
		Timestamp:   time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		Status:      "sent",
		Routed:      true,
	}
	submission := vowifi.SMSSubmission{ID: "sms-7", Segments: []vowifi.SMSSubmissionSegment{
		{Index: 0},
	}}
	c.trackOutgoingSMSSubmission(msg, submission)
	submissionUpdate := vowifi.SMSSubmissionUpdate{
		SubmissionID: "sms-7",
		SegmentIndex: 0,
		SegmentCount: 1,
		Recipient:    msg.Recipient,
		TPReference:  7,
		State:        vowifi.SMSDelivered,
	}
	statusUpdate, ok := c.recordSMSSubmissionUpdate(msg.ModemID, msg.ProfileID, submissionUpdate, smsSubmissionUpdateStatus(submissionUpdate))
	if !ok {
		t.Fatal("recordSMSSubmissionUpdate() = false, want true")
	}
	c.updateStoredSMSStatus(msg.ModemID, msg.Recipient, statusUpdate)

	inserted, err := store.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatalf("InsertMessage() error = %v", err)
	}
	if !inserted {
		t.Fatal("InsertMessage() = false, want true")
	}
	if err := c.ApplyPendingSMSStatus(ctx, msg); err != nil {
		t.Fatalf("ApplyPendingSMSStatus() error = %v", err)
	}
	messages, err := store.ListByParticipant(ctx, msg.ProfileID, msg.Recipient)
	if err != nil {
		t.Fatalf("ListByParticipant() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Status != "delivered" {
		t.Fatalf("messages = %+v, want delivered message", messages)
	}
	if len(c.smsSubmissions) != 0 {
		t.Fatalf("smsSubmissions = %d, want cleaned up after final status", len(c.smsSubmissions))
	}
}

func TestApplyPendingSMSStatusIgnoresUnknown(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	c := &coordinator{store: store}
	msg := storage.Message{
		ModemID:     "modem-1",
		ProfileID:   "profile-a",
		Source:      storage.MessageSourceRouted,
		ExternalKey: "outgoing-1",
		Sender:      "+100",
		Recipient:   "+200",
		Text:        "hello",
		Timestamp:   time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		Status:      "sent",
		Routed:      true,
	}
	c.trackOutgoingSMSSubmission(msg, vowifi.SMSSubmission{ID: "sms-8", Segments: []vowifi.SMSSubmissionSegment{
		{Index: 0},
	}})

	inserted, err := store.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatalf("InsertMessage() error = %v", err)
	}
	if !inserted {
		t.Fatal("InsertMessage() = false, want true")
	}
	if err := c.ApplyPendingSMSStatus(ctx, msg); err != nil {
		t.Fatalf("ApplyPendingSMSStatus() error = %v", err)
	}
	unknown := vowifi.SMSSubmissionUpdate{
		SubmissionID: "sms-8",
		SegmentIndex: 0,
		State:        vowifi.SMSSubmitUnknown,
	}
	if status := smsSubmissionUpdateStatus(unknown); status != "" {
		t.Fatalf("smsSubmissionUpdateStatus() = %q, want empty", status)
	}
	messages, err := store.ListByParticipant(ctx, msg.ProfileID, msg.Recipient)
	if err != nil {
		t.Fatalf("ListByParticipant() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Status != "sent" {
		t.Fatalf("messages = %+v, want sent message", messages)
	}
}
