package modem

import (
	"context"
	"errors"
	"testing"

	uiccmbim "github.com/damonto/uicc-go/mbim"
	"github.com/damonto/uicc-go/qcom/uim"
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
			name: "normal UICC ATR",
			atr:  []byte{0x3B, 0x00},
			want: false,
		},
		{
			name: "T=15 without eUICC bit",
			atr:  []byte{0x3B, 0x80, 0x81, 0x2F, 0x80, 0xAE},
			want: false,
		},
		{
			name: "T=15 without removable UICC bit",
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
		name       string
		modem      *Modem
		status     uim.SlotStatus
		want       bool
		wantSlot   uint8
		wantOpened bool
	}{
		{
			name: "ATR marks eUICC",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
				},
			},
			want:       true,
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "known pSIM ATR westk",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{ATR: westkKnownATR},
				},
			},
			want:       true,
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "known pSIM ATR f002",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{ATR: f002KnownATR},
				},
			},
			want:       true,
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "known pSIM ATR 1601",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{ATR: one601KnownATR},
				},
			},
			want:       true,
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "empty ATR",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{},
				},
			},
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "ordinary UICC",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 1,
				Slots: []uim.Slot{
					{ATR: []byte{0x3B, 0x00}},
				},
			},
			wantSlot:   1,
			wantOpened: true,
		},
		{
			name: "uses active slot when primary is unknown",
			modem: testATRModem(ModemPortTypeQmi, ModemPort{
				PortType: ModemPortTypeQmi,
				Device:   "/dev/cdc-wdm0",
			}),
			status: uim.SlotStatus{
				ActiveSlot: 2,
				Slots: []uim.Slot{
					{ATR: []byte{0x3B, 0x00}},
					{ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
				},
			},
			want:       true,
			wantSlot:   1,
			wantOpened: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qmiReader := &fakeQMIUIMReader{slotStatus: tt.status}
			atrReader := testQMIATRReader(t, tt.modem.PrimaryPort, tt.wantSlot, qmiReader, nil)

			atr, err := atrReader.read(context.Background(), tt.modem)
			if err != nil {
				t.Fatalf("atrReader.read() error = %v", err)
			}
			got := atrSupportsEUICC(atr)
			if got != tt.want {
				t.Fatalf("atrSupportsEUICC(atrReader.read()) = %v, want %v", got, tt.want)
			}
			if tt.wantOpened && len(qmiReader.calls) == 0 {
				t.Fatal("QMI reader was not opened")
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
			mbimReader := &fakeMBIMATRReader{atr: tt.atr, atrErr: tt.atrErr}
			atrReader := testMBIMATRReader(mbimReader, tt.openErr)

			atr, err := atrReader.read(context.Background(), testATRModem(ModemPortTypeMbim, ModemPort{
				PortType: ModemPortTypeMbim,
				Device:   "/dev/cdc-wdm0",
			}))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("atrReader.read() error = %v, want %v", err, tt.wantErr)
			}
			got := atrSupportsEUICC(atr)
			if got != tt.want {
				t.Fatalf("atrSupportsEUICC(atrReader.read()) = %v, want %v", got, tt.want)
			}
			if tt.openErr == nil && !mbimReader.closed {
				t.Fatal("MBIM reader was not closed")
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

type fakeMBIMATRReader struct {
	atr    []byte
	atrErr error
	closed bool
}

func (r *fakeMBIMATRReader) QueryUiccATR(context.Context) ([]byte, error) {
	return r.atr, r.atrErr
}

func (r *fakeMBIMATRReader) Close() error {
	r.closed = true
	return nil
}

func testQMIATRReader(t *testing.T, wantDevice string, wantSlot uint8, reader qmiUIMReader, openErr error) atrReader {
	t.Helper()
	return atrReader{
		openQMI: func(_ context.Context, device string, slot uint8) (qmiUIMReader, error) {
			if device != wantDevice {
				t.Fatalf("open QMI device = %q, want %q", device, wantDevice)
			}
			if slot != wantSlot {
				t.Fatalf("open QMI slot = %d, want %d", slot, wantSlot)
			}
			if openErr != nil {
				return nil, openErr
			}
			return reader, nil
		},
	}
}

func testMBIMATRReader(reader mbimATRReader, openErr error) atrReader {
	return atrReader{
		openMBIM: func(context.Context, ...uiccmbim.Option) (mbimATRReader, error) {
			if openErr != nil {
				return nil, openErr
			}
			return reader, nil
		},
	}
}

func failOnATRTransports(t *testing.T) {
	t.Helper()

	oldQMI := openQMIUIMReader
	openQMIUIMReader = func(context.Context, string, uint8) (qmiUIMReader, error) {
		t.Fatal("openQMIUIMReader called")
		return nil, nil
	}
	t.Cleanup(func() {
		openQMIUIMReader = oldQMI
	})
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
