package modem

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/damonto/uicc-go/qualcomm/uim"
)

func TestQMISIMSlot(t *testing.T) {
	tests := []struct {
		name    string
		modem   *Modem
		want    uint8
		wantErr bool
	}{
		{name: "fallback to slot one", modem: &Modem{}, want: 1},
		{name: "use primary slot", modem: &Modem{PrimarySimSlot: 2}, want: 2},
		{name: "reject unsupported slot", modem: &Modem{PrimarySimSlot: 6}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qmiSIMSlot(tt.modem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("qmiSIMSlot() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("qmiSIMSlot() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQMIRepowerSimCard(t *testing.T) {
	errOpen := errors.New("proxy unavailable")
	errPowerOff := errors.New("power off rejected")
	errPowerOn := errors.New("power on rejected")
	tests := []struct {
		name      string
		modem     *Modem
		reader    *fakeQMIUIMReader
		openErr   error
		wantCalls []string
		wantErr   error
	}{
		{
			name:      "power cycles default slot",
			modem:     &Modem{PrimaryPort: "/dev/cdc-wdm0"},
			reader:    &fakeQMIUIMReader{},
			wantCalls: []string{"power-off:1", "power-on:1", "close"},
		},
		{
			name:      "power cycles primary slot",
			modem:     &Modem{PrimaryPort: "/dev/cdc-wdm0", PrimarySimSlot: 2},
			reader:    &fakeQMIUIMReader{},
			wantCalls: []string{"power-off:2", "power-on:2", "close"},
		},
		{
			name:      "returns open error",
			modem:     &Modem{PrimaryPort: "/dev/cdc-wdm0"},
			openErr:   errOpen,
			wantCalls: nil,
			wantErr:   errOpen,
		},
		{
			name:      "returns power off error",
			modem:     &Modem{PrimaryPort: "/dev/cdc-wdm0"},
			reader:    &fakeQMIUIMReader{powerOffErr: errPowerOff},
			wantCalls: []string{"power-off:1", "close"},
			wantErr:   errPowerOff,
		},
		{
			name:      "returns power on error",
			modem:     &Modem{PrimaryPort: "/dev/cdc-wdm0"},
			reader:    &fakeQMIUIMReader{powerOnErr: errPowerOn},
			wantCalls: []string{"power-off:1", "power-on:1", "close"},
			wantErr:   errPowerOn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withQMIUIMReader(t, tt.modem.PrimaryPort, qmiTestSlot(tt.modem), tt.reader, tt.openErr)

			err := qmiRepowerSimCard(context.Background(), tt.modem)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("qmiRepowerSimCard() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && err != nil {
				t.Fatalf("qmiRepowerSimCard() error = %v", err)
			}
			if tt.reader != nil && !slices.Equal(tt.reader.calls, tt.wantCalls) {
				t.Fatalf("reader calls = %v, want %v", tt.reader.calls, tt.wantCalls)
			}
		})
	}
}

func TestQMIActivateProvisioningIfSimMissing(t *testing.T) {
	aid := []byte{0xA0, 0x00, 0x00, 0x00, 0x87, 0x10, 0x02}
	errCardStatus := errors.New("card status rejected")
	errProvisioning := errors.New("session rejected")
	tests := []struct {
		name      string
		modem     *Modem
		reader    *fakeQMIUIMReader
		wantCalls []string
		wantReq   uim.ChangeProvisioningSessionRequest
		wantErr   error
		wantText  string
	}{
		{
			name: "skips ready usim application",
			modem: &Modem{
				PrimaryPort:    "/dev/cdc-wdm0",
				PrimarySimSlot: 1,
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateReady, aid),
			},
			wantCalls: []string{"card-status", "close"},
		},
		{
			name: "activates primary provisioning session",
			modem: &Modem{
				PrimaryPort:    "/dev/cdc-wdm0",
				PrimarySimSlot: 2,
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatusForSlot(2, uim.ApplicationStateReady, uim.PersonalizationStateInProgress, aid),
			},
			wantCalls: []string{"card-status", "change-provisioning:2", "close"},
			wantReq: uim.ChangeProvisioningSessionRequest{
				Session:  uim.SessionPrimaryGWProvisioning,
				Activate: true,
				Slot:     2,
				AID:      aid,
			},
		},
		{
			name: "returns card status error",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm0",
			},
			reader:    &fakeQMIUIMReader{cardStatusErr: errCardStatus},
			wantCalls: []string{"card-status", "close"},
			wantErr:   errCardStatus,
		},
		{
			name: "returns empty aid error",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm0",
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateInProgress, nil),
			},
			wantCalls: []string{"card-status", "close"},
			wantText:  "AID is empty",
		},
		{
			name: "returns provisioning error",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm0",
			},
			reader: &fakeQMIUIMReader{
				cardStatus:      qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateInProgress, aid),
				provisioningErr: errProvisioning,
			},
			wantCalls: []string{"card-status", "change-provisioning:1", "close"},
			wantErr:   errProvisioning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withQMIUIMReader(t, tt.modem.PrimaryPort, qmiTestSlot(tt.modem), tt.reader, nil)

			err := qmiActivateProvisioningIfSimMissing(context.Background(), tt.modem)
			if tt.wantErr != nil && err == nil {
				t.Fatalf("qmiActivateProvisioningIfSimMissing() error = nil, want %v", tt.wantErr)
			}
			if tt.wantText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantText)) {
				t.Fatalf("qmiActivateProvisioningIfSimMissing() error = %v, want text %q", err, tt.wantText)
			}
			if tt.wantErr == nil && tt.wantText == "" && err != nil {
				t.Fatalf("qmiActivateProvisioningIfSimMissing() error = %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("qmiActivateProvisioningIfSimMissing() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(tt.reader.calls, tt.wantCalls) {
				t.Fatalf("reader calls = %v, want %v", tt.reader.calls, tt.wantCalls)
			}
			if tt.wantReq.Slot != 0 && !qmiChangeProvisioningRequestEqual(tt.reader.changeReq, tt.wantReq) {
				t.Fatalf("ChangeProvisioningSession() request = %+v, want %+v", tt.reader.changeReq, tt.wantReq)
			}
		})
	}
}

