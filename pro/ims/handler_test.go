//go:build ims

package ims

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

type fakeModemFinder struct {
	modem *mmodem.Modem
}

func (f fakeModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, nil
}

type updateCoordinatorProbe struct {
	Coordinator
	updated  bool
	settings Settings
	current  Settings
}

func (p *updateCoordinatorProbe) Settings(context.Context, *mmodem.Modem) (Settings, error) {
	return p.current, nil
}

func (p *updateCoordinatorProbe) UpdateSettings(_ context.Context, _ *mmodem.Modem, settings Settings) error {
	p.updated = true
	p.settings = settings
	return nil
}

func TestUpdateWiFiCallingSettingsUnderlay(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		current      Settings
		wantStatus   int
		wantSettings Settings
	}{
		{
			name:         "preserves omitted underlay",
			body:         `{"enabled":false}`,
			current:      Settings{Enabled: true, Underlay: UnderlaySettings{Mode: UnderlayModeSelf}},
			wantStatus:   http.StatusNoContent,
			wantSettings: Settings{Underlay: UnderlaySettings{Mode: UnderlayModeSelf}},
		},
		{
			name:         "selects another modem",
			body:         `{"enabled":true,"underlay":{"mode":"modem","modemId":"modem-2"}}`,
			wantStatus:   http.StatusNoContent,
			wantSettings: Settings{Enabled: true, Underlay: UnderlaySettings{Mode: UnderlayModeModem, ModemID: "modem-2"}},
		},
		{
			name:       "rejects missing modem id",
			body:       `{"enabled":true,"underlay":{"mode":"modem"}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &updateCoordinatorProbe{current: tt.current}
			h := &Handler{
				registry:    fakeModemFinder{modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"}},
				wifiCalling: probe,
			}
			e := echo.New()
			e.Validator = appvalidator.New()
			e.PUT("/modems/:id/wifi-calling/settings", h.UpdateSettings)
			req := httptest.NewRequest(http.MethodPut, "/modems/modem-1/wifi-calling/settings", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if probe.updated != (tt.wantStatus == http.StatusNoContent) {
				t.Fatalf("UpdateSettings called = %v", probe.updated)
			}
			if probe.settings != tt.wantSettings {
				t.Fatalf("UpdateSettings settings = %+v, want %+v", probe.settings, tt.wantSettings)
			}
		})
	}
}

func TestUpdateVoLTESettingsValidatesManagedDevice(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		portType     mmodem.ModemPortType
		device       *fakeManagedVoLTEDevice
		openErr      error
		wantStatus   int
		wantUpdated  bool
		wantSettings Settings
		wantOpened   bool
		wantCalls    []string
	}{
		{
			name:       "rejects unavailable device",
			body:       `{"enabled":true,"dataPath":"qmap"}`,
			portType:   mmodem.ModemPortTypeQmi,
			openErr:    wwan.ErrUnsupported,
			wantStatus: http.StatusBadRequest,
			wantOpened: true,
		},
		{
			name:         "accepts QMI data path",
			body:         `{"enabled":true,"dataPath":"qmap"}`,
			portType:     mmodem.ModemPortTypeQmi,
			device:       &fakeManagedVoLTEDevice{},
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: Settings{Enabled: true, DataPath: DataPathQMAP},
			wantOpened:   true,
			wantCalls:    []string{"status", "ims-profile", "packet-service"},
		},
		{
			name:         "accepts Qualcomm 410 data path",
			body:         `{"enabled":true,"dataPath":"qualcomm_410"}`,
			portType:     mmodem.ModemPortTypeQmi,
			device:       &fakeManagedVoLTEDevice{},
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: Settings{Enabled: true, DataPath: DataPathQualcomm410},
			wantOpened:   true,
			wantCalls:    []string{"status", "ims-profile", "packet-service"},
		},
		{
			name:         "disable skips validation",
			body:         `{"enabled":false,"dataPath":"legacy_bam_dmux"}`,
			portType:     mmodem.ModemPortTypeQmi,
			openErr:      wwan.ErrUnsupported,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: Settings{DataPath: DataPathLegacyBAMDMUX},
		},
		{
			name:       "rejects unsupported data path",
			body:       `{"enabled":false,"dataPath":"auto"}`,
			portType:   mmodem.ModemPortTypeQmi,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "requires data path for QMI",
			body:       `{"enabled":false}`,
			portType:   mmodem.ModemPortTypeQmi,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:         "derives MBIM data path when omitted",
			body:         `{"enabled":false}`,
			portType:     mmodem.ModemPortTypeMbim,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: Settings{DataPath: DataPathMBIM},
		},
		{
			name:         "ignores QMI data path selection for MBIM",
			body:         `{"enabled":false,"dataPath":"legacy_bam_dmux"}`,
			portType:     mmodem.ModemPortTypeMbim,
			wantStatus:   http.StatusNoContent,
			wantUpdated:  true,
			wantSettings: Settings{DataPath: DataPathMBIM},
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

			volte := &updateCoordinatorProbe{}
			modem := &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm0",
					PortType: tt.portType,
				}},
			}
			h := &Handler{
				registry: fakeModemFinder{modem: modem},
				volte:    volte,
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
		wifiCalling: wifiCalling,
	}
	e.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteSession)

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
