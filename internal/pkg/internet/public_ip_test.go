package internet

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLookupPublicIP(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != "https://api.ip.sb/geoip" {
			t.Errorf("request = %s %s, want GET https://api.ip.sb/geoip", req.Method, req.URL)
		}
		if got := req.UserAgent(); got != "Sigmo" {
			t.Errorf("User-Agent = %q, want Sigmo", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"ip": " 203.0.113.1 ",
				"country": "United States",
				"country_code": " us ",
				"organization": " Example Network ",
				"asn_organization": "Example ASN Holder"
			}`)),
		}, nil
	})}

	got, err := LookupPublicIP(t.Context(), client)
	if err != nil {
		t.Fatalf("LookupPublicIP() error = %v", err)
	}
	want := PublicIPInfo{IP: "203.0.113.1", Country: "US", Organization: "Example Network"}
	if got != want {
		t.Errorf("LookupPublicIP() = %+v, want %+v", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
