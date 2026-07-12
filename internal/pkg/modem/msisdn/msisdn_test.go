package msisdn

import (
	"bytes"
	"errors"
	"testing"
)

func TestRecordLen(t *testing.T) {
	tests := []struct {
		name    string
		selects []byte
		err     error
		want    int
		wantErr bool
	}{
		{
			name:    "read record length",
			selects: []byte{0x62, 0x07, 0x82, 0x05, 0x01, 0x02, 0x00, 0x20, 0x01},
			want:    32,
		},
		{
			name:    "return select error",
			err:     errors.New("select failed"),
			wantErr: true,
		},
		{
			name:    "short response",
			selects: []byte{0x62},
			wantErr: true,
		},
		{
			name:    "truncated tag",
			selects: []byte{0x62, 0x04, 0x82, 0x05, 0x01},
			wantErr: true,
		},
		{
			name:    "missing file descriptor tag",
			selects: []byte{0x62, 0x03, 0x83, 0x01, 0x01},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MSISDN{runner: fakeRunner{selects: tt.selects, err: tt.err}}
			got, err := m.recordLen()
			if (err != nil) != tt.wantErr {
				t.Fatalf("recordLen() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("recordLen() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEncodeBCD(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []byte
		wantErr bool
	}{
		{name: "even digits", value: "1234", want: []byte{0x21, 0x43}},
		{name: "odd digits", value: "123", want: []byte{0x21, 0xF3}},
		{name: "invalid digit", value: "12x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&MSISDN{}).encodeBCD(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("encodeBCD() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("encodeBCD() = %X, want %X", got, tt.want)
			}
		})
	}
}

func TestEncodeRecord(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		length  int
		wantTON byte
		wantBCD []byte
		wantErr bool
	}{
		{name: "international odd digits", number: "+12345", length: 32, wantTON: 0x91, wantBCD: []byte{0x21, 0x43, 0xF5}},
		{name: "national even digits", number: "1234", length: 32, wantTON: 0x81, wantBCD: []byte{0x21, 0x43}},
		{name: "short record", number: "1", length: 13, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeRecord("", tt.number, tt.length)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EncodeRecord() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			offset := tt.length - 14
			if got[offset+1] != tt.wantTON || !bytes.Equal(got[offset+2:offset+2+len(tt.wantBCD)], tt.wantBCD) {
				t.Fatalf("EncodeRecord() = % X", got)
			}
		})
	}
}

type fakeRunner struct {
	selects []byte
	err     error
}

func (f fakeRunner) Run([]byte) error {
	return nil
}

func (f fakeRunner) Select() ([]byte, error) {
	return f.selects, f.err
}
