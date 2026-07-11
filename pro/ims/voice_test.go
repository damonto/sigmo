//go:build ims

package ims

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	imssip "github.com/damonto/ims-go/ims/sip"
	imsvoice "github.com/damonto/ims-go/ims/voice"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestNormalizeVoiceErrorMapsClientNotConnected(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
		want    string
	}{
		{name: "client not connected", err: imsgo.ErrClientNotConnected, wantErr: ErrNotConnected},
		{name: "wrapped client not connected", err: errors.Join(errors.New("dialing call"), imsgo.ErrClientNotConnected), wantErr: ErrNotConnected},
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
			call := failedOutgoingVoiceCallAt(tt.modemID, tt.profileID, string(AccessWiFiCalling), tt.number, tt.err, at)
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

func TestForwardCallEventCreatesPendingOutgoingCall(t *testing.T) {
	tests := []struct {
		name         string
		event        imsgo.CallEvent
		wantAnswered bool
	}{
		{
			name: "dial event before DialCall returns",
			event: imsgo.CallEvent{
				CallID: "call-1",
				State:  imsvoice.CallStateDialing,
				Cause:  "early dialog terminated",
				At:     time.Date(2026, 5, 28, 1, 21, 0, 0, time.UTC),
			},
		},
		{
			name: "confirmed event before DialCall returns",
			event: imsgo.CallEvent{
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

			c.forwardCallEvent("modem-1", 0, tt.event)

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
		event imsgo.CallEvent
	}{
		{
			name: "early media event before DialCall returns",
			event: imsgo.CallEvent{
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

			c.forwardCallEvent("modem-1", 0, tt.event)

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

func TestVoiceEventsIgnoreStaleSessionID(t *testing.T) {
	at := time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC)
	tests := []struct {
		name  string
		apply func(*coordinator)
	}{
		{
			name: "call event",
			apply: func(c *coordinator) {
				c.forwardCallEvent("modem-1", 1, imsgo.CallEvent{
					CallID: "call-1",
					State:  imsvoice.CallStateFailed,
					Cause:  "stale client closed",
					At:     at,
				})
			},
		},
		{
			name: "incoming call",
			apply: func(c *coordinator) {
				c.forwardIncomingCall(&mmodem.Modem{EquipmentIdentifier: "modem-1"}, "profile-1", 1, imsgo.IncomingCall{
					Call:       &imsvoice.Call{},
					FromNumber: "+12242255559",
					ReceivedAt: at,
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{
				sessions: map[string]*sessionState{
					"modem-1": {
						id:        2,
						profileID: "profile-1",
						calls: map[string]*voiceCallState{
							"call-1": {
								info: VoiceCall{ID: "call-1", State: string(imsvoice.CallStateDialing)},
							},
						},
						pendingDial: &pendingVoiceDial{profileID: "profile-1", number: "+12242255559", startedAt: at},
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			var events []VoiceEvent
			unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
				events = append(events, event)
			})
			defer unsubscribe()

			tt.apply(c)

			session := c.sessions["modem-1"]
			if len(session.calls) != 1 {
				t.Fatalf("stored calls = %d, want unchanged current session", len(session.calls))
			}
			if got := session.calls["call-1"].info.State; got != string(imsvoice.CallStateDialing) {
				t.Fatalf("call state = %q, want dialing", got)
			}
			if len(events) != 0 {
				t.Fatalf("events = %+v, want none", events)
			}
		})
	}
}

func TestFinishFailedPendingVoiceDialReusesEventCall(t *testing.T) {
	at := time.Date(2026, 5, 28, 1, 24, 0, 0, time.UTC)
	tests := []struct {
		name      string
		event     imsgo.CallEvent
		err       error
		wantState string
		wantCause string
	}{
		{
			name: "failed event before dial returns",
			event: imsgo.CallEvent{
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
			event: imsgo.CallEvent{
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
			event: imsgo.CallEvent{
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
			c.forwardCallEvent("modem-1", 0, tt.event)

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
			c.forwardCallEvent("modem-1", 0, imsgo.CallEvent{
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

	c.forwardCallEvent("modem-1", 0, imsgo.CallEvent{
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
		{name: "browser codecs", want: []imsvoice.AudioCodec{imsvoice.CodecEVS, imsvoice.CodecAMRWB, imsvoice.CodecAMR, imsvoice.CodecPCMU}},
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
				{Name: imsvoice.CodecEVS, PayloadTypes: []int{127}, ClockRate: 16000, Bitrate: "5.9-13.2", Bandwidth: "nb-swb"},
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
				if got.Name != want.Name || got.ClockRate != want.ClockRate || got.ModeSet != want.ModeSet || got.Bitrate != want.Bitrate || got.Bandwidth != want.Bandwidth || !slices.Equal(got.PayloadTypes, want.PayloadTypes) {
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
		{name: "evs", codec: imsvoice.CodecEVS, want: true},
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
			HFOnly:          true,
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
	if !info.HFOnly {
		t.Fatal("Info().HFOnly = false, want true")
	}
}
