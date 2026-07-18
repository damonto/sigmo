package auth

import "testing"

func TestFormatOTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		randomValue int64
		want        string
	}{
		{name: "lowest random value skips reserved code", randomValue: 0, want: "000001"},
		{name: "highest random value stays six digits", randomValue: otpMaxValue - 2, want: "999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := formatOTP(tt.randomValue); got != tt.want {
				t.Fatalf("formatOTP(%d) = %q, want %q", tt.randomValue, got, tt.want)
			}
		})
	}
}
