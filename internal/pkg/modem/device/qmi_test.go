package device

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/damonto/uicc-go/qcom"
	"github.com/damonto/uicc-go/qcom/uim"
)

func TestDeviceAirplaneModeQMI(t *testing.T) {
	tests := []struct {
		name string
		mode qcom.DMSOperatingMode
		want bool
	}{
		{name: "online", mode: qcom.DMSOperatingModeOnline},
		{name: "low power", mode: qcom.DMSOperatingModeLowPower, want: true},
		{name: "offline", mode: qcom.DMSOperatingModeOffline, want: true},
		{name: "persistent low power", mode: qcom.DMSOperatingModePersistentLowPower, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeQMIAirplaneModeReader{mode: tt.mode}
			device := qmiDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			got, err := device.AirplaneMode(context.Background())
			if err != nil {
				t.Fatalf("AirplaneMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("AirplaneMode() = %v, want %v", got, tt.want)
			}
			if !reader.closed {
				t.Fatal("QMI reader closed = false, want true")
			}
		})
	}
}

func TestDeviceSetAirplaneModeQMI(t *testing.T) {
	tests := []struct {
		name string
		want bool
		mode qcom.DMSOperatingMode
	}{
		{name: "enable", want: true, mode: qcom.DMSOperatingModeLowPower},
		{name: "disable", mode: qcom.DMSOperatingModeOnline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeQMIAirplaneModeReader{}
			device := qmiDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			if err := device.SetAirplaneMode(context.Background(), tt.want); err != nil {
				t.Fatalf("SetAirplaneMode() error = %v", err)
			}
			if reader.setMode != tt.mode {
				t.Fatalf("QMI set mode = %d, want %d", reader.setMode, tt.mode)
			}
			if !reader.closed {
				t.Fatal("QMI reader closed = false, want true")
			}
		})
	}
}

func TestDeviceToggleAirplaneModeQMI(t *testing.T) {
	tests := []struct {
		name     string
		mode     qcom.DMSOperatingMode
		want     bool
		wantMode qcom.DMSOperatingMode
	}{
		{name: "turn on", mode: qcom.DMSOperatingModeOnline, want: true, wantMode: qcom.DMSOperatingModeLowPower},
		{name: "turn off", mode: qcom.DMSOperatingModeLowPower, wantMode: qcom.DMSOperatingModeOnline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeQMIAirplaneModeReader{mode: tt.mode}
			device := qmiDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			got, err := device.ToggleAirplaneMode(context.Background())
			if err != nil {
				t.Fatalf("ToggleAirplaneMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToggleAirplaneMode() = %v, want %v", got, tt.want)
			}
			if reader.setMode != tt.wantMode {
				t.Fatalf("QMI set mode = %d, want %d", reader.setMode, tt.wantMode)
			}
			if !reader.closed {
				t.Fatal("QMI reader closed = false, want true")
			}
		})
	}
}

func TestOpenDeviceAirplaneModeUnsupported(t *testing.T) {
	modem := Config{}

	if _, err := Open(modem); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenDevice() error = %v, want unsupported", err)
	}
}

