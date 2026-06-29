package message

import (
	"errors"
	"testing"
)

func TestNormalizeSMSAddress(t *testing.T) {
	tests := []struct {
		name    string
		to      string
		want    string
		wantErr error
	}{
		{
			name: "china local mobile",
			to:   "13800138000",
			want: "13800138000",
		},
		{
			name: "us local mobile",
			to:   "6502530000",
			want: "6502530000",
		},
		{
			name: "uk local mobile",
			to:   "07123456789",
			want: "07123456789",
		},
		{
			name: "international keeps plus and removes separators",
			to:   "+1 (650) 253-0000",
			want: "+16502530000",
		},
		{
			name: "short code remains local",
			to:   "777",
			want: "777",
		},
		{
			name: "long service code remains local",
			to:   "10086",
			want: "10086",
		},
		{
			name: "china sms service number remains local",
			to:   "106 90760295102",
			want: "10690760295102",
		},
		{
			name: "local number does not need a region",
			to:   "13800138000",
			want: "13800138000",
		},
		{
			name: "international access code remains dial address",
			to:   "011 86 138 0013 8000",
			want: "0118613800138000",
		},
		{
			name: "double zero access code remains dial address",
			to:   "0086 138 0013 8000",
			want: "008613800138000",
		},
		{
			name:    "letters are invalid",
			to:      "abc123",
			wantErr: ErrRecipientInvalid,
		},
		{
			name:    "ussd characters are invalid",
			to:      "*123#",
			wantErr: ErrRecipientInvalid,
		},
		{
			name:    "empty recipient",
			to:      " ",
			wantErr: ErrRecipientRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSMSAddress(tt.to)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("normalizeSMSAddress() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSMSAddress() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeSMSAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
