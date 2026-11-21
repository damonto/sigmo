package automation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/ims"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestDownloadReadsOnlyRequestedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bytes") != "1024" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("download did not request the bounded identity response")
		}
		switch r.URL.Path {
		case "/exact":
			fmt.Fprint(w, strings.Repeat("x", 1024))
		case "/partial":
			w.(http.Flusher).Flush()
			fmt.Fprint(w, strings.Repeat("x", 500))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	for _, tt := range []struct {
		path    string
		bytes   int64
		failure bool
	}{{"/exact", 1024, false}, {"/partial", 500, true}, {"/error", 0, true}} {
		t.Run(tt.path, func(t *testing.T) {
			n, err := readDownload(t.Context(), server.Client(), server.URL+tt.path, 1024)
			if n != tt.bytes || (err != nil) != tt.failure {
				t.Fatalf("download = %d, %v", n, err)
			}
		})
	}
}

type fakeDevice struct {
	device    *mmodem.Modem
	busy      bool
	connected bool
	restored  bool
	switches  int
	access    func(ims.Access) (enabled, connected bool, err error)
	smsAccess ims.Access
}

func newFakeModem(iccid string) *mmodem.Modem {
	return &mmodem.Modem{
		EquipmentIdentifier: "modem", PrimarySIMSlot: 1,
		SIM:    &mmodem.SIM{Identifier: iccid},
		Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady, Registration: wwanmodem.RegistrationHome},
	}
}

func (f *fakeDevice) Find(context.Context, string) (*mmodem.Modem, error) { return f.device, nil }
func (f *fakeDevice) ProfileInventory(context.Context, *mmodem.Modem) (*esim.Inventory, error) {
	profiles := []esim.Profile{}
	for _, iccid := range []string{"line", "original"} {
		status := "disabled"
		if f.device.SIMIdentity().ICCID == iccid {
			status = "enabled"
		}
		profiles = append(profiles, esim.Profile{ICCID: iccid, ProfileStateName: status})
	}
	return &esim.Inventory{SEs: []esim.SecureElement{{ID: "se", EID: "eid", Profiles: profiles}}}, nil
}
func (f *fakeDevice) SwitchForAutomation(ctx context.Context, device *mmodem.Modem, change esim.AutomationSwitch, before func(context.Context, *mmodem.Modem) error) error {
	if device.SIMIdentity().ICCID != change.FromICCID || change.EID != "eid" || change.SEID != "se" || change.Slot != 1 {
		return esim.ErrAutomationSIMChanged
	}
	if err := before(ctx, device); err != nil {
		return err
	}
	f.device = newFakeModem(change.ToICCID)
	f.switches++
	f.connected = false
	return nil
}
func (f *fakeDevice) Current(context.Context, *mmodem.Modem) (*internet.Connection, error) {
	if f.connected {
		return &internet.Connection{Status: internet.StatusConnected, APN: "existing"}, nil
	}
	return &internet.Connection{Status: internet.StatusDisconnected}, nil
}
func (f *fakeDevice) HasOngoingCall(string) bool { return f.busy }
func (f *fakeDevice) AutomationAccessStatus(_ context.Context, _ *mmodem.Modem, access ims.Access) (bool, bool, error) {
	if f.access == nil {
		return false, false, nil
	}
	return f.access(access)
}
func (f *fakeDevice) SendSMSVia(_ context.Context, _ *mmodem.Modem, access ims.Access, _, _ string) (storage.Message, error) {
	f.smsAccess = access
	return storage.Message{}, nil
}
func (*fakeDevice) WithAutomationConnection(context.Context, *mmodem.Modem, mmodem.SIMIdentity, func(*internet.Connection) error) error {
	return errors.New("unexpected data action")
}
func (f *fakeDevice) RestoreAutomationConnection(_ context.Context, device *mmodem.Modem, identity mmodem.SIMIdentity, prefs internet.Preferences) error {
	if device != f.device || identity.ICCID != "original" || prefs.APN != "existing" {
		return errors.New("unexpected connection")
	}
	f.restored = true
	return nil
}

