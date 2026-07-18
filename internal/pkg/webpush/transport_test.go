package webpush

import (
	"errors"
	"net/http"
	"net/netip"
	"testing"
)

func TestIsPublicPushAddress(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "public IPv4", value: "8.8.8.8", want: true},
		{name: "public IPv6", value: "2606:4700:4700::1111", want: true},
		{name: "private IPv4", value: "10.0.0.1"},
		{name: "carrier grade NAT", value: "100.64.0.1"},
		{name: "loopback IPv4", value: "127.0.0.1"},
		{name: "loopback IPv6", value: "::1"},
		{name: "link local IPv6", value: "fe80::1"},
		{name: "unspecified", value: "0.0.0.0"},
		{name: "multicast", value: "ff02::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPublicPushAddress(netip.MustParseAddr(tt.value)); got != tt.want {
				t.Fatalf("isPublicPushAddress(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestPushHTTPClientRejectsRedirects(t *testing.T) {
	client := newPushHTTPClient()
	err := client.CheckRedirect(&http.Request{}, []*http.Request{{}})
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want %v", err, http.ErrUseLastResponse)
	}
}