func TestDeviceSlot(t *testing.T) {
	tests := []struct {
		name    string
		slot    int
		want    uint8
		wantErr bool
	}{
		{name: "reject missing slot", wantErr: true},
		{name: "use configured slot", slot: 2, want: 2},
		{name: "reject unsupported slot", slot: 6, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSIMSlot(tt.slot)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeSIMSlot() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeSIMSlot() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQMIDevicePowerCycleSIM(t *testing.T) {
	oldDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldDelay
	})

	errOpen := errors.New("proxy unavailable")
	errPowerOff := errors.New("power off rejected")
	errPowerOn := errors.New("power on rejected")
	tests := []struct {
		name      string
		cfg       Config
		reader    *fakeQMIUIMReader
		openErr   error
		cancelCtx bool
		wantCalls []string
		wantErr   error
	}{
		{
			name:      "power cycles default slot",
			cfg:       testQMIConfig(1),
			reader:    &fakeQMIUIMReader{},
			wantCalls: []string{"power-off:1", "power-on:1", "close"},
		},
		{
			name:      "power cycles primary slot",
			cfg:       testQMIConfig(2),
			reader:    &fakeQMIUIMReader{},
			wantCalls: []string{"power-off:2", "power-on:2", "close"},
		},
		{
			name:      "returns open error",
			cfg:       testQMIConfig(1),
			openErr:   errOpen,
			wantCalls: nil,
			wantErr:   errOpen,
		},
		{
			name:      "returns power off error",
			cfg:       testQMIConfig(1),
			reader:    &fakeQMIUIMReader{powerOffErr: errPowerOff},
			wantCalls: []string{"power-off:1", "close"},
			wantErr:   errPowerOff,
		},
		{
			name:      "returns power on error",
			cfg:       testQMIConfig(1),
			reader:    &fakeQMIUIMReader{powerOnErr: errPowerOn},
			wantCalls: []string{"power-off:1", "power-on:1", "close"},
			wantErr:   errPowerOn,
		},
		{
			name:      "powers SIM back on after parent context is canceled",
			cfg:       testQMIConfig(1),
			reader:    &fakeQMIUIMReader{},
			cancelCtx: true,
			wantCalls: []string{"power-off:1", "power-on:1", "close"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot := qmiTestSlot(tt.cfg)
			ctx := context.Background()
			if tt.cancelCtx {
				cancelCtx, cancel := context.WithCancel(ctx)
				ctx = cancelCtx
				tt.reader.afterPowerOff = cancel
			}

			device := qmiDevice{
				device:  tt.cfg.Device,
				slot:    int(slot),
				imei:    tt.cfg.IMEI,
				openUIM: qmiUIMOpener(t, slot, tt.reader, tt.openErr),
			}
			err := device.PowerCycleSIM(ctx)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("PowerCycleSIM() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && err != nil {
				t.Fatalf("PowerCycleSIM() error = %v", err)
			}
			if tt.reader != nil && !slices.Equal(tt.reader.calls, tt.wantCalls) {
				t.Fatalf("reader calls = %v, want %v", tt.reader.calls, tt.wantCalls)
			}
		})
	}
}

func TestQMIDeviceActivateProvisioningIfSIMMissing(t *testing.T) {
	aid := []byte{0xA0, 0x00, 0x00, 0x00, 0x87, 0x10, 0x02}
	errCardStatus := errors.New("card status rejected")
	errProvisioning := errors.New("session rejected")
	tests := []struct {
		name      string
		cfg       Config
		reader    *fakeQMIUIMReader
		wantCalls []string
		wantReq   uim.ChangeProvisioningSessionRequest
		wantErr   error
		wantText  string
	}{
		{
			name: "skips ready usim application",
			cfg:  testQMIConfig(1),
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateReady, aid),
			},
			wantCalls: []string{"card-status", "close"},
		},
		{
			name: "activates primary provisioning session",
			cfg:  testQMIConfig(2),
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatusForSlot(2, uim.ApplicationStateReady, uim.PersonalizationStateInProgress, aid),
			},
			wantCalls: []string{"card-status", "close", "change-provisioning:2", "close"},
			wantReq: uim.ChangeProvisioningSessionRequest{
				Session:  uim.SessionPrimaryGWProvisioning,
				Activate: true,
				Slot:     2,
				AID:      aid,
			},
		},
		{
			name:      "returns card status error",
			cfg:       testQMIConfig(1),
			reader:    &fakeQMIUIMReader{cardStatusErr: errCardStatus},
			wantCalls: []string{"card-status", "close"},
			wantErr:   errCardStatus,
		},
		{
			name: "returns empty aid error",
			cfg:  testQMIConfig(1),
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateInProgress, nil),
			},
			wantCalls: []string{"card-status", "close"},
			wantText:  "AID is empty",
		},
		{
			name: "returns provisioning error",
			cfg:  testQMIConfig(1),
			reader: &fakeQMIUIMReader{
				cardStatus:      qmiTestCardStatus(uim.ApplicationStateReady, uim.PersonalizationStateInProgress, aid),
				provisioningErr: errProvisioning,
			},
			wantCalls: []string{"card-status", "close", "change-provisioning:1", "close"},
			wantErr:   errProvisioning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot := qmiTestSlot(tt.cfg)
			device := qmiDevice{
				device:  tt.cfg.Device,
				slot:    int(slot),
				imei:    tt.cfg.IMEI,
				openUIM: qmiUIMOpener(t, slot, tt.reader, nil),
			}
			err := device.ActivateProvisioningIfSIMMissing(context.Background())
			if tt.wantErr != nil && err == nil {
				t.Fatalf("ActivateProvisioningIfSIMMissing() error = nil, want %v", tt.wantErr)
			}
			if tt.wantText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantText)) {
				t.Fatalf("ActivateProvisioningIfSIMMissing() error = %v, want text %q", err, tt.wantText)
			}
			if tt.wantErr == nil && tt.wantText == "" && err != nil {
				t.Fatalf("ActivateProvisioningIfSIMMissing() error = %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ActivateProvisioningIfSIMMissing() error = %v, want %v", err, tt.wantErr)
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

func TestDecodeQMIICCID(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		want    string
		wantErr bool
	}{
		{
			name: "swapped bcd",
			raw:  []byte{0x98, 0x68, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0},
			want: "8986000000000000000",
		},
		{
			name:    "invalid bcd",
			raw:     []byte{0x9A},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeQMIICCID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("decodeQMIICCID() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeQMIICCID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("decodeQMIICCID() = %q, want %q", got, tt.want)
			}
		})
	}
}

