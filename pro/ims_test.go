//go:build ims

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	procall "github.com/damonto/sigmo/pro/call"
	pims "github.com/damonto/sigmo/pro/ims"
)

func TestForwardCallsSkipsDTMF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	call := storage.Call{ID: "call-1", State: procall.StateActive}
	events := make(chan procall.Event, 1)
	events <- procall.Event{Call: call}
	dtmfEvents := make(chan procall.Event, 1)
	dtmfEvents <- procall.Event{Call: call, DTMF: &procall.DTMF{Digit: "5", At: time.Now()}}
	source := &fakeCallEventSource{events: events, dtmfEvents: dtmfEvents}
	var forwarded []storage.Call
	relay := callForwarderFunc(func(_ context.Context, got storage.Call) error {
		forwarded = append(forwarded, got)
		cancel()
		return nil
	})
	if err := forwardCalls(ctx, relay, source); err != nil {
		t.Fatalf("forwardCalls() error = %v", err)
	}
	if len(forwarded) != 1 || forwarded[0] != call {
		t.Fatalf("forwarded = %+v, want one state event for %+v", forwarded, call)
	}
	if len(events) != 0 || len(dtmfEvents) != 1 {
		t.Fatal("forwarder must consume only call state updates")
	}
	if !source.unsubscribed {
		t.Fatal("call event subscription was not removed")
	}
}

type fakeCallEventSource struct {
	events       <-chan procall.Event
	dtmfEvents   <-chan procall.Event
	unsubscribed bool
}

func (s *fakeCallEventSource) Subscribe(config procall.SubscriptionConfig) (<-chan procall.Event, func()) {
	if config.Kind == procall.EventKindDTMF {
		return s.dtmfEvents, func() { s.unsubscribed = true }
	}
	return s.events, func() { s.unsubscribed = true }
}

type callForwarderFunc func(context.Context, storage.Call) error

func (f callForwarderFunc) ForwardCall(ctx context.Context, call storage.Call) error {
	return f(ctx, call)
}

func TestWiFiCallingOverview(t *testing.T) {
	errStatus := errors.New("status read")
	tests := []struct {
		name              string
		status            pims.WiFiCallingStatus
		err               error
		wantWiFiEnabled   bool
		wantWiFiConnected bool
		wantErr           error
	}{
		{
			name: "fills connected status",
			status: pims.WiFiCallingStatus{
				WiFiCallingSettings: pims.WiFiCallingSettings{
					Enabled: true,
				},
				Connected: true,
			},
			wantWiFiEnabled:   true,
			wantWiFiConnected: true,
		},
		{
			name: "ignores unavailable route",
			err:  pims.ErrUnavailable,
		},
		{
			name: "ignores missing profile id",
			err:  mmodem.ErrProfileIDMissing,
		},
		{
			name:    "wraps status error",
			err:     errStatus,
			wantErr: errStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extension := wifiCallingOverview(func(ctx context.Context, modem *mmodem.Modem) (pims.WiFiCallingStatus, error) {
				return tt.status, tt.err
			})
			fields := &modemstatus.Fields{}

			err := extension(t.Context(), &mmodem.Modem{}, fields)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("wifiCallingOverview() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("wifiCallingOverview() error = %v", err)
			}
			if fields.WiFiCallingEnabled != tt.wantWiFiEnabled {
				t.Fatalf("WiFiCallingEnabled = %v, want %v", fields.WiFiCallingEnabled, tt.wantWiFiEnabled)
			}
			if fields.WiFiCallingConnected != tt.wantWiFiConnected {
				t.Fatalf("WiFiCallingConnected = %v, want %v", fields.WiFiCallingConnected, tt.wantWiFiConnected)
			}
		})
	}
}
