//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	imsclient "github.com/damonto/ims-client"
	imscall "github.com/damonto/ims-client/ims/call"
	usimreader "github.com/damonto/ims-client/usim/reader"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/ts43-go/ts43"
	"github.com/godbus/dbus/v5"
)

func TestIncomingMessageKey(t *testing.T) {
	tests := []struct {
		name string
		msg  imsclient.SMS
		want string
	}{
		{
			name: "uses SIP call id",
			msg: imsclient.SMS{
				CallID: " sms-call-id ",
			},
			want: "sms-call-id",
		},
		{
			name: "falls back to stable content hash",
			msg: imsclient.SMS{
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

func TestConnectedClientRequiresSameProfile(t *testing.T) {
	tests := []struct {
		name      string
		session   *sessionState
		profileID string
		wantErr   error
	}{
		{
			name:      "same profile",
			session:   &sessionState{client: &imsclient.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-a",
		},
		{
			name:      "different profile",
			session:   &sessionState{client: &imsclient.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-b",
			wantErr:   ErrNotConnected,
		},
		{
			name:      "disconnected",
			session:   &sessionState{client: &imsclient.Client{}, profileID: "profile-a"},
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
	}{
		{name: "client not connected", err: imsclient.ErrClientNotConnected, wantErr: ErrNotConnected},
		{name: "wrapped client not connected", err: errors.Join(errors.New("dialing call"), imsclient.ErrClientNotConnected), wantErr: ErrNotConnected},
		{name: "keeps other errors", err: errors.New("sip rejected"), wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeVoiceError(tt.err)
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
			if call.Direction != string(imscall.DirectionOutgoing) || call.State != string(imscall.StateFailed) {
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
		state        imscall.State
		wantAnswered time.Time
	}{
		{
			name:         "active call is marked answered",
			call:         VoiceCall{ID: "call-1", State: string(imscall.StateActive), UpdatedAt: at},
			state:        imscall.StateActive,
			wantAnswered: at,
		},
		{
			name:         "keeps existing answer time",
			call:         VoiceCall{ID: "call-1", State: string(imscall.StateActive), UpdatedAt: at, AnsweredAt: at.Add(-time.Second)},
			state:        imscall.StateActive,
			wantAnswered: at.Add(-time.Second),
		},
		{
			name:  "dialing call is not marked answered",
			call:  VoiceCall{ID: "call-1", State: string(imscall.StateDialing), UpdatedAt: at},
			state: imscall.StateDialing,
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

func TestForwardCallEventCreatesPendingOutgoingCall(t *testing.T) {
	tests := []struct {
		name  string
		event imsclient.CallEvent
	}{
		{
			name: "dial event before DialCall returns",
			event: imsclient.CallEvent{
				CallID: "call-1",
				State:  imscall.StateDialing,
				Cause:  "early dialog terminated",
				At:     time.Date(2026, 5, 28, 1, 21, 0, 0, time.UTC),
			},
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
			if call.Direction != string(imscall.DirectionOutgoing) || call.Number != "+12242255559" {
				t.Fatalf("call route = direction %q number %q, want outgoing trimmed number", call.Direction, call.Number)
			}
			if call.State != string(tt.event.State) || call.Reason != tt.event.Cause {
				t.Fatalf("call state = %q/%q, want %q/%q", call.State, call.Reason, tt.event.State, tt.event.Cause)
			}
			if call.StartedAt.IsZero() || !call.UpdatedAt.Equal(tt.event.At) {
				t.Fatalf("call times = started %v updated %v, want started set and updated %v", call.StartedAt, call.UpdatedAt, tt.event.At)
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

func TestBrowserVoiceMediaOfferUsesFullDuplexCodec(t *testing.T) {
	tests := []struct {
		name string
		want []imscall.AudioCodec
	}{
		{name: "browser codecs", want: []imscall.AudioCodec{imscall.CodecAMR, imscall.CodecPCMU}},
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
		wantCodecs []imscall.AudioCodecConfig
	}{
		{
			name: "browser codecs with dtmf",
			wantCodecs: []imscall.AudioCodecConfig{
				{Name: imscall.CodecAMR, PayloadTypes: []int{102}, ClockRate: 8000, ModeSet: "0,2,4,7"},
				{Name: imscall.CodecTelephoneEvent, PayloadTypes: []int{101}, ClockRate: 8000},
				{Name: imscall.CodecPCMU, PayloadTypes: []int{0}, ClockRate: 8000},
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
		codec imscall.AudioCodec
		want  bool
	}{
		{name: "amr", codec: imscall.CodecAMR, want: true},
		{name: "amr wb", codec: imscall.CodecAMRWB, want: false},
		{name: "pcmu", codec: imscall.CodecPCMU, want: true},
		{name: "evs", codec: imscall.CodecEVS, want: false},
		{name: "telephone event", codec: imscall.CodecTelephoneEvent, want: false},
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
		media: imscall.NegotiatedMedia{
			Codec:           imscall.CodecAMR,
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
	if info.Codec != string(imscall.CodecAMR) || info.PayloadType != 102 || info.ClockRate != 8000 {
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
	client := &imsclient.Client{}
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    client,
				connected: true,
				calls: map[string]*voiceCallState{
					"ringing": {
						info: VoiceCall{ID: "ringing", State: string(imscall.StateRinging)},
					},
					"answering": {
						info: VoiceCall{ID: "answering", State: string(imscall.StateAnswering)},
					},
					"active": {
						info: VoiceCall{ID: "active", State: string(imscall.StateActive)},
					},
					"ended": {
						info: VoiceCall{ID: "ended", State: string(imscall.StateEnded)},
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
	for _, id := range []string{"ringing", "answering", "active"} {
		call := session.calls[id].info
		if call.State != string(imscall.StateFailed) {
			t.Fatalf("call %s state = %q, want failed", id, call.State)
		}
		if call.Reason != "wifi calling disconnected" {
			t.Fatalf("call %s reason = %q, want wifi calling disconnected", id, call.Reason)
		}
		if call.EndedAt.IsZero() || call.UpdatedAt.IsZero() {
			t.Fatalf("call %s times = ended %v updated %v, want set", id, call.EndedAt, call.UpdatedAt)
		}
	}
	if got := session.calls["ended"].info.State; got != string(imscall.StateEnded) {
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
				client:    &imsclient.Client{},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imscall.StateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	c.markDisconnected("modem-1", &imsclient.Client{})

	session := c.sessions["modem-1"]
	if !session.connected {
		t.Fatal("stale client disconnected the active session")
	}
	if got := session.calls["active"].info.State; got != string(imscall.StateActive) {
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
				client:    &imsclient.Client{},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imscall.StateActive)},
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
	if events[0].Call.ID != "active" || events[0].Call.State != string(imscall.StateFailed) {
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
			err: &imsclient.NSDSWFCEntitlementError{
				Err:     imsclient.ErrWFCEntitlementUserActionRequired,
				Carrier: "Carrier",
				Websheet: imsclient.WFCWebsheet{
					URL:   "https://example.com/nsds",
					Data:  "token=abc",
					Title: "Wi-Fi Calling",
				},
			},
			wantURL:         "https://example.com/nsds",
			wantUserData:    "token=abc",
			wantContentType: "application/x-www-form-urlencoded",
		},
		{
			name: "ts43",
			err: &imsclient.WFCEntitlementError{
				Err:    imsclient.ErrWFCEntitlementUserActionRequired,
				Config: ts43.WFCConfig{CarrierName: "Carrier"},
				Status: ts43.WFCStatus{
					ServiceFlowURL:      "https://example.com/ts43?existing=1",
					ServiceFlowUserData: "token=abc",
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
