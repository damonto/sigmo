package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	appcall "github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/httpapi"
	pims "github.com/damonto/sigmo/internal/app/ims"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/pkg/license"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func TestIMSCallHistoryAvailableWithoutPro(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "calls.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	runtime := &Runtime{Storage: store, License: &license.Controller{}}
	t.Cleanup(func() {
		if err := runtime.close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := configureIMS(t.Context(), runtime, nil); err != nil {
		t.Fatal(err)
	}
	server := echo.New()
	server.HTTPErrorHandler = httpapi.HTTPErrorHandler
	for _, register := range runtime.routes {
		if err := register(server.Group("/api/v1"), router.RegisterConfig{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/api/v1/calls", http.StatusOK},
		{http.MethodDelete, "/api/v1/calls/missing", http.StatusNotFound},
		{http.MethodGet, "/api/v1/modems/test/wifi-calling/settings", http.StatusForbidden},
		{http.MethodGet, "/api/v1/modems/test/volte/settings", http.StatusForbidden},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
	for _, name := range []feature.Name{feature.NativeCalls, feature.CallHistory} {
		if !runtime.Policy.Allows(name) {
			t.Errorf("base feature %q requires Pro authorization", name)
		}
	}
}

func TestForwardCallsSkipsDTMF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	call := storage.Call{ID: "call-1", State: appcall.StateActive}
	events := make(chan appcall.Event, 1)
	events <- appcall.Event{Call: call}
	dtmfEvents := make(chan appcall.Event, 1)
	dtmfEvents <- appcall.Event{Call: call, DTMF: &appcall.DTMF{Digit: "5", At: time.Now()}}
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
	events       <-chan appcall.Event
	dtmfEvents   <-chan appcall.Event
	unsubscribed bool
}

func (s *fakeCallEventSource) Subscribe(config appcall.SubscriptionConfig) (<-chan appcall.Event, func()) {
	if config.Kind == appcall.EventKindDTMF {
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

func TestVoLTEOverview(t *testing.T) {
	errStatus := errors.New("status read")
	tests := []struct {
		name          string
		status        pims.VoLTEStatus
		err           error
		wantEnabled   bool
		wantConnected bool
		wantErr       error
	}{
		{name: "connected", status: pims.VoLTEStatus{VoLTESettings: pims.VoLTESettings{Enabled: true}, Connected: true}, wantEnabled: true, wantConnected: true},
		{name: "enabled and disconnected", status: pims.VoLTEStatus{VoLTESettings: pims.VoLTESettings{Enabled: true}}, wantEnabled: true},
		{name: "disabled"},
		{name: "unavailable", err: pims.ErrUnavailable},
		{name: "missing profile", err: mmodem.ErrProfileIDMissing},
		{name: "wraps status error", err: errStatus, wantErr: errStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			extension := voLTEOverview(func(ctx context.Context, modem *mmodem.Modem) (pims.VoLTEStatus, error) {
				if modem != device || ctx != t.Context() {
					t.Fatal("status read must use the requested modem and context")
				}
				return tt.status, tt.err
			})
			fields := &modemstatus.Fields{WiFiCallingEnabled: true, WiFiCallingConnected: true}
			err := extension(t.Context(), device, fields)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("voLTEOverview() error = %v, want %v", err, tt.wantErr)
			}
			if fields.VoLTEEnabled != tt.wantEnabled || fields.VoLTEConnected != tt.wantConnected {
				t.Fatalf("VoLTE state = %v/%v, want %v/%v", fields.VoLTEEnabled, fields.VoLTEConnected, tt.wantEnabled, tt.wantConnected)
			}
			if !fields.WiFiCallingEnabled || !fields.WiFiCallingConnected {
				t.Fatal("VoLTE status must not change Wi-Fi Calling state")
			}
		})
	}
}
