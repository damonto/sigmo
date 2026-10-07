//go:build ims

package call

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestWriteCallEvent(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 123, time.UTC)
	call := storage.Call{ID: "call-1", ModemID: "modem-1", State: StateActive}
	writeDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := callWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			writeDone <- err
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Errorf("close server socket: %v", err)
			}
		}()
		if err := writeCallEvent(conn, Event{Call: call}); err != nil {
			writeDone <- err
			return
		}
		writeDone <- writeCallEvent(conn, Event{Call: call, DTMF: &DTMF{Digit: "#", At: at}})
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial test socket: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close client socket: %v", err)
		}
	}()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var state EventMessage
	if err := conn.ReadJSON(&state); err != nil {
		t.Fatalf("read state event: %v", err)
	}
	if state.Type != "call" || state.Call != ResponseFromCall(call) {
		t.Fatalf("state event = %+v, want unchanged call message", state)
	}
	var dtmf map[string]string
	if err := conn.ReadJSON(&dtmf); err != nil {
		t.Fatalf("read DTMF event: %v", err)
	}
	if len(dtmf) != 4 || dtmf["type"] != "dtmf" || dtmf["callID"] != "call-1" || dtmf["digit"] != "#" || dtmf["at"] != "2026-09-28T10:00:00.000000123Z" {
		t.Fatalf("DTMF message = %+v, want only type, callID, digit and receive time", dtmf)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("writeCallEvent() error = %v", err)
	}
}

func TestCallActionErrorMapsExpectedFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{name: "ussd dial string", err: ErrUSSDDialString, wantStatus: http.StatusBadRequest, wantCode: errorCodeUSSDDialString},
		{name: "invalid number", err: ErrInvalidNumber, wantStatus: http.StatusBadRequest, wantCode: errorCodeCallNumberInvalid},
		{name: "no call route available", err: ErrNoRouteAvailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeNoCallRouteAvailable},
		{name: "wifi calling disconnected", err: ErrWiFiCallingNotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
		{name: "volte disconnected", err: ErrVoLTENotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeVoLTENotConnected},
		{name: "modem calling unavailable", err: ErrModemCallingUnavailable, wantStatus: http.StatusNotImplemented, wantCode: errorCodeModemCallingUnavailable},
		{name: "invalid call state", err: ErrInvalidCallState, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidCallState},
		{name: "invalid call hold", err: ErrInvalidCallHold, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidCallHold},
		{name: "state and hold conflict", err: ErrCallUpdateConflict, wantStatus: http.StatusBadRequest, wantCode: errorCodeCallUpdateConflict},
		{name: "dtmf digits required", err: ErrDTMFDigitsRequired, wantStatus: http.StatusBadRequest, wantCode: errorCodeDTMFDigitsRequired},
		{name: "invalid dtmf digit", err: ErrInvalidDTMFDigit, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidDTMFDigit},
		{name: "invalid dtmf state", err: ErrInvalidDTMFCallState, wantStatus: http.StatusConflict, wantCode: errorCodeInvalidDTMFCallState},
		{name: "call on hold", err: ErrCallOnHold, wantStatus: http.StatusConflict, wantCode: errorCodeCallOnHold},
		{name: "dtmf unsupported", err: ErrUnsupportedDTMF, wantStatus: http.StatusNotImplemented, wantCode: errorCodeCallDTMFUnsupported},
		{name: "active call record delete", err: ErrCallRecordActive, wantStatus: http.StatusConflict, wantCode: errorCodeCallRecordActive},
		{name: "wrapped wifi calling disconnected", err: errors.Join(errors.New("dial route"), ErrWiFiCallingNotConnected), wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
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
		{name: "unsupported codec", err: ErrUnsupportedCodec, wantStatus: http.StatusUnsupportedMediaType, wantCode: errorCodeCallMediaUnsupportedCodec},
		{name: "media unavailable", err: ErrMediaUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeCallMediaUnavailable},
		{name: "wifi calling disconnected", err: ErrWiFiCallingNotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/modems/test/calls/test/webrtc/sessions", nil)
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
	response := ResponseFromCall(storage.Call{
		ID:        "call-1",
		Route:     RouteWiFiCalling,
		Direction: DirectionOutgoing,
		Number:    "+12242255559",
		State:     StateDialing,
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	})

	if response.StartedAt != "2026-05-27T10:00:00.000000123Z" {
		t.Fatalf("StartedAt = %q, want RFC3339Nano timestamp", response.StartedAt)
	}
	if response.Number != "+12242255559" {
		t.Fatalf("Number = %q, want raw number", response.Number)
	}
	if response.Hold != HoldNone {
		t.Fatalf("Hold = %q, want %q", response.Hold, HoldNone)
	}
	if response.UpdatedAt != response.StartedAt {
		t.Fatalf("UpdatedAt = %q, want %q", response.UpdatedAt, response.StartedAt)
	}
	if response.AnsweredAt != "" || response.EndedAt != "" {
		t.Fatalf("unset times = answered %q ended %q, want empty strings", response.AnsweredAt, response.EndedAt)
	}
}

func TestBuildWebRTCICEServersResponse(t *testing.T) {
	tests := []struct {
		name    string
		servers []WebRTCICEServer
		wantURL string
	}{
		{
			name: "turn credentials",
			servers: []WebRTCICEServer{
				{
					URLs:       []string{"turn:turn.example.com:3478"},
					Username:   "sigmo",
					Credential: "secret",
				},
			},
			wantURL: "turn:turn.example.com:3478",
		},
		{
			name: "stun",
			servers: []WebRTCICEServer{
				{URLs: []string{"stun:stun.l.google.com:19302"}},
			},
			wantURL: "stun:stun.l.google.com:19302",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildWebRTCICEServersResponse(tt.servers)
			if len(got.ICEServers) != 1 {
				t.Fatalf("iceServers len = %d, want 1", len(got.ICEServers))
			}
			server := got.ICEServers[0]
			if len(server.URLs) != 1 || server.URLs[0] != tt.wantURL {
				t.Fatalf("urls = %v, want %q", server.URLs, tt.wantURL)
			}
			if server.Username != tt.servers[0].Username || server.Credential != tt.servers[0].Credential {
				t.Fatalf("auth = %q/%q, want %q/%q", server.Username, server.Credential, tt.servers[0].Username, tt.servers[0].Credential)
			}
		})
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
				{ID: "dialing", ModemID: "modem-1", State: StateDialing},
				{ID: "ringing", ModemID: "modem-1", State: StateRinging},
				{ID: "answering", ModemID: "modem-1", State: StateAnswering},
				{ID: "early-media", ModemID: "modem-1", State: StateEarlyMedia},
				{ID: "active", ModemID: "modem-1", State: StateActive},
				{ID: "confirmed", ModemID: "modem-1", State: StateConfirmed},
				{ID: "ending", ModemID: "modem-1", State: StateEnding},
				{ID: "ended", ModemID: "modem-1", State: StateEnded},
				{ID: "failed", ModemID: "modem-1", State: StateFailed},
				{ID: "other", ModemID: "modem-2", State: StateActive},
			},
			want: []string{"dialing", "ringing", "answering", "early-media", "active", "confirmed", "ending"},
		},
		{
			name: "empty",
			calls: []storage.Call{
				{ID: "ended", ModemID: "modem-1", State: StateEnded},
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

func TestWebRTCICEServersRespondsWithEmptyListWhenTURNUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	h := &Handler{media: &Media{ice: webRTCICEProvider{client: server.Client(), endpoint: server.URL}}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/call-media/ice-servers", nil)
	if err := h.WebRTCICEServers(echo.New().NewContext(req, recorder)); err != nil {
		t.Fatalf("WebRTCICEServers() error = %v", err)
	}
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"iceServers":[]}` {
		t.Fatalf("response = %d %s, want 200 with empty iceServers array", recorder.Code, recorder.Body.String())
	}
}
