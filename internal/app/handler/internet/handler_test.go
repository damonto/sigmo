package internet

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestModemLookupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		code       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "not found",
			err:        mmodem.ErrNotFound,
			code:       "current_failed",
			wantStatus: http.StatusNotFound,
			wantBody:   "modem_not_found",
		},
		{
			name:       "internal",
			err:        errors.New("dbus unavailable"),
			code:       "current_failed",
			wantStatus: http.StatusInternalServerError,
			wantBody:   "current_failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := httpapi.ModemLookupError(c, tt.err, tt.code); err != nil {
				t.Fatalf("ModemLookupError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestInternetError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "unsupported ip method",
			err:        internetcore.ErrUnsupportedIPMethod,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   errorCodeUnsupportedInternetConfiguration,
		},
		{
			name:       "internal connect error",
			err:        errors.New("permission denied"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   errorCodeConnectInternetFailed,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := internetError(c, tt.err, errorCodeConnectInternetFailed); err != nil {
				t.Fatalf("internetError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestResponseFromPublic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info internetcore.IPInfo
		want PublicResponse
	}{
		{
			name: "public info",
			info: internetcore.IPInfo{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
			},
			want: PublicResponse{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := responseFromPublic(tt.info)
			if got != tt.want {
				t.Fatalf("responseFromPublic() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResponseFromConnection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		connection *internetcore.Connection
		want       ConnectionResponse
	}{
		{
			name: "connected with proxy",
			connection: &internetcore.Connection{
				Status:          internetcore.StatusConnected,
				APN:             "internet",
				DefaultRoute:    true,
				ProxyEnabled:    true,
				AlwaysOn:        true,
				Proxy:           internetcore.ProxyStatus{Enabled: true, Username: "wwan0", Password: "secret", HTTPAddress: "127.0.0.1:8080", SOCKS5Address: "127.0.0.1:1080"},
				InterfaceName:   "wwan0",
				Bearer:          "/bearer/1",
				IPv4Addresses:   []string{"10.0.0.2"},
				IPv6Addresses:   []string{},
				DNS:             []string{"1.1.1.1"},
				DurationSeconds: 12,
				TXBytes:         100,
				RXBytes:         200,
				RouteMetric:     10,
			},
			want: ConnectionResponse{
				Status:          internetcore.StatusConnected,
				APN:             "internet",
				DefaultRoute:    true,
				ProxyEnabled:    true,
				AlwaysOn:        true,
				Proxy:           Proxy{Enabled: true, Username: "wwan0", Password: "secret", HTTPAddress: "127.0.0.1:8080", SOCKS5Address: "127.0.0.1:1080"},
				InterfaceName:   "wwan0",
				Bearer:          "/bearer/1",
				IPv4Addresses:   []string{"10.0.0.2"},
				IPv6Addresses:   []string{},
				DNS:             []string{"1.1.1.1"},
				DurationSeconds: 12,
				TXBytes:         100,
				RXBytes:         200,
				RouteMetric:     10,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := responseFromConnection(tt.connection)
			if got.Status != tt.want.Status ||
				got.APN != tt.want.APN ||
				got.DefaultRoute != tt.want.DefaultRoute ||
				got.ProxyEnabled != tt.want.ProxyEnabled ||
				got.AlwaysOn != tt.want.AlwaysOn ||
				got.Proxy != tt.want.Proxy ||
				got.InterfaceName != tt.want.InterfaceName ||
				got.Bearer != tt.want.Bearer ||
				got.DurationSeconds != tt.want.DurationSeconds ||
				got.TXBytes != tt.want.TXBytes ||
				got.RXBytes != tt.want.RXBytes ||
				got.RouteMetric != tt.want.RouteMetric {
				t.Fatalf("responseFromConnection() = %#v, want %#v", got, tt.want)
			}
			if strings.Join(got.IPv4Addresses, ",") != strings.Join(tt.want.IPv4Addresses, ",") {
				t.Fatalf("IPv4Addresses = %#v, want %#v", got.IPv4Addresses, tt.want.IPv4Addresses)
			}
			if strings.Join(got.IPv6Addresses, ",") != strings.Join(tt.want.IPv6Addresses, ",") {
				t.Fatalf("IPv6Addresses = %#v, want %#v", got.IPv6Addresses, tt.want.IPv6Addresses)
			}
			if strings.Join(got.DNS, ",") != strings.Join(tt.want.DNS, ",") {
				t.Fatalf("DNS = %#v, want %#v", got.DNS, tt.want.DNS)
			}
		})
	}
}
