package modem

import "testing"

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

func TestModemSnapshotSIMKind(t *testing.T) {
	tests := []struct {
		name     string
		modem    *Modem
		wantKind SIMKind
	}{
		{
			name:     "cached eUICC ATR",
			modem:    &Modem{Sim: &SIM{ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}}},
			wantKind: SIMKindEUICC,
		},
		{
			name:     "cached known ESTKme ATR",
			modem:    &Modem{Sim: &SIM{ATR: westkKnownATR}},
			wantKind: SIMKindEUICC,
		},
		{
			name:     "ordinary cached ATR",
			modem:    &Modem{Sim: &SIM{ATR: []byte{0x3B, 0x00}}},
			wantKind: SIMKindPhysical,
		},
		{
			name:     "missing ATR",
			modem:    &Modem{Sim: &SIM{}},
			wantKind: SIMKindUnknown,
		},
		{
			name:     "missing SIM",
			modem:    &Modem{},
			wantKind: SIMKindUnknown,
		},
		{
			name:     "nil modem",
			wantKind: SIMKindUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.modem.Snapshot().SIMKind(); got != tt.wantKind {
				t.Fatalf("ModemSnapshot.SIMKind() = %q, want %q", got, tt.wantKind)
			}
		})
	}
}
