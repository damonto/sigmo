package wwan

import "testing"

func TestICCIDMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{name: "exact", current: "894921007608556913", target: "894921007608556913", want: true},
		{name: "target has trailing padding", current: "894921007608556913", target: "894921007608556913f", want: true},
		{name: "current has trailing padding", current: "894921007608556913FF", target: "894921007608556913", want: true},
		{name: "internal hexadecimal digit is preserved", current: "89860110F9900160570", target: "89860110f9900160570f", want: true},
		{name: "different internal digit", current: "8986011099900160570", target: "89860110f9900160570f"},
		{name: "empty target matches any SIM", current: "894921007608556913", want: true},
		{name: "empty current does not match target", target: "894921007608556913"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ICCIDMatches(tt.current, tt.target); got != tt.want {
				t.Fatalf("ICCIDMatches(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
			}
		})
	}
}