type fakeSMS struct {
	pin   string
	count int
	err   error
}

func (f *fakeSMS) SendWithProfileVia(ctx context.Context, device *mmodem.Modem, req message.SendRequest, sender message.Sender, check func(context.Context) error) (string, error) {
	f.pin = req.ExpectedProfileID
	if req.ExpectedSIM != device.SIMIdentity() || f.pin != device.SIMIdentity().ICCID {
		return "", message.ErrProfileChanged
	}
	if err := check(ctx); err != nil {
		return "", err
	}
	f.count++
	if sender != nil {
		_, err := sender(ctx, device, req.To, req.Text)
		return req.To, err
	}
	return req.To, f.err
}

func TestSMSOnTemporaryESIMWithRecovery(t *testing.T) {
	db := testStore(t)
	for _, test := range []struct {
		name               string
		manualChange, busy bool
		code               string
	}{{"restore", false, false, "restored"}, {"manual switch", true, false, "restore_skipped"}, {"active call", false, true, ""}} {
		t.Run(test.name, func(t *testing.T) {
			device := &fakeDevice{device: newFakeModem("original"), connected: true, busy: test.busy}
			sms := &fakeSMS{err: message.ErrSendUnconfirmed}
			exec := &Execution{store: db, registry: device, esim: device, internet: device, messages: sms}
			task := storage.SIMAutomation{ModemID: "modem", ProfileID: "line", SEID: "se", EID: "eid"}
			var journal restoration
			to, body := "+12025550123", "example"
			result := exec.execute(t.Context(), task, Action{Type: "sms", To: &to, Text: &body}, func(_ context.Context, value restoration) error {
				if device.device.SIMIdentity().ICCID != "original" {
					t.Fatal("journal written after changing the SIM")
				}
				journal = value
				return nil
			})
			if test.busy {
				if result.Code != "call_in_progress" || device.switches != 0 || sms.count != 0 || journal.Previous != "" {
					t.Fatalf("busy run = %+v, switches %d, SMS %d, journal %+v", result, device.switches, sms.count, journal)
				}
				return
			}
			if result.Status != "unconfirmed" || sms.count != 1 || sms.pin != "line" || journal.Previous == "" {
				t.Fatalf("run = %+v, SMS %+v, journal %+v", result, sms, journal)
			}
			if test.manualChange {
				device.device = newFakeModem("user-selected")
			}
			code, retry := exec.restore(t.Context(), task, journal)
			if code != test.code || retry || device.restored == test.manualChange {
				t.Fatalf("restoration = %q, %v, connected %v", code, retry, device.restored)
			}
		})
	}
}

