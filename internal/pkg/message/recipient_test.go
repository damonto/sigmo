package message

import (
	"errors"
	"testing"
)

func TestNormalizeRecipientForRegion(t *testing.T) {
	tests := []struct {
		name    string
		to      string
		region  string
		want    string
		wantErr error
	}{
		{
			name:   "china local mobile",
			to:     "13800138000",
			region: "CN",
			want:   "+8613800138000",
		},
		{
			name:   "us local mobile",
			to:     "6502530000",
			region: "US",
			want:   "+16502530000",
		},
		{
			name:   "uk local mobile",
			to:     "07123456789",
			region: "GB",
			want:   "+447123456789",
		},
		{
			name:   "international is canonicalized",
			to:     "+1 (650) 253-0000",
			region: "CN",
			want:   "+16502530000",
		},
		{
			name:   "short code remains local",
			to:     "777",
			region: "NZ",
			want:   "777",
		},
		{
			name:   "long service code remains local",
			to:     "10086",
			region: "CN",
			want:   "10086",
		},
		{
			name:    "invalid local number",
			to:      "1234567",
			region:  "CN",
			wantErr: ErrRecipientInvalid,
		},
		{
			name:    "unknown region rejects local number",
			to:      "13800138000",
			region:  "UN",
			wantErr: ErrRecipientInvalid,
		},
		{
			name:   "unknown region accepts international number",
			to:     "+44 7123 456789",
			region: "UN",
			want:   "+447123456789",
		},
		{
			name:    "empty recipient",
			to:      " ",
			region:  "CN",
			wantErr: ErrRecipientRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRecipientForRegion(tt.to, tt.region)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("normalizeRecipientForRegion() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRecipientForRegion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRecipientForRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}
