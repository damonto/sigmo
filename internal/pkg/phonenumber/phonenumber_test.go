package phonenumber

import (
	"errors"
	"testing"
)

func TestNormalizeForRegion(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		region  string
		want    string
		wantErr error
	}{
		{name: "china local mobile", value: "13800138000", region: "CN", want: "+8613800138000"},
		{name: "us local mobile", value: "6502530000", region: "US", want: "+16502530000"},
		{name: "uk local mobile", value: "07123456789", region: "GB", want: "+447123456789"},
		{name: "international is canonicalized", value: "+1 (650) 253-0000", region: "CN", want: "+16502530000"},
		{name: "short code remains local", value: "777", region: "NZ", want: "777"},
		{name: "long service code remains local", value: "10086", region: "CN", want: "10086"},
		{name: "invalid local number", value: "1234567", region: "CN", wantErr: ErrInvalid},
		{name: "unknown region rejects local number", value: "13800138000", region: "UN", wantErr: ErrInvalid},
		{name: "unknown region accepts international number", value: "+44 7123 456789", region: "UN", want: "+447123456789"},
		{name: "empty number", value: " ", region: "CN", wantErr: ErrRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeForRegion(tt.value, tt.region)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NormalizeForRegion() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeForRegion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeForRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}