func TestAutomationWaitsForEligibleRoute(t *testing.T) {
	const disconnected = -1
	for _, tt := range []struct {
		name                  string
		action                string
		only, wifi, volte     bool
		wifiReady, volteReady time.Duration
		wantRoute, wantCode   string
		wantWait              time.Duration
	}{
		{name: "SMS waits for Wi-Fi even with VoLTE ready", action: actionSMS, wifi: true, volte: true, wifiReady: 3 * time.Second, wantRoute: call.RouteWiFiCalling, wantWait: 3 * time.Second},
		{name: "call waits for Wi-Fi even with VoLTE ready", action: actionCall, wifi: true, volte: true, wifiReady: 3 * time.Second, wantRoute: call.RouteWiFiCalling, wantWait: 3 * time.Second},
		{name: "SMS falls back only at the deadline", action: actionSMS, wifi: true, volte: true, wifiReady: disconnected, wantRoute: call.RouteVoLTE, wantWait: readinessTimeout},
		{name: "call falls back only at the deadline", action: actionCall, wifi: true, volte: true, wifiReady: disconnected, wantRoute: call.RouteVoLTE, wantWait: readinessTimeout},
		{name: "SMS waits for VoLTE with Wi-Fi disabled", action: actionSMS, volte: true, volteReady: 2 * time.Second, wantRoute: call.RouteVoLTE, wantWait: 2 * time.Second},
		{name: "IMS routes share the same wait", action: actionSMS, wifi: true, volte: true, wifiReady: disconnected, volteReady: readinessTimeout + time.Second, wantCode: "network_unavailable", wantWait: readinessTimeout},
		{name: "Wi-Fi timeout cannot use the modem", action: actionSMS, wifi: true, wifiReady: disconnected, wantCode: "network_unavailable", wantWait: readinessTimeout},
		{name: "VoLTE timeout cannot use the modem", action: actionSMS, volte: true, volteReady: disconnected, wantCode: "network_unavailable", wantWait: readinessTimeout},
		{name: "SMS can use modem with both settings off", action: actionSMS, wantRoute: call.RouteModem},
		{name: "call requires IMS", action: actionCall, wantCode: "call_unavailable"},
		{name: "strict SMS stops if Wi-Fi is disabled", action: actionSMS, only: true, volte: true, wantCode: "wifi_calling_unavailable"},
		{name: "strict call times out even with VoLTE ready", action: actionCall, only: true, wifi: true, volte: true, wifiReady: disconnected, wantCode: "wifi_calling_unavailable", wantWait: readinessTimeout},
		{name: "strict SMS uses connected Wi-Fi", action: actionSMS, only: true, wifi: true, wantRoute: call.RouteWiFiCalling},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				device := &fakeDevice{device: newFakeModem("line"), access: func(access ims.Access) (bool, bool, error) {
					if access == ims.AccessWiFiCalling {
						return tt.wifi, tt.wifiReady >= 0 && time.Since(start) >= tt.wifiReady, nil
					}
					return tt.volte, tt.volteReady >= 0 && time.Since(start) >= tt.volteReady, nil
				}}
				exec := &Execution{internet: device, policy: feature.NewPolicy(func() bool { return true })}
				line := activeLine{device: device.device, identity: device.device.SIMIdentity()}
				route, code := exec.waitReady(t.Context(), line, Action{Type: tt.action, WiFiCallingOnly: tt.only})
				if elapsed := time.Since(start); route != tt.wantRoute || code != tt.wantCode || elapsed != tt.wantWait {
					t.Fatalf("ready = %q, %q after %v, want %q, %q after %v", route, code, elapsed, tt.wantRoute, tt.wantCode, tt.wantWait)
				}
			})
		})
	}
}

func TestSMSRechecksWiFiBeforeSubmission(t *testing.T) {
	wifiConnected := true
	device := &fakeDevice{device: newFakeModem("line"), access: func(access ims.Access) (bool, bool, error) {
		return true, access != ims.AccessWiFiCalling || wifiConnected, nil
	}}
	sms := &fakeSMS{}
	exec := &Execution{internet: device, messages: sms}
	line := activeLine{device: device.device, identity: device.device.SIMIdentity()}
	to, body := "10086", "example"
	action := Action{Type: actionSMS, To: &to, Text: &body, WiFiCallingOnly: true}
	route, code := exec.waitReady(t.Context(), line, action)
	if route != call.RouteWiFiCalling || code != "" {
		t.Fatalf("ready = %q, %q", route, code)
	}
	wifiConnected = false
	if got := exec.sendSMS(t.Context(), line, action, route); got.Code != "wifi_calling_unavailable" || sms.count != 0 {
		t.Fatalf("send on disconnected Wi-Fi = %+v, %d submissions", got, sms.count)
	}
	wifiConnected = true
	if got := exec.sendSMS(t.Context(), line, action, route); got.Status != statusSucceeded || sms.count != 1 || device.smsAccess != ims.AccessWiFiCalling {
		t.Fatalf("send on Wi-Fi = %+v, %d submissions on %q", got, sms.count, device.smsAccess)
	}
	device.device.InvalidateSIMActivation()
	if got := exec.sendSMS(t.Context(), line, action, route); got.Code != "line_unavailable" || sms.count != 1 {
		t.Fatalf("send after SIM activation changed = %+v, %d submissions", got, sms.count)
	}
}
