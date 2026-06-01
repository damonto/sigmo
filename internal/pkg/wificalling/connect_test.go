//go:build wifi_calling

package wificalling

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	vowifi "github.com/damonto/vowifi-go"
	imsvoice "github.com/damonto/vowifi-go/ims/voice"
	"github.com/godbus/dbus/v5"
)

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

func sessionKeys(sessions map[string]*sessionState) string {
	keys := make([]string, 0, len(sessions))
	for key := range sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
