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

func TestSMSReportStatus(t *testing.T) {
	tests := []struct {
		name   string
		report vowifi.SMSReport
		want   string
	}{
		{
			name:   "delivered",
			report: vowifi.SMSReport{Status: imssms.ReportStatusReceivedBySME},
			want:   "delivered",
		},
		{
			name:   "retrying",
			report: vowifi.SMSReport{Status: imssms.ReportStatusRetryingSMEBusy},
			want:   "retrying",
		},
		{
			name:   "permanent failure",
			report: vowifi.SMSReport{Status: imssms.ReportStatusPermanentIncompatibleDestination},
			want:   "failed",
		},
		{
			name:   "temporary failure without retry",
			report: vowifi.SMSReport{Status: imssms.ReportStatusNoRetryServiceRejected},
			want:   "failed",
		},
		{
			name:   "completed but not confirmed delivered keeps sent",
			report: vowifi.SMSReport{Status: imssms.ReportStatusForwardedUnconfirmed},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := smsReportStatus(tt.report); got != tt.want {
				t.Fatalf("smsReportStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSMSReportTrackingAggregatesSegments(t *testing.T) {
	tests := []struct {
		name            string
		submission      vowifi.SMSSubmission
		reports         []vowifi.SMSReport
		wantUpdates     []string
		wantTrackStatus string
	}{
		{
			name: "single segment delivered",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 7},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 7, Status: imssms.ReportStatusReceivedBySME},
			},
			wantUpdates: []string{"delivered"},
		},
		{
			name: "multipart waits for all delivered reports",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 1},
				{TPReference: 2},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 1, Status: imssms.ReportStatusReceivedBySME},
				{Recipient: "+200", MessageReference: 2, Status: imssms.ReportStatusReceivedBySME},
			},
			wantUpdates: []string{"delivered"},
		},
		{
			name: "multipart failed segment wins",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 1},
				{TPReference: 2},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 1, Status: imssms.ReportStatusReceivedBySME},
				{Recipient: "+200", MessageReference: 2, Status: imssms.ReportStatusPermanentIncompatibleDestination},
			},
			wantUpdates: []string{"failed"},
		},
		{
			name: "partial retrying updates status",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 1},
				{TPReference: 2},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 1, Status: imssms.ReportStatusRetryingSMEBusy},
			},
			wantUpdates: []string{"retrying"},
		},
		{
			name: "missing reports keep sent",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 1},
				{TPReference: 2},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 1, Status: imssms.ReportStatusReceivedBySME},
			},
		},
		{
			name: "pending report applies when tracking is registered",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 9},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 9, Status: imssms.ReportStatusReceivedBySME},
			},
			wantTrackStatus: "delivered",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{}
			msg := storage.Message{
				ModemID:     "modem-1",
				ProfileID:   "profile-a",
				Source:      storage.MessageSourceWiFiCalling,
				ExternalKey: "outgoing-1",
				Recipient:   "+200",
				Status:      "sent",
			}
			if tt.wantTrackStatus != "" {
				for _, report := range tt.reports {
					c.recordSMSReport(msg.ModemID, msg.ProfileID, report, smsReportStatus(report))
				}
				if got := c.trackOutgoingSMSReport(msg, tt.submission); got != tt.wantTrackStatus {
					t.Fatalf("trackOutgoingSMSReport() = %q, want %q", got, tt.wantTrackStatus)
				}
				return
			}

			if got := c.trackOutgoingSMSReport(msg, tt.submission); got != "" {
				t.Fatalf("trackOutgoingSMSReport() = %q, want empty initial status", got)
			}
			var updates []string
			for _, report := range tt.reports {
				update, ok := c.recordSMSReport(msg.ModemID, msg.ProfileID, report, smsReportStatus(report))
				if ok {
					updates = append(updates, update.status)
				}
			}
			if !slices.Equal(updates, tt.wantUpdates) {
				t.Fatalf("updates = %v, want %v", updates, tt.wantUpdates)
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
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: "outgoing-1",
		Sender:      "+100",
		Recipient:   "+200",
		Text:        "hello",
		Timestamp:   time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		Status:      "sent",
		WiFiCalling: true,
	}
	c.trackOutgoingSMSReport(msg, vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
		{TPReference: 7},
	}})

	c.forwardSMSReport(ctx, msg.ModemID, msg.ProfileID, vowifi.SMSReport{
		Recipient:        msg.Recipient,
		MessageReference: 7,
		Status:           imssms.ReportStatusReceivedBySME,
	})

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
	if len(c.smsReports) != 0 {
		t.Fatalf("smsReports = %d, want cleaned up after final status", len(c.smsReports))
	}
}
