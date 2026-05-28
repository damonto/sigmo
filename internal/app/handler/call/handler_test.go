package call

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
	}{
		{name: "ussd dial string", err: pcall.ErrUSSDDialString, wantStatus: http.StatusBadRequest, wantCode: errorCodeUSSDDialString},
		{name: "invalid number", err: pcall.ErrInvalidNumber, wantStatus: http.StatusBadRequest, wantCode: errorCodeCallNumberInvalid},
		{name: "no call route available", err: pcall.ErrNoRouteAvailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeNoCallRouteAvailable},
		{name: "wifi calling disconnected", err: pcall.ErrWiFiCallingNotConnected, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
		{name: "modem calling unavailable", err: pcall.ErrModemCallingUnavailable, wantStatus: http.StatusNotImplemented, wantCode: errorCodeModemCallingUnavailable},
		{name: "invalid call state", err: pcall.ErrInvalidCallState, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidCallState},
		{name: "wrapped wifi calling disconnected", err: errors.Join(errors.New("dial route"), pcall.ErrWiFiCallingNotConnected), wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeWiFiCallingNotConnected},
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
			req := httptest.NewRequest(http.MethodGet, "/api/v1/modems/test/calls/test/media", nil)
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
	if response.UpdatedAt != response.StartedAt {
		t.Fatalf("UpdatedAt = %q, want %q", response.UpdatedAt, response.StartedAt)
	}
	if response.AnsweredAt != "" || response.EndedAt != "" {
		t.Fatalf("unset times = answered %q ended %q, want empty strings", response.AnsweredAt, response.EndedAt)
	}
}

func TestBuildMediaInfoResponseIncludesPayloadFormat(t *testing.T) {
	response := buildMediaInfoResponse(pcall.MediaInfo{
		Codec:           "AMR-WB",
		PayloadType:     104,
		ClockRate:       16000,
		Channels:        1,
		OctetAlign:      false,
		DTMFPayloadType: 101,
		DTMFClockRate:   8000,
		PTimeMillis:     20,
	})

	if response.Codec != "AMR-WB" || response.PayloadType != 104 || response.ClockRate != 16000 {
		t.Fatalf("media response = %+v, want AMR-WB payload 104 clock 16000", response)
	}
	if response.OctetAlign {
		t.Fatal("OctetAlign = true, want false for bandwidth-efficient AMR")
	}
}

func TestReadCallMediaForwardsRTPPackets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	media := newFakeMediaSession()
	packetCh := make(chan []byte, 1)
	errCh := make(chan error, 1)

	go readCallMedia(ctx, media, packetCh, errCh)

	media.readCh <- []byte{1, 2, 3}

	select {
	case got := <-packetCh:
		if string(got) != string([]byte{1, 2, 3}) {
			t.Fatalf("packet = %v, want [1 2 3]", got)
		}
	case err := <-errCh:
		t.Fatalf("readCallMedia() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for media packet")
	}
}

func TestReadBrowserMediaWritesBinaryRTPPackets(t *testing.T) {
	media := newFakeMediaSession()
	errCh := make(chan error, 1)
	serverDone := make(chan struct{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen for websocket media test: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := callWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			close(serverDone)
			return
		}
		defer conn.Close()
		readBrowserMedia(r.Context(), conn, media, errCh)
		close(serverDone)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ignored")); err != nil {
		t.Fatalf("WriteMessage(text) error = %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{4, 5, 6}); err != nil {
		t.Fatalf("WriteMessage(binary) error = %v", err)
	}

	select {
	case got := <-media.writeCh:
		if string(got) != string([]byte{4, 5, 6}) {
			t.Fatalf("written packet = %v, want [4 5 6]", got)
		}
	case err := <-errCh:
		t.Fatalf("readBrowserMedia() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for written media packet")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket media reader to exit")
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
				{ID: "active", ModemID: "modem-1", State: pcall.StateActive},
				{ID: "ending", ModemID: "modem-1", State: pcall.StateEnding},
				{ID: "ended", ModemID: "modem-1", State: pcall.StateEnded},
				{ID: "failed", ModemID: "modem-1", State: pcall.StateFailed},
				{ID: "other", ModemID: "modem-2", State: pcall.StateActive},
			},
			want: []string{"dialing", "ringing", "answering", "active", "ending"},
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

type fakeMediaSession struct {
	readCh  chan []byte
	writeCh chan []byte
}

func newFakeMediaSession() *fakeMediaSession {
	return &fakeMediaSession{
		readCh:  make(chan []byte, 1),
		writeCh: make(chan []byte, 1),
	}
}

func (f *fakeMediaSession) Info() pcall.MediaInfo {
	return pcall.MediaInfo{}
}

func (f *fakeMediaSession) ReadPacket(ctx context.Context) ([]byte, error) {
	select {
	case packet := <-f.readCh:
		return append([]byte(nil), packet...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeMediaSession) WritePacket(ctx context.Context, packet []byte) error {
	copy := append([]byte(nil), packet...)
	select {
	case f.writeCh <- copy:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
