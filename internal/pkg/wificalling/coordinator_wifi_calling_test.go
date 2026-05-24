//go:build wifi_calling

package wificalling

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	imsclient "github.com/damonto/ims-client"
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
