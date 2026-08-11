package modem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type internetStatusStub struct {
	connection *internet.Connection
	err        error
}

func (s internetStatusStub) Current(context.Context, *mmodem.Modem) (*internet.Connection, error) {
	return s.connection, s.err
}

func TestCatalogBuildListResponseKeepsDiscoveredModems(t *testing.T) {
	euiccATR := []byte{0x3B, 0x9F, 0x96, 0x80, 0x3F, 0xC7, 0x82, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x15, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0x63}
	tests := []struct {
		name    string
		devices []*mmodem.Modem
		wantIDs []string
	}{
		{
			name: "keeps enabled modem when primary SIM query is unavailable",
			devices: []*mmodem.Modem{
				{
					EquipmentIdentifier: "bad-modem",
					Model:               "No SIM",
					Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn},
				},
				{
					EquipmentIdentifier: "good-modem",
					Model:               "Locked",
					Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn, SIM: wwanmodem.SIMStateLocked},
					SIM:                 &mmodem.SIM{ATR: euiccATR},
				},
			},
			wantIDs: []string{"bad-modem", "good-modem"},
		},
		{
			name: "keeps list populated when every live query is unavailable",
			devices: []*mmodem.Modem{
				{
					EquipmentIdentifier: "bad-modem",
					Model:               "No SIM",
					Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn},
				},
			},
			wantIDs: []string{"bad-modem"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)

			got, err := catalog.buildListResponse(t.Context(), tt.devices)
			if err != nil {
				t.Fatalf("buildListResponse() error = %v", err)
			}

			gotIDs := make([]string, 0, len(got))
			for _, modem := range got {
				gotIDs = append(gotIDs, modem.ID)
			}
			if !slices.Equal(gotIDs, tt.wantIDs) {
				t.Fatalf("modem IDs = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestCatalogBuildListResponseKeepsSearchingModemWhenLiveQueriesFail(t *testing.T) {
	catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
	device := &mmodem.Modem{
		EquipmentIdentifier: "866069053507297",
		Manufacturer:        "Qualcomm",
		Model:               "Searching modem",
		Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn, Registration: wwanmodem.RegistrationSearching},
		SIM: &mmodem.SIM{
			Active:             true,
			Identifier:         "8944000000000000000",
			OperatorIdentifier: "23433",
			OperatorName:       "EE",
		},
	}

	got, err := catalog.buildListResponse(t.Context(), []*mmodem.Modem{device})
	if err != nil {
		t.Fatalf("buildListResponse() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("modem count = %d, want 1", len(got))
	}
	if got[0].ID != device.EquipmentIdentifier {
		t.Fatalf("modem ID = %q, want %q", got[0].ID, device.EquipmentIdentifier)
	}
	if got[0].State != "searching" {
		t.Fatalf("state = %q, want searching", got[0].State)
	}
	if got[0].RegistrationState != registrationStateName(wwanmodem.RegistrationSearching) {
		t.Fatalf("registration state = %q, want %q", got[0].RegistrationState, registrationStateName(wwanmodem.RegistrationSearching))
	}
	if got[0].SIM.Identifier != device.SIM.Identifier {
		t.Fatalf("SIM identifier = %q, want %q", got[0].SIM.Identifier, device.SIM.Identifier)
	}
}

func TestCatalogBuildBasicResponseIncludesPrimaryPort(t *testing.T) {
	tests := []struct {
		name   string
		device *mmodem.Modem
	}{
		{
			name: "QMI control device",
			device: &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				PrimaryPort:         "/dev/cdc-wdm0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			got := catalog.buildBasicResponse(tt.device)
			if got.PrimaryPort != tt.device.PrimaryPort {
				t.Fatalf("primary port = %q, want %q", got.PrimaryPort, tt.device.PrimaryPort)
			}
			if got.SIMKind != mmodem.SIMKindUnknown {
				t.Fatalf("SIM kind = %q, want %q", got.SIMKind, mmodem.SIMKindUnknown)
			}
		})
	}
}

func TestCatalogBuildResponseLockedModem(t *testing.T) {
	tests := []struct {
		name            string
		wantSupported   bool
		wantUnlockLabel string
	}{
		{name: "supports sim pin unlock", wantSupported: true, wantUnlockLabel: "sim-pin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			device := &mmodem.Modem{
				EquipmentIdentifier: "860588043408833",
				PrimaryPort:         "/dev/cdc-wdm0",
				Manufacturer:        "Quectel",
				Model:               "RM520N",
				Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn, SIM: wwanmodem.SIMStateLocked},
				SIM:                 &mmodem.SIM{Slot: 1, Active: true, ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
			}

			got, err := catalog.buildResponse(t.Context(), device)
			if err != nil {
				t.Fatalf("buildResponse() error = %v", err)
			}
			if got.State != "locked" {
				t.Fatalf("state = %q, want locked", got.State)
			}
			if got.UnlockRequired != tt.wantUnlockLabel {
				t.Fatalf("unlockRequired = %q, want %q", got.UnlockRequired, tt.wantUnlockLabel)
			}
			if got.UnlockSupported != tt.wantSupported {
				t.Fatalf("unlockSupported = %v, want %v", got.UnlockSupported, tt.wantSupported)
			}
			if got.PrimaryPort != device.PrimaryPort {
				t.Fatalf("primary port = %q, want %q", got.PrimaryPort, device.PrimaryPort)
			}
			if got.SIMKind != mmodem.SIMKindEUICC {
				t.Fatalf("SIM kind = %q, want %q", got.SIMKind, mmodem.SIMKindEUICC)
			}
		})
	}
}

