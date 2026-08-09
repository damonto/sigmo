//go:build ims

package ims

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

type fakeModemFinder struct {
	modem *mmodem.Modem
}

func (f fakeModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, nil
}

type voLTESettingsProbe struct {
	updated  bool
	settings VoLTESettings
}

type voLTEStatusReaderStub struct {
	status VoLTEStatus
	err    error
}

func (s voLTEStatusReaderStub) VoLTEStatus(context.Context, *mmodem.Modem) (VoLTEStatus, error) {
	return s.status, s.err
}

func (p *voLTESettingsProbe) UpdateVoLTESettings(_ context.Context, _ *mmodem.Modem, settings VoLTESettings) error {
	p.updated = true
	p.settings = settings
	return nil
}

type handlerConnectivityProbe struct {
	connectivityHTTP
	wifiUpdated  bool
	wifiSettings WiFiCallingSettings
	volte        *voLTESettingsProbe
	volteErr     error
}

func (p *handlerConnectivityProbe) ReplaceWiFiCallingSettings(_ context.Context, modem *mmodem.Modem, settings WiFiCallingSettings) error {
	settings, err := ResolveWiFiCallingSettings(modem, settings)
	if err != nil {
		return err
	}
	p.wifiUpdated = true
	p.wifiSettings = settings
	return nil
}

func (p *handlerConnectivityProbe) ReplaceVoLTESettings(ctx context.Context, modem *mmodem.Modem, settings VoLTESettings) error {
	if p.volteErr != nil {
		return p.volteErr
	}
	return updateVoLTESettings(ctx, modem, p.volte, settings)
}

func TestReadVoLTESettingsSkipsModemStatusInAirplaneMode(t *testing.T) {
	previousOpen := openManagedVoLTEDevice
	opened := false
	openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
		opened = true
		return &fakeManagedVoLTEDevice{}, nil
	}
	t.Cleanup(func() {
		openManagedVoLTEDevice = previousOpen
	})
	modem := qmiTestModem("modem-1")
	modem.Status.Power = wwanmodem.PowerStateLow
	want := VoLTESettingsResponse{
		Enabled:  true,
		State:    StateDisconnected,
		DataPath: DataPathQMAP,
	}

	got, err := ReadVoLTESettings(t.Context(), modem, voLTEStatusReaderStub{status: VoLTEStatus{
		VoLTESettings: VoLTESettings{Enabled: true, DataPath: DataPathQMAP},
		State:         StateDisconnected,
	}})
	if err != nil {
		t.Fatalf("ReadVoLTESettings() error = %v", err)
	}
	if got != want {
		t.Fatalf("ReadVoLTESettings() = %+v, want %+v", got, want)
	}
	if opened {
		t.Fatal("ReadVoLTESettings() queried modem IMS status in airplane mode")
	}
}

func TestReadVoLTEStatusTreatsInvalidOperationAsUnavailable(t *testing.T) {
	previousOpen := openManagedVoLTEDevice
	device := &fakeManagedVoLTEDevice{statusErr: qcom.QMIErrorInvalidOperation}
	openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
		return device, nil
	}
	t.Cleanup(func() {
		openManagedVoLTEDevice = previousOpen
	})

	got, err := readVoLTEStatus(t.Context(), qmiTestModem("modem-1"))
	if err != nil {
		t.Fatalf("readVoLTEStatus() error = %v", err)
	}
	if got != (wwan.VoLTEStatus{}) {
		t.Fatalf("readVoLTEStatus() = %+v, want zero status", got)
	}
	if !slices.Equal(device.calls, []string{"status"}) {
		t.Fatalf("device calls = %v, want [status]", device.calls)
	}
	if !device.closed {
		t.Fatal("readVoLTEStatus() did not close device")
	}
}

