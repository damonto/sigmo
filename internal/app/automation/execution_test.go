package automation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/esim"
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
func (*fakeDevice) MessageRoute() message.Route  { return nil }
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

func (f *fakeSMS) SendWithProfile(_ context.Context, device *mmodem.Modem, req message.SendRequest) (string, error) {
	f.pin = req.ExpectedProfileID
	f.count++
	if f.pin != device.SIMIdentity().ICCID {
		return "", message.ErrProfileChanged
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