type fakeQMIUIMReader struct {
	calls           []string
	powerOffErr     error
	powerOnErr      error
	cardStatus      uim.CardStatus
	cardStatusErr   error
	changeReq       uim.ChangeProvisioningSessionRequest
	provisioningErr error
}

func (r *fakeQMIUIMReader) PowerOffSIM(_ context.Context, slot uint8) error {
	r.calls = append(r.calls, fmtCall("power-off", slot))
	return r.powerOffErr
}

func (r *fakeQMIUIMReader) PowerOnSIM(_ context.Context, req uim.PowerOnSIMRequest) error {
	r.calls = append(r.calls, fmtCall("power-on", req.Slot))
	return r.powerOnErr
}

func (r *fakeQMIUIMReader) CardStatus(context.Context) (uim.CardStatus, error) {
	r.calls = append(r.calls, "card-status")
	return r.cardStatus, r.cardStatusErr
}

func (r *fakeQMIUIMReader) ChangeProvisioningSession(_ context.Context, req uim.ChangeProvisioningSessionRequest) error {
	r.calls = append(r.calls, fmtCall("change-provisioning", req.Slot))
	r.changeReq = req
	return r.provisioningErr
}

func (r *fakeQMIUIMReader) Close() error {
	r.calls = append(r.calls, "close")
	return nil
}

func withQMIUIMReader(t *testing.T, wantDevice string, wantSlot uint8, reader qmiUIMReader, openErr error) {
	t.Helper()

	old := openQMIUIMReader
	openQMIUIMReader = func(_ context.Context, device string, slot uint8) (qmiUIMReader, error) {
		if device != wantDevice {
			t.Fatalf("openQMIUIMReader() device = %q, want %q", device, wantDevice)
		}
		if slot != wantSlot {
			t.Fatalf("openQMIUIMReader() slot = %d, want %d", slot, wantSlot)
		}
		if openErr != nil {
			return nil, openErr
		}
		return reader, nil
	}
	t.Cleanup(func() {
		openQMIUIMReader = old
	})
}

func qmiTestSlot(m *Modem) uint8 {
	slot, err := qmiSIMSlot(m)
	if err != nil {
		return 0
	}
	return slot
}

func qmiTestCardStatus(appState uim.ApplicationState, personalizationState uim.PersonalizationState, aid []byte) uim.CardStatus {
	return qmiTestCardStatusForSlot(1, appState, personalizationState, aid)
}

func qmiTestCardStatusForSlot(slot uint8, appState uim.ApplicationState, personalizationState uim.PersonalizationState, aid []byte) uim.CardStatus {
	cards := make([]uim.Card, slot)
	cards[slot-1] = uim.Card{
		State: uim.CardStatePresent,
		Applications: []uim.CardApplication{{
			Type:                 uim.ApplicationTypeUSIM,
			State:                appState,
			PersonalizationState: personalizationState,
			AID:                  slices.Clone(aid),
		}},
	}
	return uim.CardStatus{Cards: cards}
}

func qmiChangeProvisioningRequestEqual(a, b uim.ChangeProvisioningSessionRequest) bool {
	return a.Session == b.Session &&
		a.Activate == b.Activate &&
		a.Slot == b.Slot &&
		slices.Equal(a.AID, b.AID)
}

func fmtCall(action string, slot uint8) string {
	return action + ":" + strconv.Itoa(int(slot))
}