func TestUpdateWiFiCallingSettingsUnderlay(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantSettings WiFiCallingSettings
	}{
		{
			name:       "requires underlay for PUT",
			body:       `{"enabled":false}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:         "selects another modem",
			body:         `{"enabled":true,"underlay":{"mode":"modem","modemId":"modem-2"}}`,
			wantStatus:   http.StatusNoContent,
			wantSettings: WiFiCallingSettings{Enabled: true, Underlay: UnderlaySettings{Mode: UnderlayModeModem, ModemID: "modem-2"}},
		},
		{
			name:       "rejects missing modem id",
			body:       `{"enabled":true,"underlay":{"mode":"modem"}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &handlerConnectivityProbe{}
			h := &Handler{
				registry:     fakeModemFinder{modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"}},
				connectivity: probe,
			}
			e := echo.New()
			e.Validator = appvalidator.New()
			e.PUT("/modems/:id/wifi-calling/settings", h.UpdateWiFiCallingSettings)
			req := httptest.NewRequest(http.MethodPut, "/modems/modem-1/wifi-calling/settings", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if probe.wifiUpdated != (tt.wantStatus == http.StatusNoContent) {
				t.Fatalf("UpdateWiFiCallingSettings called = %v", probe.wifiUpdated)
			}
			if probe.wifiSettings != tt.wantSettings {
				t.Fatalf("UpdateWiFiCallingSettings settings = %+v, want %+v", probe.wifiSettings, tt.wantSettings)
			}
		})
	}
}

func TestUpdateVoLTESettingsValidatesManagedDevice(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		portType     wwanmodem.PortType
		device       *fakeManagedVoLTEDevice
		openErr      error
		wantStatus   int
		wantUpdated  bool
		wantSettings VoLTESettings
		wantOpened   bool
		wantCalls    []string
	}{
		{
			name:       "rejects unavailable device",
			body:       `{"enabled":true,"dataPath":"qmap"}`,
			portType:   wwanmodem.PortQMI,
			openErr:    wwan.ErrUnsupported,
			wantStatus: http.StatusBadRequest,
			wantOpened: true,
		},
		{
			name:         "accepts QMI data path",
			body:         `{"enabled":true,"dataPath":"qmap"}`,
			portType:     wwanmodem.PortQMI,
			device:       &fakeManagedVoLTEDevice{},
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: VoLTESettings{Enabled: true, DataPath: DataPathQMAP},
			wantOpened:   true,
			wantCalls:    []string{"status", "ims-profile", "packet-service"},
		},
		{
			name:         "accepts Qualcomm 410 data path",
			body:         `{"enabled":true,"dataPath":"qualcomm_410"}`,
			portType:     wwanmodem.PortQMI,
			device:       &fakeManagedVoLTEDevice{},
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: VoLTESettings{Enabled: true, DataPath: DataPathQualcomm410},
			wantOpened:   true,
			wantCalls:    []string{"status", "ims-profile", "packet-service"},
		},
		{
			name:         "disable skips validation",
			body:         `{"enabled":false,"dataPath":"legacy_bam_dmux"}`,
			portType:     wwanmodem.PortQMI,
			openErr:      wwan.ErrUnsupported,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: VoLTESettings{DataPath: DataPathLegacyBAMDMUX},
		},
		{
			name:       "rejects unsupported data path",
			body:       `{"enabled":false,"dataPath":"auto"}`,
			portType:   wwanmodem.PortQMI,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "requires data path for QMI",
			body:       `{"enabled":false}`,
			portType:   wwanmodem.PortQMI,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:         "derives MBIM data path when omitted",
			body:         `{"enabled":false}`,
			portType:     wwanmodem.PortMBIM,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: VoLTESettings{DataPath: DataPathMBIM},
		},
		{
			name:         "ignores QMI data path selection for MBIM",
			body:         `{"enabled":false,"dataPath":"legacy_bam_dmux"}`,
			portType:     wwanmodem.PortMBIM,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: VoLTESettings{DataPath: DataPathMBIM},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			opened := false
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				opened = true
				return tt.device, tt.openErr
			}
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
			})

			volte := &voLTESettingsProbe{}
			connectivity := &handlerConnectivityProbe{volte: volte}
			modem := &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm0",
					PortType: tt.portType,
				}},
			}
			h := &Handler{
				registry:     fakeModemFinder{modem: modem},
				connectivity: connectivity,
			}
			e := echo.New()
			e.Validator = appvalidator.New()
			e.PUT("/modems/:id/volte/settings", h.UpdateVoLTESettings)
			req := httptest.NewRequest(http.MethodPut, "/modems/modem-1/volte/settings", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if volte.updated != tt.wantUpdated {
				t.Fatalf("UpdateSettings called = %v, want %v", volte.updated, tt.wantUpdated)
			}
			if volte.settings != tt.wantSettings {
				t.Fatalf("UpdateSettings settings = %+v, want %+v", volte.settings, tt.wantSettings)
			}
			if opened != tt.wantOpened {
				t.Fatalf("openManagedVoLTEDevice called = %v, want %v", opened, tt.wantOpened)
			}
			var calls []string
			if tt.device != nil {
				calls = tt.device.calls
			}
			if !slices.Equal(calls, tt.wantCalls) {
				t.Fatalf("device calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestUpdateVoLTESettingsRejectsAirplaneMode(t *testing.T) {
	modem := qmiTestModem("modem-1")
	h := &Handler{
		registry: fakeModemFinder{modem: modem},
		connectivity: &handlerConnectivityProbe{
			volteErr: ErrVoLTEAirplaneMode,
		},
	}
	e := echo.New()
	e.PUT("/modems/:id/volte/settings", h.UpdateVoLTESettings)
	req := httptest.NewRequest(
		http.MethodPut,
		"/modems/modem-1/volte/settings",
		strings.NewReader(`{"enabled":false,"dataPath":"qmap"}`),
	)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestDeleteSessionRouteDisconnectsCurrentSession(t *testing.T) {
	cancelled := false
	wifiCalling := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				cancel: func() {
					cancelled = true
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	e := echo.New()
	h := &Handler{
		registry: fakeModemFinder{
			modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		},
		connectivity: &Connectivity{
			wifiCalling: wifiCalling,
			operations:  make(map[string]*sync.Mutex),
		},
	}
	e.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteWiFiCallingSession)

	req := httptest.NewRequest(http.MethodDelete, "/modems/modem-1/wifi-calling/sessions/current", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !cancelled {
		t.Fatal("session was not cancelled")
	}
	if _, ok := wifiCalling.sessions["modem-1"]; ok {
		t.Fatal("session was not removed")
	}

	req = httptest.NewRequest(http.MethodDelete, "/modems/modem-1/wifi-calling/sessions/current", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}
