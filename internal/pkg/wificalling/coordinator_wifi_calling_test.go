//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	vowifi "github.com/damonto/vowifi-go"
	imssip "github.com/damonto/vowifi-go/ims/sip"
	imsvoice "github.com/damonto/vowifi-go/ims/voice"
	usimreader "github.com/damonto/vowifi-go/usim/reader"
	"github.com/godbus/dbus/v5"
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
			report: vowifi.SMSReport{Status: vowifi.SMSReportStatusReceivedBySME},
			want:   "delivered",
		},
		{
			name:   "retrying",
			report: vowifi.SMSReport{Status: vowifi.SMSReportStatusRetryingSMEBusy},
			want:   "retrying",
		},
		{
			name:   "permanent failure",
			report: vowifi.SMSReport{Status: vowifi.SMSReportStatusPermanentIncompatibleDestination},
			want:   "failed",
		},
		{
			name:   "temporary failure without retry",
			report: vowifi.SMSReport{Status: vowifi.SMSReportStatusNoRetryServiceRejected},
			want:   "failed",
		},
		{
			name:   "completed but not confirmed delivered keeps sent",
			report: vowifi.SMSReport{Status: vowifi.SMSReportStatusForwardedUnconfirmed},
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
				{Recipient: "+200", MessageReference: 7, Status: vowifi.SMSReportStatusReceivedBySME},
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
				{Recipient: "+200", MessageReference: 1, Status: vowifi.SMSReportStatusReceivedBySME},
				{Recipient: "+200", MessageReference: 2, Status: vowifi.SMSReportStatusReceivedBySME},
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
				{Recipient: "+200", MessageReference: 1, Status: vowifi.SMSReportStatusReceivedBySME},
				{Recipient: "+200", MessageReference: 2, Status: vowifi.SMSReportStatusPermanentIncompatibleDestination},
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
				{Recipient: "+200", MessageReference: 1, Status: vowifi.SMSReportStatusRetryingSMEBusy},
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
				{Recipient: "+200", MessageReference: 1, Status: vowifi.SMSReportStatusReceivedBySME},
			},
		},
		{
			name: "pending report applies when tracking is registered",
			submission: vowifi.SMSSubmission{Segments: []vowifi.SMSSubmissionSegment{
				{TPReference: 9},
			}},
			reports: []vowifi.SMSReport{
				{Recipient: "+200", MessageReference: 9, Status: vowifi.SMSReportStatusReceivedBySME},
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
		Status:           vowifi.SMSReportStatusReceivedBySME,
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

func TestRetryDelays(t *testing.T) {
	tests := []struct {
		name string
		want []time.Duration
	}{
		{
			name: "wifi calling connect backoff",
			want: []time.Duration{
				30 * time.Second,
				60 * time.Second,
				120 * time.Second,
				240 * time.Second,
				300 * time.Second,
				600 * time.Second,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Equal(retryDelays, tt.want) {
				t.Fatalf("retryDelays = %v, want %v", retryDelays, tt.want)
			}
		})
	}
}

func TestTerminalInfo(t *testing.T) {
	tests := []struct {
		name string
		imei string
		want vowifi.TerminalInfo
	}{
		{
			name: "uses real device imei and transfer device shape",
			imei: "123456789012345",
			want: vowifi.TerminalInfo{
				ID:              "123456789012345",
				Vendor:          "Google",
				Model:           "Pixel 8 Pro",
				SoftwareVersion: "15/AP3A.240905.015",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalInfo(tt.imei); got != tt.want {
				t.Fatalf("terminalInfo() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConnectedClientRequiresSameProfile(t *testing.T) {
	tests := []struct {
		name      string
		session   *sessionState
		profileID string
		wantErr   error
	}{
		{
			name:      "same profile",
			session:   &sessionState{client: &vowifi.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-a",
		},
		{
			name:      "different profile",
			session:   &sessionState{client: &vowifi.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-b",
			wantErr:   ErrNotConnected,
		},
		{
			name:      "disconnected",
			session:   &sessionState{client: &vowifi.Client{}, profileID: "profile-a"},
			profileID: "profile-a",
			wantErr:   ErrNotConnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{sessions: map[string]*sessionState{"modem-1": tt.session}}
			_, err := c.connectedClient("modem-1", tt.profileID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("connectedClient() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeVoiceErrorMapsClientNotConnected(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
		want    string
	}{
		{name: "client not connected", err: vowifi.ErrClientNotConnected, wantErr: ErrNotConnected},
		{name: "wrapped client not connected", err: errors.Join(errors.New("dialing call"), vowifi.ErrClientNotConnected), wantErr: ErrNotConnected},
		{
			name: "sip warning text",
			err: fmt.Errorf("dialing call: %w", imssip.NewResponseError(imssip.Message{
				StartLine: "SIP/2.0 403 Forbidden",
				Headers: imssip.Headers{
					{Name: "Warning", Value: `399 anonymous.invalid "Credit limit reached"`},
				},
			})),
			want: "Credit limit reached",
		},
		{name: "keeps other errors", err: errors.New("sip rejected"), wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeVoiceError(tt.err)
			if tt.want != "" {
				if err == nil || err.Error() != tt.want {
					t.Fatalf("normalizeVoiceError() error = %v, want %q", err, tt.want)
				}
				return
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeVoiceError() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && err != tt.err {
				t.Fatalf("normalizeVoiceError() error = %v, want original %v", err, tt.err)
			}
		})
	}
}

func TestFailedOutgoingVoiceCall(t *testing.T) {
	at := time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name      string
		modemID   string
		profileID string
		number    string
		err       error
		wantID    string
	}{
		{
			name:      "builds stable failed call identity",
			modemID:   "modem-1",
			profileID: "profile-a",
			number:    " +12242255559 ",
			err:       errors.New("sip rejected"),
			wantID:    "failed:" + "4b843c5bd19f8208780b58b8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := failedOutgoingVoiceCallAt(tt.modemID, tt.profileID, tt.number, tt.err, at)
			if call.ID != tt.wantID {
				t.Fatalf("ID = %q, want %q", call.ID, tt.wantID)
			}
			if call.ModemID != tt.modemID || call.ProfileID != tt.profileID {
				t.Fatalf("call identity = %+v, want modem/profile set", call)
			}
			if call.Direction != string(imsvoice.CallDirectionOutgoing) || call.State != string(imsvoice.CallStateFailed) {
				t.Fatalf("call state = direction %q state %q, want outgoing failed", call.Direction, call.State)
			}
			if call.Number != strings.TrimSpace(tt.number) || call.Reason != tt.err.Error() {
				t.Fatalf("call details = number %q reason %q, want trimmed number and reason", call.Number, call.Reason)
			}
			if !call.StartedAt.Equal(at) || !call.EndedAt.Equal(at) || !call.UpdatedAt.Equal(at) {
				t.Fatalf("call times = started %v ended %v updated %v, want %v", call.StartedAt, call.EndedAt, call.UpdatedAt, at)
			}
		})
	}
}

func TestInitialDialedVoiceCallState(t *testing.T) {
	at := time.Date(2026, 5, 28, 1, 20, 0, 0, time.UTC)
	tests := []struct {
		name         string
		call         VoiceCall
		state        imsvoice.CallState
		wantAnswered time.Time
	}{
		{
			name:         "active call is marked answered",
			call:         VoiceCall{ID: "call-1", State: string(imsvoice.CallStateActive), UpdatedAt: at},
			state:        imsvoice.CallStateActive,
			wantAnswered: at,
		},
		{
			name:         "keeps existing answer time",
			call:         VoiceCall{ID: "call-1", State: string(imsvoice.CallStateActive), UpdatedAt: at, AnsweredAt: at.Add(-time.Second)},
			state:        imsvoice.CallStateActive,
			wantAnswered: at.Add(-time.Second),
		},
		{
			name:  "dialing call is not marked answered",
			call:  VoiceCall{ID: "call-1", State: string(imsvoice.CallStateDialing), UpdatedAt: at},
			state: imsvoice.CallStateDialing,
		},
		{
			name:  "early media call is not marked answered",
			call:  VoiceCall{ID: "call-1", State: "early_media", UpdatedAt: at},
			state: imsvoice.CallState("early_media"),
		},
		{
			name:         "confirmed call is marked answered",
			call:         VoiceCall{ID: "call-1", State: string(imsvoice.CallStateConfirmed), UpdatedAt: at},
			state:        imsvoice.CallStateConfirmed,
			wantAnswered: at,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initialDialedVoiceCallState(tt.call, tt.state)
			if !got.AnsweredAt.Equal(tt.wantAnswered) {
				t.Fatalf("AnsweredAt = %v, want %v", got.AnsweredAt, tt.wantAnswered)
			}
		})
	}
}

func TestAdvanceVoiceCall(t *testing.T) {
	at := time.Date(2026, 5, 29, 9, 0, 0, 0, time.UTC)
	base := VoiceCall{
		ID:        "call-1",
		State:     string(imsvoice.CallStateDialing),
		Hold:      string(imsvoice.CallHoldNone),
		Reason:    "previous",
		StartedAt: at.Add(-time.Minute),
		UpdatedAt: at.Add(-time.Minute),
	}
	tests := []struct {
		name         string
		call         VoiceCall
		transition   voiceCallTransition
		wantState    string
		wantHold     string
		wantReason   string
		wantAnswered time.Time
		wantEnded    time.Time
		wantUpdated  time.Time
		wantChanged  bool
	}{
		{
			name: "active marks answered and preserves reason",
			call: base,
			transition: voiceCallTransition{
				state: string(imsvoice.CallStateActive),
				hold:  string(imsvoice.CallHoldNone),
				at:    at,
			},
			wantState:    string(imsvoice.CallStateActive),
			wantHold:     string(imsvoice.CallHoldNone),
			wantReason:   "previous",
			wantAnswered: at,
			wantUpdated:  at,
			wantChanged:  true,
		},
		{
			name: "answer action marks answered before active",
			call: base,
			transition: voiceCallTransition{
				state:    string(imsvoice.CallStateAnswering),
				answered: true,
				at:       at,
			},
			wantState:    string(imsvoice.CallStateAnswering),
			wantHold:     string(imsvoice.CallHoldNone),
			wantReason:   "previous",
			wantAnswered: at,
			wantUpdated:  at,
			wantChanged:  true,
		},
		{
			name: "terminal marks ended and records reason",
			call: base,
			transition: voiceCallTransition{
				state:     string(imsvoice.CallStateFailed),
				reason:    "network closed",
				reasonSet: true,
				at:        at,
			},
			wantState:   string(imsvoice.CallStateFailed),
			wantHold:    string(imsvoice.CallHoldNone),
			wantReason:  "network closed",
			wantEnded:   at,
			wantUpdated: at,
			wantChanged: true,
		},
		{
			name: "end action marks ended before terminal",
			call: base,
			transition: voiceCallTransition{
				state: string(imsvoice.CallStateEnding),
				ended: true,
				at:    at,
			},
			wantState:   string(imsvoice.CallStateEnding),
			wantHold:    string(imsvoice.CallHoldNone),
			wantReason:  "previous",
			wantEnded:   at,
			wantUpdated: at,
			wantChanged: true,
		},
		{
			name: "terminal call ignores later active event",
			call: VoiceCall{
				ID:        "call-1",
				State:     string(imsvoice.CallStateEnded),
				Reason:    "done",
				StartedAt: at.Add(-time.Minute),
				EndedAt:   at.Add(-time.Second),
				UpdatedAt: at.Add(-time.Second),
			},
			transition: voiceCallTransition{
				state: string(imsvoice.CallStateActive),
				at:    at,
			},
			wantState:   string(imsvoice.CallStateEnded),
			wantReason:  "done",
			wantEnded:   at.Add(-time.Second),
			wantUpdated: at.Add(-time.Second),
		},
		{
			name: "empty call id is ignored",
			call: VoiceCall{State: string(imsvoice.CallStateDialing)},
			transition: voiceCallTransition{
				state: string(imsvoice.CallStateActive),
				at:    at,
			},
			wantState: string(imsvoice.CallStateDialing),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := advanceVoiceCall(tt.call, tt.transition)
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if got.State != tt.wantState || got.Hold != tt.wantHold || got.Reason != tt.wantReason {
				t.Fatalf("call state = state %q hold %q reason %q, want %q/%q/%q", got.State, got.Hold, got.Reason, tt.wantState, tt.wantHold, tt.wantReason)
			}
			if !got.AnsweredAt.Equal(tt.wantAnswered) {
				t.Fatalf("AnsweredAt = %v, want %v", got.AnsweredAt, tt.wantAnswered)
			}
			if !got.EndedAt.Equal(tt.wantEnded) {
				t.Fatalf("EndedAt = %v, want %v", got.EndedAt, tt.wantEnded)
			}
			if !got.UpdatedAt.Equal(tt.wantUpdated) {
				t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, tt.wantUpdated)
			}
		})
	}
}

func TestStatusFromSession(t *testing.T) {
	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		settings      Settings
		session       *sessionState
		profileID     string
		wantState     string
		wantConnected bool
		wantDuration  int64
	}{
		{
			name:      "idle when disabled without session",
			profileID: "profile-1",
			wantState: StateIdle,
		},
		{
			name:      "disconnected when enabled without session",
			settings:  Settings{Enabled: true},
			profileID: "profile-1",
			wantState: StateDisconnected,
		},
		{
			name: "connecting session",
			session: &sessionState{
				phase:     sessionPhaseConnecting,
				profileID: "profile-1",
			},
			profileID: "profile-1",
			wantState: StateConnecting,
		},
		{
			name: "websheet required session",
			session: &sessionState{
				phase:     sessionPhaseWebsheetRequired,
				profileID: "profile-1",
			},
			profileID: "profile-1",
			wantState: StateWebsheetRequired,
		},
		{
			name: "connected session",
			session: &sessionState{
				phase:       sessionPhaseConnected,
				client:      &vowifi.Client{},
				profileID:   "profile-1",
				connectedAt: now.Add(-2 * time.Minute),
			},
			profileID:     "profile-1",
			wantState:     StateConnected,
			wantConnected: true,
			wantDuration:  120,
		},
		{
			name: "profile mismatch uses settings state",
			settings: Settings{
				Enabled: true,
			},
			session: &sessionState{
				phase:     sessionPhaseConnected,
				client:    &vowifi.Client{},
				profileID: "profile-2",
			},
			profileID: "profile-1",
			wantState: StateDisconnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusFromSession(tt.settings, tt.session, tt.profileID, now)
			if got.State != tt.wantState {
				t.Fatalf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.Connected != tt.wantConnected {
				t.Fatalf("Connected = %v, want %v", got.Connected, tt.wantConnected)
			}
			if got.DurationSeconds != tt.wantDuration {
				t.Fatalf("DurationSeconds = %d, want %d", got.DurationSeconds, tt.wantDuration)
			}
		})
	}
}

func TestMarkConnectingResetsDisconnectedSession(t *testing.T) {
	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:      &vowifi.Client{},
				connected:   true,
				connectedAt: now,
				phase:       sessionPhaseDisconnected,
				profileID:   "profile-1",
			},
		},
	}

	c.markConnecting("modem-1")

	session := c.sessions["modem-1"]
	if session.client != nil {
		t.Fatal("client is set, want nil")
	}
	if session.connected {
		t.Fatal("connected = true, want false")
	}
	if !session.connectedAt.IsZero() {
		t.Fatalf("connectedAt = %v, want zero", session.connectedAt)
	}
	if session.phase != sessionPhaseConnecting {
		t.Fatalf("phase = %q, want %q", session.phase, sessionPhaseConnecting)
	}
	got := statusFromSession(Settings{Enabled: true}, session, "profile-1", now)
	if got.State != StateConnecting {
		t.Fatalf("State = %q, want %q", got.State, StateConnecting)
	}
}

func TestForwardCallEventCreatesPendingOutgoingCall(t *testing.T) {
	tests := []struct {
		name         string
		event        vowifi.CallEvent
		wantAnswered bool
	}{
		{
			name: "dial event before DialCall returns",
			event: vowifi.CallEvent{
				CallID: "call-1",
				State:  imsvoice.CallStateDialing,
				Cause:  "early dialog terminated",
				At:     time.Date(2026, 5, 28, 1, 21, 0, 0, time.UTC),
			},
		},
		{
			name: "confirmed event before DialCall returns",
			event: vowifi.CallEvent{
				CallID: "call-2",
				State:  imsvoice.CallStateConfirmed,
				At:     time.Date(2026, 5, 28, 1, 22, 0, 0, time.UTC),
			},
			wantAnswered: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{
				sessions: map[string]*sessionState{
					"modem-1": {
						profileID: "profile-1",
						calls:     make(map[string]*voiceCallState),
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			pending := c.setPendingVoiceDial("modem-1", "profile-1", " +12242255559 ")
			var events []VoiceEvent
			unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
				events = append(events, event)
			})
			defer unsubscribe()

			c.forwardCallEvent("modem-1", tt.event)

			state := c.sessions["modem-1"].calls[tt.event.CallID]
			if state == nil {
				t.Fatal("pending outgoing call was not stored")
			}
			call := state.info
			if call.ModemID != "modem-1" || call.ProfileID != "profile-1" || call.ID != tt.event.CallID {
				t.Fatalf("call identity = %+v, want modem/profile/call id", call)
			}
			if call.Direction != string(imsvoice.CallDirectionOutgoing) || call.Number != "+12242255559" {
				t.Fatalf("call route = direction %q number %q, want outgoing trimmed number", call.Direction, call.Number)
			}
			if call.State != string(tt.event.State) || call.Reason != tt.event.Cause {
				t.Fatalf("call state = %q/%q, want %q/%q", call.State, call.Reason, tt.event.State, tt.event.Cause)
			}
			if call.StartedAt.IsZero() || !call.UpdatedAt.Equal(tt.event.At) {
				t.Fatalf("call times = started %v updated %v, want started set and updated %v", call.StartedAt, call.UpdatedAt, tt.event.At)
			}
			if tt.wantAnswered {
				if !call.AnsweredAt.Equal(tt.event.At) {
					t.Fatalf("AnsweredAt = %v, want %v", call.AnsweredAt, tt.event.At)
				}
			} else if !call.AnsweredAt.IsZero() {
				t.Fatalf("AnsweredAt = %v, want zero", call.AnsweredAt)
			}
			if len(events) != 1 || events[0].Call.ID != tt.event.CallID {
				t.Fatalf("events = %+v, want one pending call event", events)
			}

			c.clearPendingVoiceDial("modem-1", pending)
			if got := c.sessions["modem-1"].pendingDial; got != nil {
				t.Fatalf("pendingDial = %+v, want nil", got)
			}
		})
	}
}

func TestForwardCallEventStoresCallPointer(t *testing.T) {
	tests := []struct {
		name  string
		event vowifi.CallEvent
	}{
		{
			name: "early media event before DialCall returns",
			event: vowifi.CallEvent{
				Call:   &imsvoice.Call{},
				CallID: "call-1",
				State:  imsvoice.CallStateEarlyMedia,
				At:     time.Date(2026, 5, 28, 1, 23, 0, 0, time.UTC),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{
				sessions: map[string]*sessionState{
					"modem-1": {
						profileID:   "profile-1",
						calls:       make(map[string]*voiceCallState),
						pendingDial: &pendingVoiceDial{profileID: "profile-1", number: "+12242255559", startedAt: tt.event.At},
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}

			c.forwardCallEvent("modem-1", tt.event)

			state := c.sessions["modem-1"].calls[tt.event.CallID]
			if state == nil {
				t.Fatal("pending outgoing call was not stored")
			}
			if state.call != tt.event.Call {
				t.Fatalf("call pointer = %p, want %p", state.call, tt.event.Call)
			}
		})
	}
}

func TestFinishFailedPendingVoiceDialReusesEventCall(t *testing.T) {
	at := time.Date(2026, 5, 28, 1, 24, 0, 0, time.UTC)
	tests := []struct {
		name      string
		event     vowifi.CallEvent
		err       error
		wantState string
		wantCause string
	}{
		{
			name: "failed event before dial returns",
			event: vowifi.CallEvent{
				CallID: "sip-call-487",
				State:  imsvoice.CallStateFailed,
				Cause:  "Request Terminated",
				At:     at.Add(time.Second),
			},
			err:       errors.New("487 Request Terminated"),
			wantState: string(imsvoice.CallStateFailed),
			wantCause: "Request Terminated",
		},
		{
			name: "failed event without cause uses dial error",
			event: vowifi.CallEvent{
				CallID: "sip-call-487-empty-cause",
				State:  imsvoice.CallStateFailed,
				At:     at.Add(time.Second),
			},
			err:       errors.New("487 Request Terminated"),
			wantState: string(imsvoice.CallStateFailed),
			wantCause: "487 Request Terminated",
		},
		{
			name: "dialing event before dial failure",
			event: vowifi.CallEvent{
				CallID: "sip-call-dialing",
				State:  imsvoice.CallStateDialing,
				At:     at.Add(2 * time.Second),
			},
			err:       errors.New("487 Request Terminated"),
			wantState: string(imsvoice.CallStateFailed),
			wantCause: "487 Request Terminated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{
				sessions: map[string]*sessionState{
					"modem-1": {
						profileID: "profile-1",
						calls:     make(map[string]*voiceCallState),
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			pending := c.setPendingVoiceDial("modem-1", "profile-1", "+12242255559")
			pending.startedAt = at
			c.sessions["modem-1"].pendingDial.startedAt = at
			c.forwardCallEvent("modem-1", tt.event)

			got, ok := c.finishFailedPendingVoiceDial("modem-1", pending, tt.err)

			if !ok {
				t.Fatal("finishFailedPendingVoiceDial() ok = false, want true")
			}
			if got.ID != tt.event.CallID {
				t.Fatalf("ID = %q, want event call id %q", got.ID, tt.event.CallID)
			}
			if got.State != tt.wantState || got.Reason != tt.wantCause {
				t.Fatalf("state = %q/%q, want %q/%q", got.State, got.Reason, tt.wantState, tt.wantCause)
			}
			if got.EndedAt.IsZero() || got.UpdatedAt.IsZero() {
				t.Fatalf("times = ended %v updated %v, want terminal timestamps", got.EndedAt, got.UpdatedAt)
			}
			if len(c.sessions["modem-1"].calls) != 1 {
				t.Fatalf("stored calls = %d, want only the event call", len(c.sessions["modem-1"].calls))
			}
			if c.sessions["modem-1"].pendingDial != nil {
				t.Fatalf("pendingDial = %+v, want nil", c.sessions["modem-1"].pendingDial)
			}
			c.forwardCallEvent("modem-1", vowifi.CallEvent{
				CallID: tt.event.CallID + "-late",
				State:  imsvoice.CallStateFailed,
				Cause:  "late failed event",
				At:     at.Add(3 * time.Second),
			})
			if len(c.sessions["modem-1"].calls) != 1 {
				t.Fatalf("stored calls after late event = %d, want only the reused event call", len(c.sessions["modem-1"].calls))
			}
		})
	}
}

func TestFinishFailedPendingVoiceDialConsumesPendingWithoutEvent(t *testing.T) {
	at := time.Date(2026, 5, 28, 1, 25, 0, 0, time.UTC)
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				profileID: "profile-1",
				calls:     make(map[string]*voiceCallState),
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	pending := c.setPendingVoiceDial("modem-1", "profile-1", "+12242255559")

	got, ok := c.finishFailedPendingVoiceDial("modem-1", pending, errors.New("487 Request Terminated"))

	if ok {
		t.Fatal("finishFailedPendingVoiceDial() ok = true, want false")
	}
	if got.ID != "" {
		t.Fatalf("finishFailedPendingVoiceDial() = %+v, want no event call", got)
	}
	if c.sessions["modem-1"].pendingDial != nil {
		t.Fatalf("pendingDial = %+v, want nil", c.sessions["modem-1"].pendingDial)
	}

	c.forwardCallEvent("modem-1", vowifi.CallEvent{
		CallID: "sip-call-487",
		State:  imsvoice.CallStateFailed,
		Cause:  "Request Terminated",
		At:     at,
	})
	if len(c.sessions["modem-1"].calls) != 0 {
		t.Fatalf("stored calls = %d, want no late event call", len(c.sessions["modem-1"].calls))
	}
}

func TestBrowserVoiceMediaOfferUsesFullDuplexCodec(t *testing.T) {
	tests := []struct {
		name string
		want []imsvoice.AudioCodec
	}{
		{name: "browser codecs", want: []imsvoice.AudioCodec{imsvoice.CodecAMRWB, imsvoice.CodecAMR, imsvoice.CodecPCMU}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offer := browserVoiceMediaOffer()
			if !slices.Equal(offer.Codecs, tt.want) {
				t.Fatalf("Codecs = %v, want %v", offer.Codecs, tt.want)
			}
		})
	}
}

func TestBrowserVoiceConfigUsesFullDuplexCodec(t *testing.T) {
	tests := []struct {
		name       string
		wantCodecs []imsvoice.AudioCodecConfig
	}{
		{
			name: "browser codecs with dtmf",
			wantCodecs: []imsvoice.AudioCodecConfig{
				{Name: imsvoice.CodecAMRWB, PayloadTypes: []int{104}, ClockRate: 16000},
				{Name: imsvoice.CodecAMR, PayloadTypes: []int{102}, ClockRate: 8000, ModeSet: "0,2,4,7"},
				{Name: imsvoice.CodecTelephoneEvent, PayloadTypes: []int{101}, ClockRate: 8000},
				{Name: imsvoice.CodecPCMU, PayloadTypes: []int{0}, ClockRate: 8000},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := browserVoiceConfig()
			if cfg.PTime != 20*time.Millisecond || cfg.MaxPTime != 240*time.Millisecond {
				t.Fatalf("timing = ptime %v maxptime %v, want 20ms/240ms", cfg.PTime, cfg.MaxPTime)
			}
			if len(cfg.Codecs) != len(tt.wantCodecs) {
				t.Fatalf("Codecs length = %d, want %d", len(cfg.Codecs), len(tt.wantCodecs))
			}
			for i, want := range tt.wantCodecs {
				got := cfg.Codecs[i]
				if got.Name != want.Name || got.ClockRate != want.ClockRate || got.ModeSet != want.ModeSet || !slices.Equal(got.PayloadTypes, want.PayloadTypes) {
					t.Fatalf("Codecs[%d] = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestIsSupportedCallMediaCodec(t *testing.T) {
	tests := []struct {
		name  string
		codec imsvoice.AudioCodec
		want  bool
	}{
		{name: "amr", codec: imsvoice.CodecAMR, want: true},
		{name: "amr wb", codec: imsvoice.CodecAMRWB, want: true},
		{name: "pcmu", codec: imsvoice.CodecPCMU, want: true},
		{name: "evs", codec: imsvoice.CodecEVS, want: false},
		{name: "telephone event", codec: imsvoice.CodecTelephoneEvent, want: false},
		{name: "empty", codec: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSupportedCallMediaCodec(tt.codec); got != tt.want {
				t.Fatalf("isSupportedCallMediaCodec(%q) = %v, want %v", tt.codec, got, tt.want)
			}
		})
	}
}

func TestCallMediaSessionInfoIncludesPayloadFormat(t *testing.T) {
	session := callMediaSession{
		media: imsvoice.NegotiatedMedia{
			Codec:           imsvoice.CodecAMR,
			PayloadType:     102,
			ClockRate:       8000,
			Channels:        1,
			OctetAlign:      false,
			DTMFPayloadType: 101,
			DTMFClockRate:   8000,
			PTime:           20 * time.Millisecond,
		},
	}

	info := session.Info()
	if info.Codec != string(imsvoice.CodecAMR) || info.PayloadType != 102 || info.ClockRate != 8000 {
		t.Fatalf("Info() = %+v, want AMR payload 102 clock 8000", info)
	}
	if info.OctetAlign {
		t.Fatal("Info().OctetAlign = true, want false for bandwidth-efficient AMR")
	}
}

func TestReaderCandidatesPreferPrimaryThenFallbackPorts(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  []readerCandidate
	}{
		{
			name: "qmi primary falls back to at",
			modem: &mmodem.Modem{
				PrimaryPort:    "/dev/cdc-wdm1",
				PrimarySimSlot: 1,
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
				},
			},
			want: []readerCandidate{
				{portType: mmodem.ModemPortTypeQmi, device: "/dev/cdc-wdm1"},
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB6"},
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB7"},
			},
		},
		{
			name: "unknown primary uses supported ports",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyGPS0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeGps, Device: "/dev/ttyGPS0"},
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []readerCandidate{
				{portType: mmodem.ModemPortTypeMbim, device: "/dev/cdc-wdm0"},
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB2"},
			},
		},
		{
			name: "deduplicates primary port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyUSB2",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []readerCandidate{{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readerCandidates(tt.modem)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("readerCandidates() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOpenReaderFallsBackAfterPrimaryFailure(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort:    "/dev/cdc-wdm1",
		PrimarySimSlot: 2,
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
		},
	}
	var attempts []readerCandidate
	reader, err := openReaderWith(context.Background(), modem, func(_ context.Context, candidate readerCandidate, slot int) (usimreader.Reader, error) {
		attempts = append(attempts, candidate)
		if slot != 2 {
			t.Fatalf("slot = %d, want 2", slot)
		}
		if candidate.device != "/dev/ttyUSB7" {
			return nil, errors.New("reader unavailable")
		}
		return fakeUSIMReader{}, nil
	})
	if err != nil {
		t.Fatalf("openReaderWith() error = %v", err)
	}
	if reader == nil {
		t.Fatal("openReaderWith() reader is nil")
	}
	want := []readerCandidate{
		{portType: mmodem.ModemPortTypeQmi, device: "/dev/cdc-wdm1"},
		{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB6"},
		{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB7"},
	}
	if !slices.Equal(attempts, want) {
		t.Fatalf("attempts = %+v, want %+v", attempts, want)
	}
}

func TestOpenReaderReturnsJoinedCandidateErrors(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/cdc-wdm1",
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
		},
	}
	_, err := openReaderWith(context.Background(), modem, func(_ context.Context, candidate readerCandidate, _ int) (usimreader.Reader, error) {
		return nil, errors.New(readerPortTypeName(candidate.portType) + " unavailable")
	})
	if err == nil {
		t.Fatal("openReaderWith() error = nil, want error")
	}
	for _, want := range []string{"open QMI reader on /dev/cdc-wdm1", "open AT reader on /dev/ttyUSB6"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestMarkDisconnectedFailsOpenCalls(t *testing.T) {
	client := &vowifi.Client{}
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    client,
				connected: true,
				calls: map[string]*voiceCallState{
					"ringing": {
						info: VoiceCall{ID: "ringing", State: string(imsvoice.CallStateRinging)},
					},
					"answering": {
						info: VoiceCall{ID: "answering", State: string(imsvoice.CallStateAnswering)},
					},
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
					"ended": {
						info: VoiceCall{ID: "ended", State: string(imsvoice.CallStateEnded)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	var events []VoiceEvent
	unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	c.markDisconnected("modem-1", client)

	session := c.sessions["modem-1"]
	if session.connected || session.client != nil {
		t.Fatalf("session connected = %v client nil = %v, want disconnected", session.connected, session.client == nil)
	}
	if session.phase != sessionPhaseDisconnected {
		t.Fatalf("session phase = %q, want %q", session.phase, sessionPhaseDisconnected)
	}
	for _, id := range []string{"ringing", "answering", "active"} {
		call := session.calls[id].info
		if call.State != string(imsvoice.CallStateFailed) {
			t.Fatalf("call %s state = %q, want failed", id, call.State)
		}
		if call.Reason != "wifi calling disconnected" {
			t.Fatalf("call %s reason = %q, want wifi calling disconnected", id, call.Reason)
		}
		if call.EndedAt.IsZero() || call.UpdatedAt.IsZero() {
			t.Fatalf("call %s times = ended %v updated %v, want set", id, call.EndedAt, call.UpdatedAt)
		}
	}
	if got := session.calls["ended"].info.State; got != string(imsvoice.CallStateEnded) {
		t.Fatalf("ended call state = %q, want ended", got)
	}

	gotIDs := make([]string, 0, len(events))
	for _, event := range events {
		gotIDs = append(gotIDs, event.Call.ID)
	}
	sort.Strings(gotIDs)
	if want := []string{"active", "answering", "ringing"}; !slices.Equal(gotIDs, want) {
		t.Fatalf("event ids = %v, want %v", gotIDs, want)
	}
}

func TestMarkDisconnectedIgnoresStaleClient(t *testing.T) {
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    &vowifi.Client{},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	c.markDisconnected("modem-1", &vowifi.Client{})

	session := c.sessions["modem-1"]
	if !session.connected {
		t.Fatal("stale client disconnected the active session")
	}
	if got := session.calls["active"].info.State; got != string(imsvoice.CallStateActive) {
		t.Fatalf("active call state = %q, want active", got)
	}
}

func TestStopFailsOpenCallsBeforeRemovingSession(t *testing.T) {
	cancelled := false
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				cancel: func() {
					cancelled = true
				},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	var events []VoiceEvent
	unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	c.stop("modem-1")

	if !cancelled {
		t.Fatal("session was not cancelled")
	}
	if _, ok := c.sessions["modem-1"]; ok {
		t.Fatal("session was not removed")
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Call.ID != "active" || events[0].Call.State != string(imsvoice.CallStateFailed) {
		t.Fatalf("event = %+v, want failed active call", events[0])
	}
}

func TestStopByPathStopsMatchingSession(t *testing.T) {
	tests := []struct {
		name          string
		removedPath   dbus.ObjectPath
		wantRemaining string
	}{
		{
			name:          "removes matching path",
			removedPath:   "/modem/1",
			wantRemaining: "modem-2",
		},
		{
			name:          "ignores unknown path",
			removedPath:   "/modem/3",
			wantRemaining: "modem-1,modem-2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cancelled := make(map[string]bool)
			session := func(modemID string, path dbus.ObjectPath) *sessionState {
				return &sessionState{
					cancel: func() {
						cancelled[modemID] = true
					},
					modemPath: path,
					profileID: modemID + "-profile",
					client:    nil,
					connected: true,
				}
			}
			c := &coordinator{sessions: map[string]*sessionState{
				"modem-1": session("modem-1", "/modem/1"),
				"modem-2": session("modem-2", "/modem/2"),
			}}

			c.stopByPath(tt.removedPath)

			gotRemaining := sessionKeys(c.sessions)
			if gotRemaining != tt.wantRemaining {
				t.Fatalf("remaining sessions = %q, want %q", gotRemaining, tt.wantRemaining)
			}
			if tt.removedPath == "/modem/1" && !cancelled["modem-1"] {
				t.Fatal("matching session was not cancelled")
			}
		})
	}
}

func TestWFCWebsheetRequestFromEntitlementErrors(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantURL         string
		wantUserData    string
		wantContentType string
	}{
		{
			name: "nsds",
			err: &vowifi.WFCEntitlementError{
				Err: vowifi.ErrWFCEntitlementUserActionRequired,
				Result: vowifi.WFCEntitlementResult{
					Scheme:  vowifi.WFCEntitlementSchemeNSDS,
					Carrier: "Carrier",
					Websheet: &vowifi.WFCWebsheet{
						Kind:  vowifi.WFCWebsheetKindEmergencyAddress,
						URL:   "https://example.com/nsds",
						Data:  "token=abc",
						Title: "Wi-Fi Calling",
					},
				},
			},
			wantURL:         "https://example.com/nsds",
			wantUserData:    "token=abc",
			wantContentType: "application/x-www-form-urlencoded",
		},
		{
			name: "ts43",
			err: &vowifi.WFCEntitlementError{
				Err: vowifi.ErrWFCEntitlementUserActionRequired,
				Result: vowifi.WFCEntitlementResult{
					Scheme:  vowifi.WFCEntitlementSchemeTS43,
					Carrier: "Carrier",
					Websheet: &vowifi.WFCWebsheet{
						Kind:  vowifi.WFCWebsheetKindEmergencyAddress,
						URL:   "https://example.com/ts43?existing=1",
						Data:  "token=abc",
						Title: "Wi-Fi Calling",
					},
				},
			},
			wantURL: "https://example.com/ts43?existing=1&token=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{AllowPrivateHosts: true})}
			req, ok := c.wfcWebsheetRequest(tt.err)
			if !ok {
				t.Fatal("wfcWebsheetRequest() ok = false")
			}
			if req.URL != tt.wantURL {
				t.Fatalf("URL = %q, want %q", req.URL, tt.wantURL)
			}
			if req.UserData != tt.wantUserData {
				t.Fatalf("UserData = %q, want %q", req.UserData, tt.wantUserData)
			}
			if req.ContentType != tt.wantContentType {
				t.Fatalf("ContentType = %q, want %q", req.ContentType, tt.wantContentType)
			}
		})
	}
}

func TestCreateWFCWebsheetFromEntitlementResult(t *testing.T) {
	tests := []struct {
		name       string
		result     vowifi.WFCEntitlementResult
		wantMethod string
		wantErr    error
	}{
		{
			name: "opens emergency address websheet",
			result: vowifi.WFCEntitlementResult{
				Scheme:  vowifi.WFCEntitlementSchemeNSDS,
				Carrier: "Carrier",
				Action:  vowifi.WFCEntitlementActionOpenWebsheet,
				Websheet: &vowifi.WFCWebsheet{
					Kind:  vowifi.WFCWebsheetKindEmergencyAddress,
					URL:   "http://127.0.0.1/e911",
					Data:  "token=abc",
					Title: "E911",
				},
			},
			wantMethod: "POST",
		},
		{
			name: "pending",
			result: vowifi.WFCEntitlementResult{
				Action:  vowifi.WFCEntitlementActionWait,
				Pending: true,
			},
			wantErr: ErrEntitlementPending,
		},
		{
			name: "denied",
			result: vowifi.WFCEntitlementResult{
				Action: vowifi.WFCEntitlementActionDenied,
			},
			wantErr: ErrEntitlementDenied,
		},
		{
			name: "missing websheet",
			result: vowifi.WFCEntitlementResult{
				Action: vowifi.WFCEntitlementActionOpenWebsheet,
			},
			wantErr: ErrWebsheetUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{AllowPrivateHosts: true})}
			info, err := c.createWFCWebsheet(context.Background(), tt.result)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("createWFCWebsheet() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("createWFCWebsheet() error = %v", err)
			}
			if info.Method != tt.wantMethod {
				t.Fatalf("Method = %q, want %q", info.Method, tt.wantMethod)
			}
		})
	}
}

func TestWFCWebsheetCallbackResult(t *testing.T) {
	tests := []struct {
		name     string
		callback websheet.Callback
		want     wfcWebsheetCallbackAction
	}{
		{
			name:     "entitlement changed retries connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "VoWiFiWebServiceFlow", Method: "entitlementChanged", Event: "entitlementChanged", ResultCode: "success"},
			want:     wfcWebsheetCallbackRetry,
		},
		{
			name:     "manual done retries connection",
			callback: websheet.Callback{Event: "finishFlow"},
			want:     wfcWebsheetCallbackRetry,
		},
		{
			name:     "dismiss cancels pending connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "VoWiFiWebServiceFlow", Method: "dismissFlow", Event: "dismissFlow", ResultCode: "cancel"},
			want:     wfcWebsheetCallbackDismiss,
		},
		{
			name:     "close webview cancels pending connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "WiFiCallingWebViewController", Method: "CloseWebView"},
			want:     wfcWebsheetCallbackDismiss,
		},
		{
			name:     "status update retries connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "WiFiCallingWebViewController", Method: "phoneServicesAccountStatusChanged", Event: "phoneServicesAccountStatusChanged"},
			want:     wfcWebsheetCallbackRetry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wfcWebsheetCallbackResult(tt.callback); got != tt.want {
				t.Fatalf("wfcWebsheetCallbackResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sessionKeys(sessions map[string]*sessionState) string {
	keys := make([]string, 0, len(sessions))
	for key := range sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

type fakeUSIMReader struct{}

func (fakeUSIMReader) ListApplications(context.Context) ([]usimreader.Application, error) {
	return nil, nil
}

func (fakeUSIMReader) GetFileAttributes(context.Context, usimreader.FileRef) (usimreader.FileAttributes, error) {
	return usimreader.FileAttributes{}, nil
}

func (fakeUSIMReader) ReadTransparent(context.Context, usimreader.TransparentRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) ReadRecord(context.Context, usimreader.RecordRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) Authenticate3G(context.Context, usimreader.AuthenticateRequest) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) SMSPPDownload(context.Context, usimreader.SMSPPDownloadRequest) (usimreader.SMSPPDownloadResponse, error) {
	return usimreader.SMSPPDownloadResponse{}, nil
}

func (fakeUSIMReader) Close() error {
	return nil
}
