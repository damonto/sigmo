package call

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pcall "github.com/damonto/sigmo/internal/pkg/call"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestCallActionErrorMapsExpectedFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{name: "ussd dial string", err: pcall.ErrUSSDDialString, wantStatus: http.StatusBadRequest, wantCode: errorCodeUSSDDialString},
		{name: "invalid number", err: pcall.ErrInvalidNumber, wantStatus: http.StatusBadRequest, wantCode: errorCodeCallNumberInvalid},
		{name: "no call route available", err: pcall.ErrNoRouteAvailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeNoCallRouteAvailable},
		{name: "wifi calling disconnected", err: pcall.ErrWiFiCallingNotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
		{name: "modem calling unavailable", err: pcall.ErrModemCallingUnavailable, wantStatus: http.StatusNotImplemented, wantCode: errorCodeModemCallingUnavailable},
		{name: "invalid call state", err: pcall.ErrInvalidCallState, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidCallState},
		{name: "invalid call hold", err: pcall.ErrInvalidCallHold, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidCallHold},
		{name: "state and hold conflict", err: pcall.ErrCallUpdateConflict, wantStatus: http.StatusBadRequest, wantCode: errorCodeCallUpdateConflict},
		{name: "dtmf digits required", err: pcall.ErrDTMFDigitsRequired, wantStatus: http.StatusBadRequest, wantCode: errorCodeDTMFDigitsRequired},
		{name: "invalid dtmf digit", err: pcall.ErrInvalidDTMFDigit, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidDTMFDigit},
		{name: "invalid dtmf state", err: pcall.ErrInvalidDTMFCallState, wantStatus: http.StatusConflict, wantCode: errorCodeInvalidDTMFCallState},
		{name: "call on hold", err: pcall.ErrCallOnHold, wantStatus: http.StatusConflict, wantCode: errorCodeCallOnHold},
		{name: "dtmf unsupported", err: pcall.ErrUnsupportedDTMF, wantStatus: http.StatusNotImplemented, wantCode: errorCodeCallDTMFUnsupported},
		{name: "active call record delete", err: pcall.ErrCallRecordActive, wantStatus: http.StatusConflict, wantCode: errorCodeCallRecordActive},
		{name: "wrapped wifi calling disconnected", err: errors.Join(errors.New("dial route"), pcall.ErrWiFiCallingNotConnected), wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
		{name: "dial rejection", err: errors.New("dial Wi-Fi Calling: Credit limit reached"), wantStatus: http.StatusBadGateway, wantCode: errorCodeDialCallFailed, wantMsg: "Credit limit reached"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/test/calls", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := callActionError(c, tt.err, errorCodeDialCallFailed); err != nil {
				t.Fatalf("callActionError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			var got httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.ErrorCode != tt.wantCode {
				t.Fatalf("error_code = %q, want %q", got.ErrorCode, tt.wantCode)
			}
			if tt.wantMsg != "" && got.Message != tt.wantMsg {
				t.Fatalf("message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

func TestCallMediaErrorMapsExpectedFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "unsupported codec", err: pcall.ErrUnsupportedCodec, wantStatus: http.StatusUnsupportedMediaType, wantCode: errorCodeCallMediaUnsupportedCodec},
		{name: "media unavailable", err: pcall.ErrMediaUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeCallMediaUnavailable},
		{name: "wifi calling disconnected", err: pcall.ErrWiFiCallingNotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/modems/test/calls/test/webrtc-offer", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := callMediaError(c, tt.err); err != nil {
				t.Fatalf("callMediaError() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			var got httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.ErrorCode != tt.wantCode {
				t.Fatalf("error_code = %q, want %q", got.ErrorCode, tt.wantCode)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		origin     string
		remoteAddr string
		want       bool
	}{
		{name: "non browser client", host: "sigmo.local", want: true},
		{name: "same host", host: "sigmo.local", origin: "http://sigmo.local", want: true},
		{name: "same host different port", host: "10.10.10.101:9527", origin: "http://10.10.10.101:5173", want: true},
		{name: "loopback dev origin", host: "10.10.10.101:9527", origin: "http://localhost:5173", want: true},
		{name: "remote dev origin", host: "10.10.10.101:9527", origin: "http://10.10.10.200:5173", remoteAddr: "10.10.10.200:60123", want: true},
		{name: "same forwarded host", host: "127.0.0.1:8080", origin: "https://sigmo.example", want: true},
		{name: "different host", host: "sigmo.local", origin: "https://evil.example", want: false},
		{name: "invalid origin", host: "sigmo.local", origin: "://bad", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/modems/test/calls/events", nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remoteAddr
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.name == "same forwarded host" {
				req.Header.Set("X-Forwarded-Host", "sigmo.example")
			}

			if got := sameOrigin(req); got != tt.want {
				t.Fatalf("sameOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCallResponseFormatsUnsetTimesAsEmptyStrings(t *testing.T) {
	startedAt := time.Date(2026, 5, 27, 10, 0, 0, 123, time.UTC)
	response := buildCallResponse(storage.Call{
		ID:        "call-1",
		Route:     pcall.RouteWiFiCalling,
		Direction: pcall.DirectionOutgoing,
		Number:    "+12242255559",
		State:     pcall.StateDialing,
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	})

	if response.StartedAt != "2026-05-27T10:00:00.000000123Z" {
		t.Fatalf("StartedAt = %q, want RFC3339Nano timestamp", response.StartedAt)
	}
	if response.Number != "+12242255559" {
		t.Fatalf("Number = %q, want raw number", response.Number)
	}
	if response.Hold != pcall.HoldNone {
		t.Fatalf("Hold = %q, want %q", response.Hold, pcall.HoldNone)
	}
	if response.UpdatedAt != response.StartedAt {
		t.Fatalf("UpdatedAt = %q, want %q", response.UpdatedAt, response.StartedAt)
	}
	if response.AnsweredAt != "" || response.EndedAt != "" {
		t.Fatalf("unset times = answered %q ended %q, want empty strings", response.AnsweredAt, response.EndedAt)
	}
}

func TestCurrentCallEventsFiltersTerminalAndOtherModemCalls(t *testing.T) {
	tests := []struct {
		name  string
		calls []storage.Call
		want  []string
	}{
		{
			name: "current calls only",
			calls: []storage.Call{
				{ID: "dialing", ModemID: "modem-1", State: pcall.StateDialing},
				{ID: "ringing", ModemID: "modem-1", State: pcall.StateRinging},
				{ID: "answering", ModemID: "modem-1", State: pcall.StateAnswering},
				{ID: "early-media", ModemID: "modem-1", State: pcall.StateEarlyMedia},
				{ID: "active", ModemID: "modem-1", State: pcall.StateActive},
				{ID: "confirmed", ModemID: "modem-1", State: pcall.StateConfirmed},
				{ID: "ending", ModemID: "modem-1", State: pcall.StateEnding},
				{ID: "ended", ModemID: "modem-1", State: pcall.StateEnded},
				{ID: "failed", ModemID: "modem-1", State: pcall.StateFailed},
				{ID: "other", ModemID: "modem-2", State: pcall.StateActive},
			},
			want: []string{"dialing", "ringing", "answering", "early-media", "active", "confirmed", "ending"},
		},
		{
			name: "empty",
			calls: []storage.Call{
				{ID: "ended", ModemID: "modem-1", State: pcall.StateEnded},
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := currentCallEvents(tt.calls, "modem-1")
			var ids []string
			for _, call := range got {
				ids = append(ids, call.ID)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("currentCallEvents() ids = %v, want %v", ids, tt.want)
			}
			for i := range ids {
				if ids[i] != tt.want[i] {
					t.Fatalf("currentCallEvents() ids = %v, want %v", ids, tt.want)
				}
			}
		})
	}
}
