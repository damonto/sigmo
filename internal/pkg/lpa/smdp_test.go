package lpa

import "testing"

func TestSMDPAddressText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "fqdn", raw: "smdp.example.com", want: "https://smdp.example.com"},
		{name: "url trims path", raw: "https://smdp.example.com/path", want: "https://smdp.example.com"},
		{name: "http normalizes scheme", raw: "http://smdp.example.com", want: "https://smdp.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var address SMDPAddress
			if err := address.UnmarshalText([]byte(tt.raw)); err != nil {
				t.Fatalf("SMDPAddress.UnmarshalText() error = %v", err)
			}
			text, err := address.MarshalText()
			if err != nil {
				t.Fatalf("SMDPAddress.MarshalText() error = %v", err)
			}
			if string(text) != tt.want {
				t.Fatalf("SMDPAddress.MarshalText() = %q, want %q", text, tt.want)
			}
			if got := address.URL().String(); got != tt.want {
				t.Fatalf("SMDPAddress.URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSMDPAddressTextErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty"},
		{name: "missing host", raw: "https:///path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var address SMDPAddress
			if err := address.UnmarshalText([]byte(tt.raw)); err == nil {
				t.Fatal("SMDPAddress.UnmarshalText() error = nil, want error")
			}
		})
	}
}
