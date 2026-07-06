package modem

import (
	"context"
	"errors"
	"slices"
	"testing"
)

var errFakeMBIMATR = errors.New("mbim atr")

var (
	westkKnownATR  = []byte{0x3B, 0xBF, 0x93, 0x00, 0x80, 0x1F, 0xC6, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x13, 0x57, 0x65, 0x73, 0x74, 0x6B, 0x2E, 0x6D, 0x65, 0xE3}
	f002KnownATR   = []byte{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0xF0, 0x02, 0x00, 0x02, 0x5C}
	one601KnownATR = []byte{0x3B, 0x9F, 0x96, 0x80, 0x1F, 0xC7, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x1B, 0x57, 0xAA, 0x86, 0x60, 0x16, 0x01, 0x00, 0x01, 0xBA}
)

func TestATRSupportsEUICC(t *testing.T) {
	tests := []struct {
		name string
		atr  []byte
		want bool
	}{
		{
			name: "eUICC global interface byte",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
			want: true,
		},
		{
			name: "TS 102 221 eUICC ATR",
			atr:  []byte{0x3B, 0x97, 0x93, 0x80, 0x3F, 0xC7, 0x82, 0x80, 0x31, 0xE0, 0x73, 0xFE, 0x21, 0x13, 0x10},
			want: true,
		},
		{
			name: "known pSIM ATR westk",
			atr:  westkKnownATR,
			want: true,
		},
		{
			name: "known pSIM ATR f002",
			atr:  f002KnownATR,
			want: true,
		},
		{
			name: "known pSIM ATR 1601",
			atr:  one601KnownATR,
			want: true,
		},
		{
			name: "normal Device ATR",
			atr:  []byte{0x3B, 0x00},
			want: false,
		},
		{
			name: "T=15 without eUICC bit",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x80, 0xAE},
			want: false,
		},
		{
			name: "T=15 without removable Device bit",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x02, 0x2C},
			want: false,
		},
		{
			name: "bad checksum",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0x00},
			want: false,
		},
		{
			name: "TD1 T=15 is invalid for eUICC marker",
			atr:  []byte{0x3B, 0x80, 0x1F, 0x20, 0x82, 0x3D},
			want: false,
		},
		{
			name: "empty ATR",
			atr:  nil,
			want: false,
		},
		{
			name: "bad convention",
			atr:  []byte{0x00, 0x00},
			want: false,
		},
		{
			name: "truncated interface byte",
			atr:  []byte{0x3B, 0x80},
			want: false,
		},
		{
			name: "truncated historical bytes",
			atr:  []byte{0x3B, 0x02, 0x80},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atrSupportsEUICC(tt.atr); got != tt.want {
				t.Fatalf("atrSupportsEUICC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportsEUICCUsesCachedATR(t *testing.T) {
	tests := []struct {
		name string
		atr  []byte
		want bool
	}{
		{name: "cached eUICC ATR", atr: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}, want: true},
		{name: "cached known ESTKme ATR", atr: westkKnownATR, want: true},
		{name: "ordinary cached ATR", atr: []byte{0x3B, 0x00}},
		{name: "missing ATR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failOnATRTransports(t)
			modem := testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			})
			modem.Sim = &SIM{ATR: tt.atr}
			got, err := SupportsEUICC(modem)
			if err != nil {
				t.Fatalf("SupportsEUICC() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SupportsEUICC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestATRReaderReadQMI(t *testing.T) {
	tests := []struct {
		name     string
		modem    *Modem
		atr      []byte
		atrErr   error
		want     bool
		wantErr  error
		wantSlot uint8
	}{
		{
			name: "ATR marks eUICC",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
			want:     true,
			wantSlot: 1,
		},
		{
			name: "known pSIM ATR westk",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      westkKnownATR,
			want:     true,
			wantSlot: 1,
		},
		{
			name: "known pSIM ATR f002",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      f002KnownATR,
			want:     true,
			wantSlot: 1,
		},
		{
			name: "known pSIM ATR 1601",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      one601KnownATR,
			want:     true,
			wantSlot: 1,
		},
		{
			name: "empty ATR",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			wantSlot: 1,
		},
		{
			name: "ordinary UICC",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      []byte{0x3B, 0x00},
			wantSlot: 1,
		},
		{
			name: "primary slot unknown uses slot one",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atr:      []byte{0x3B, 0x00},
			wantSlot: 1,
		},
		{
			name: "ATR read error",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			atrErr:   errFakeMBIMATR,
			wantErr:  errFakeMBIMATR,
			wantSlot: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uicc := &fakeATRDevice{atr: tt.atr, atrErr: tt.atrErr}
			atr, err := readDeviceATR(context.Background(), tt.modem, testDeviceATROpener(uicc, nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("readDeviceATR() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(uicc.calls, []string{"atr"}) {
				t.Fatalf("device calls = %v, want [atr]", uicc.calls)
			}
			if tt.wantErr != nil {
				return
			}
			got := atrSupportsEUICC(atr)
			if got != tt.want {
				t.Fatalf("atrSupportsEUICC(readDeviceATR()) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestATRReaderReadMBIM(t *testing.T) {
	tests := []struct {
		name    string
		atr     []byte
		openErr error
		atrErr  error
		want    bool
		wantErr error
	}{
		{
			name: "ATR marks eUICC",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
			want: true,
		},
		{
			name: "known pSIM ATR westk",
			atr:  westkKnownATR,
			want: true,
		},
		{
			name: "known pSIM ATR f002",
			atr:  f002KnownATR,
			want: true,
		},
		{
			name: "known pSIM ATR 1601",
			atr:  one601KnownATR,
			want: true,
		},
		{
			name: "ordinary UICC",
			atr:  []byte{0x3B, 0x00},
		},
		{
			name:    "open error",
			openErr: errFakeMBIMATR,
			wantErr: errFakeMBIMATR,
		},
		{
			name:    "ATR query error",
			atrErr:  errFakeMBIMATR,
			wantErr: errFakeMBIMATR,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uicc := &fakeATRDevice{atr: tt.atr, atrErr: tt.atrErr}
			atr, err := readDeviceATR(context.Background(), testATRModem(ModemPortTypeMbim, ModemPort{
				PortType: ModemPortTypeMbim,
				Device:   "/dev/cdc-wdm0",
			}), testDeviceATROpener(uicc, tt.openErr))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("readDeviceATR() error = %v, want %v", err, tt.wantErr)
			}
			got := atrSupportsEUICC(atr)
			if got != tt.want {
				t.Fatalf("atrSupportsEUICC(readDeviceATR()) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportsEUICCDoesNotProbeATOrUnknown(t *testing.T) {
	tests := []struct {
		name  string
		modem *Modem
	}{
		{
			name: "AT port",
			modem: testATRModem(ModemPortTypeAt, ModemPort{
				PortType: ModemPortTypeAt,
				Device:   "/dev/ttyUSB2",
			}),
		},
		{
			name:  "unknown port",
			modem: &Modem{PrimaryPort: "/dev/ttyUSB2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failOnATRTransports(t)
			got, err := SupportsEUICC(tt.modem)
			if err != nil {
				t.Fatalf("SupportsEUICC() error = %v", err)
			}
			if got {
				t.Fatal("SupportsEUICC() = true, want false")
			}
		})
	}
}

type fakeATRDevice struct {
	atr    []byte
	atrErr error
	calls  []string
}

func (u *fakeATRDevice) ATR(context.Context) ([]byte, error) {
	u.calls = append(u.calls, "atr")
	return slices.Clone(u.atr), u.atrErr
}

func testDeviceATROpener(uicc deviceATRReader, openErr error) deviceATROpener {
	return func(*Modem) (deviceATRReader, error) {
		if openErr != nil {
			return nil, openErr
		}
		return uicc, nil
	}
}

func failOnATRTransports(t *testing.T) {
	t.Helper()
}

func testATRModem(portType ModemPortType, port ModemPort) *Modem {
	return &Modem{
		EquipmentIdentifier: "test-imei",
		PrimaryPort:         port.Device,
		Ports: []ModemPort{
			{
				PortType: portType,
				Device:   port.Device,
			},
		},
	}
}