type fakeQMIUIMReader struct {
	calls           []string
	atr             []byte
	atrErr          error
	powerOffErr     error
	afterPowerOff   func()
	powerOnErr      error
	slotStatus      uim.SlotStatus
	slotStatusErr   error
	cardStatus      uim.CardStatus
	cardStatusErr   error
	changeReq       uim.ChangeProvisioningSessionRequest
	provisioningErr error
}

func (r *fakeQMIUIMReader) ATR(context.Context) ([]byte, error) {
	r.calls = append(r.calls, "atr")
	return slices.Clone(r.atr), r.atrErr
}

func (r *fakeQMIUIMReader) PowerOffSIM(_ context.Context, slot uint8) error {
	r.calls = append(r.calls, fmtCall("power-off", slot))
	if r.afterPowerOff != nil {
		r.afterPowerOff()
	}
	return r.powerOffErr
}

func (r *fakeQMIUIMReader) PowerOnSIM(_ context.Context, req uim.PowerOnSIMRequest) error {
	r.calls = append(r.calls, fmtCall("power-on", req.Slot))
	return r.powerOnErr
}

func (r *fakeQMIUIMReader) SlotStatus(context.Context) (uim.SlotStatus, error) {
	r.calls = append(r.calls, "slot-status")
	return r.slotStatus, r.slotStatusErr
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

func qmiUIMOpener(t *testing.T, wantSlot uint8, reader qmiUIMReader, openErr error) func(context.Context, uint8) (qmiUIMReader, error) {
	t.Helper()

	return func(_ context.Context, slot uint8) (qmiUIMReader, error) {
		if slot != wantSlot {
			t.Fatalf("open QMI UIM slot = %d, want %d", slot, wantSlot)
		}
		if openErr != nil {
			return nil, openErr
		}
		return reader, nil
	}
}

type fakeQMIAirplaneModeReader struct {
	mode     qcom.DMSOperatingMode
	setMode  qcom.DMSOperatingMode
	setCalls int
	closed   bool
}

func (r *fakeQMIAirplaneModeReader) OperatingMode(context.Context) (qcom.DMSOperatingMode, error) {
	return r.mode, nil
}

func (r *fakeQMIAirplaneModeReader) SetOperatingMode(_ context.Context, mode qcom.DMSOperatingMode) error {
	r.setMode = mode
	r.setCalls++
	return nil
}

func (r *fakeQMIAirplaneModeReader) Close() error {
	r.closed = true
	return nil
}

func qmiDeviceWithAirplaneModeReader(t *testing.T, wantDevice string, reader qmiAirplaneModeReader, openErr error) qmiDevice {
	t.Helper()

	return qmiDevice{
		device: wantDevice,
		slot:   1,
		imei:   "modem-1",
		openRadio: func(context.Context) (qmiAirplaneModeReader, error) {
			return reader, openErr
		},
	}
}

func mustOpenDevice(t *testing.T, cfg Config) *Device {
	t.Helper()
	device, err := Open(cfg)
	if err != nil {
		t.Fatalf("OpenDevice() error = %v", err)
	}
	return device
}

func testDeviceConfig(portType PortType) Config {
	return Config{
		PortType: portType,
		Device:   "/dev/cdc-wdm0",
		Slot:     1,
		IMEI:     "modem-1",
	}
}

func qmiTestSlot(cfg Config) uint8 {
	slot, err := normalizeSIMSlot(cfg.Slot)
	if err != nil {
		return 0
	}
	return slot
}

func testQMIConfig(slot int) Config {
	return Config{PortType: PortTypeQMI, Device: "/dev/cdc-wdm0", Slot: slot, IMEI: "imei-1"}
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
