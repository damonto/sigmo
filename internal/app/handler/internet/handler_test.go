package internet

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/apierror"
	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/validator"
)

func TestSpeedTestRequestSize(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		body string
		want internetcore.SpeedTestSize
	}{
		{"omitted", `{"iccid":"sim-1"}`, 10},
		{"null", `{"iccid":"sim-1","sizeMB":null}`, 10},
		{"selected", `{"iccid":"sim-1","sizeMB":50}`, 50},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			c := e.NewContext(req, httptest.NewRecorder())
			var input SpeedTestRequest
			if err := httpapi.BindJSON(c, &input, "invalid_iccid"); err != nil {
				t.Fatal(err)
			}
			if got, err := input.size(); err != nil || got != tt.want {
				t.Errorf("size = %d MB, %v, want %d MB", got, err, tt.want)
			}
		})
	}
}

func TestRunSpeedTestRejectsInvalidSizeBeforeModemLookup(t *testing.T) {
	t.Parallel()
	e := echo.New()
	var err error
	e.Validator, err = validator.New()
	if err != nil {
		t.Fatal(err)
	}
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	e.POST("/modems/:id/internet-connections/speed-tests", (&Handler{}).RunSpeedTest)
	for _, value := range []string{"0", "-10", "20", "101", "25.5", `"50"`, "true", "{}"} {
		t.Run(value, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/modems/modem-1/internet-connections/speed-tests",
				strings.NewReader(`{"iccid":"sim-1","sizeMB":`+value+`}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
			}
			var problem apierror.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.ErrorCode != "invalid_request" {
				t.Errorf("error_code = %q, want invalid_request", problem.ErrorCode)
			}
		})
	}
}

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
			code:       "current_internet_connection_failed",
			wantStatus: http.StatusNotFound,
			wantBody:   "modem_not_found",
		},
		{
			name:       "internal",
			err:        errors.New("modem unavailable"),
			code:       "current_internet_connection_failed",
			wantStatus: http.StatusInternalServerError,
			wantBody:   "current_internet_connection_failed",
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
			wantBody:   "internet_connection_unsupported_ip_method",
		},
		{
			name:       "default route conflict",
			err:        internetcore.ErrDefaultRouteConflict,
			wantStatus: http.StatusConflict,
			wantBody:   "internet_default_route_conflict",
		},
		{
			name:       "internal connect error",
			err:        errors.New("permission denied"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "connect_internet_failed",
		},
		{
			name:       "unsupported auth",
			err:        mmodem.ErrUnsupportedBearerAuth,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   "internet_auth_invalid_config",
		},
		{
			name:       "unsupported ip type",
			err:        mmodem.ErrUnsupportedBearerIPType,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   "internet_ip_type_invalid_config",
		},
		{
			name:       "profile required",
			err:        internetcore.ErrProfileIDRequired,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   "internet_profile_id_required",
		},
		{
			name:       "not connected",
			err:        internetcore.ErrNotConnected,
			wantStatus: http.StatusConflict,
			wantBody:   "internet_connection_not_connected",
		},
		{
			name:       "active SIM changed",
			err:        mmodem.ErrSIMChanged,
			wantStatus: http.StatusConflict,
			wantBody:   "sim_target_changed",
		},
		{
			name:       "airplane mode",
			err:        internetcore.ErrAirplaneMode,
			wantStatus: http.StatusConflict,
			wantBody:   "internet_airplane_mode",
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

			if err := internetError(c, tt.err, "connect_internet_failed"); err != nil {
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
		info internetcore.PublicIPInfo
		want PublicResponse
	}{
		{
			name: "public info",
			info: internetcore.PublicIPInfo{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
				ISP:          "HKT",
			},
			want: PublicResponse{
				IP:           "209.9.201.161",
				Country:      "HK",
				Organization: "AS4760 HKT Limited",
				ISP:          "HKT",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResponseFromPublic(tt.info)
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
				SessionID:       "session-1",
				APN:             "internet",
				IPType:          "ipv4",
				APNUsername:     "user",
				APNPassword:     "secret",
				APNAuth:         "pap",
				DefaultRoute:    true,
				ProxyEnabled:    true,
				AlwaysOn:        true,
				Proxy:           internetcore.ProxyStatus{Enabled: true, Username: "wwan0", Password: "secret", HTTPAddress: "127.0.0.1:8080", SOCKS5Address: "127.0.0.1:1080"},
				InterfaceName:   "wwan0",
				Bearer:          1,
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
				SessionID:       "session-1",
				APN:             "internet",
				IPType:          "ipv4",
				APNUsername:     "user",
				APNPassword:     "secret",
				APNAuth:         "pap",
				DefaultRoute:    true,
				ProxyEnabled:    true,
				AlwaysOn:        true,
				Proxy:           Proxy{Enabled: true, Username: "wwan0", Password: "secret", HTTPAddress: "127.0.0.1:8080", SOCKS5Address: "127.0.0.1:1080"},
				InterfaceName:   "wwan0",
				Bearer:          1,
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

			got := ResponseFromConnection(tt.connection)
			if got.Status != tt.want.Status ||
				got.SessionID != tt.want.SessionID ||
				got.APN != tt.want.APN ||
				got.IPType != tt.want.IPType ||
				got.APNUsername != tt.want.APNUsername ||
				got.APNPassword != tt.want.APNPassword ||
				got.APNAuth != tt.want.APNAuth ||
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