func TestCatalogBuildResponseUsesCachedSIMWithoutTransport(t *testing.T) {
	catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
	device := &mmodem.Modem{
		EquipmentIdentifier: "866069053507297",
		PrimaryPort:         "/dev/cdc-wdm0",
		Manufacturer:        "Qualcomm",
		Model:               "Cached modem",
		Status:              wwanmodem.Status{Power: wwanmodem.PowerStateOn, Registration: wwanmodem.RegistrationHome},
		PrimarySIMSlot:      1,
		SIMSlots:            []uint32{1},
		SIM: &mmodem.SIM{
			Slot:               1,
			Active:             true,
			Identifier:         "8944000000000000000",
			ATR:                []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
			OperatorIdentifier: "23433",
			OperatorName:       "EE",
		},
	}

	got, err := catalog.buildResponse(t.Context(), device)
	if err != nil {
		t.Fatalf("buildResponse() error = %v", err)
	}
	if got.SIM.Identifier != device.SIM.Identifier {
		t.Fatalf("primary SIM identifier = %q, want %q", got.SIM.Identifier, device.SIM.Identifier)
	}
	if len(got.Slots) != 1 || got.Slots[0].Slot != 1 || got.Slots[0].Identifier != device.SIM.Identifier {
		t.Fatalf("slots = %+v, want cached primary SIM", got.Slots)
	}
	if got.PrimaryPort != device.PrimaryPort {
		t.Fatalf("primary port = %q, want %q", got.PrimaryPort, device.PrimaryPort)
	}
	if got.SIMKind != mmodem.SIMKindEUICC {
		t.Fatalf("SIM kind = %q, want %q", got.SIMKind, mmodem.SIMKindEUICC)
	}
}

func TestCatalogApplyOverviewExtensions(t *testing.T) {
	errStatus := errors.New("status source")
	tests := []struct {
		name              string
		extensions        []modemstatus.Extension
		wantWiFiEnabled   bool
		wantWiFiConnected bool
		wantErr           error
	}{
		{
			name: "fills wifi calling fields",
			extensions: []modemstatus.Extension{
				func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
					fields.WiFiCallingEnabled = true
					fields.WiFiCallingConnected = true
					return nil
				},
			},
			wantWiFiEnabled:   true,
			wantWiFiConnected: true,
		},
		{
			name: "skips nil extension",
			extensions: []modemstatus.Extension{
				nil,
			},
		},
		{
			name: "wraps extension error",
			extensions: []modemstatus.Extension{
				func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
					return errStatus
				},
			},
			wantErr: errStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil, tt.extensions...)
			resp := &ModemResponse{}

			err := catalog.applyOverviewExtensions(t.Context(), &mmodem.Modem{}, resp)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("applyOverviewExtensions() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyOverviewExtensions() error = %v", err)
			}
			if resp.WiFiCallingEnabled != tt.wantWiFiEnabled {
				t.Fatalf("WiFiCallingEnabled = %v, want %v", resp.WiFiCallingEnabled, tt.wantWiFiEnabled)
			}
			if resp.WiFiCallingConnected != tt.wantWiFiConnected {
				t.Fatalf("WiFiCallingConnected = %v, want %v", resp.WiFiCallingConnected, tt.wantWiFiConnected)
			}
		})
	}
}

func TestCatalogApplyInternetStatus(t *testing.T) {
	errStatus := errors.New("status source")
	tests := []struct {
		name     string
		internet internetStatusReader
		want     bool
	}{
		{
			name: "connected",
			internet: internetStatusStub{
				connection: &internet.Connection{Status: internet.StatusConnected},
			},
			want: true,
		},
		{
			name: "disconnected",
			internet: internetStatusStub{
				connection: &internet.Connection{Status: internet.StatusDisconnected},
			},
		},
		{name: "status unavailable", internet: internetStatusStub{err: errStatus}},
		{name: "connector unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := &catalog{internet: tt.internet}
			resp := &ModemResponse{}

			catalog.applyInternetStatus(t.Context(), &mmodem.Modem{EquipmentIdentifier: "modem-1"},
				resp,
			)

			if resp.InternetConnected != tt.want {
				t.Fatalf("InternetConnected = %v, want %v", resp.InternetConnected, tt.want)
			}
		})
	}
}

func TestModemResponseJSONIncludesOverviewFields(t *testing.T) {
	tests := []struct {
		name string
		resp ModemResponse
		want string
	}{
		{
			name: "internet connected",
			resp: ModemResponse{
				Fields: modemstatus.Fields{InternetConnected: true},
			},
			want: `"internetConnected":true`,
		},
		{
			name: "wifi calling connected",
			resp: ModemResponse{
				Fields: modemstatus.Fields{
					WiFiCallingEnabled:   true,
					WiFiCallingConnected: true,
				},
			},
			want: `"wifiCallingConnected":true`,
		},
		{
			name: "primary port",
			resp: ModemResponse{PrimaryPort: "/dev/cdc-wdm0"},
			want: `"primaryPort":"/dev/cdc-wdm0"`,
		},
		{
			name: "SIM kind",
			resp: ModemResponse{SIMKind: mmodem.SIMKindUnknown},
			want: `"simKind":"unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Fatalf("Marshal() = %s, want field %s", got, tt.want)
			}
		})
	}
}

func TestModemResponseJSONExcludesLegacyESIMFlag(t *testing.T) {
	encoded, err := json.Marshal(ModemResponse{SIMKind: mmodem.SIMKindEUICC})
	if err != nil {
		t.Fatalf("marshal ModemResponse: %v", err)
	}
	if bytes.Contains(encoded, []byte(`"supportsEsim"`)) {
		t.Fatalf("ModemResponse JSON includes legacy supportsEsim field: %s", encoded)
	}
}
