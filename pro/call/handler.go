//go:build ims

package call

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Handler struct {
	registry *mmodem.Registry
	calls    *Calls
	media    *Media
}

const (
	errorCodeListCallsFailed           = "list_calls_failed"
	errorCodeListWebRTCICEServers      = "list_webrtc_ice_servers_failed"
	errorCodeDialCallInvalidRequest    = "dial_call_invalid_request"
	errorCodeDialCallFailed            = "dial_call_failed"
	errorCodeUpdateCallInvalidRequest  = "update_call_invalid_request"
	errorCodeUpdateCallFailed          = "update_call_failed"
	errorCodeSendDTMFInvalidRequest    = "send_dtmf_invalid_request"
	errorCodeSendDTMFFailed            = "send_dtmf_failed"
	errorCodeCallNumberRequired        = "call_number_required"
	errorCodeCallNumberInvalid         = "call_number_invalid"
	errorCodeUSSDDialString            = "ussd_dial_string"
	errorCodeInvalidCallRoute          = "invalid_call_route"
	errorCodeNoCallRouteAvailable      = "no_call_route_available"
	errorCodeWiFiCallingNotConnected   = "wifi_calling_not_connected"
	errorCodeVoLTENotConnected         = "volte_not_connected"
	errorCodeModemCallingUnavailable   = "modem_calling_unavailable"
	errorCodeCallNotFound              = "call_not_found"
	errorCodeInvalidCallState          = "invalid_call_state"
	errorCodeInvalidCallHold           = "invalid_call_hold"
	errorCodeCallUpdateConflict        = "call_update_conflict"
	errorCodeDTMFDigitsRequired        = "dtmf_digits_required"
	errorCodeInvalidDTMFDigit          = "invalid_dtmf_digit"
	errorCodeInvalidDTMFCallState      = "invalid_dtmf_call_state"
	errorCodeCallOnHold                = "call_on_hold"
	errorCodeCallRecordActive          = "call_record_active"
	errorCodeCallDTMFUnsupported       = "call_dtmf_unsupported"
	errorCodeHangupCallFailed          = "hangup_call_failed"
	errorCodeDeleteCallFailed          = "delete_call_failed"
	errorCodeCallMediaUnavailable      = "call_media_unavailable"
	errorCodeCallMediaUnsupportedCodec = "call_media_unsupported_codec"
	errorCodeSubscribeCallEventsFailed = "subscribe_call_events_failed"
)

var callWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return sameOrigin(r)
	},
}

func RegisterRoutes(group *echo.Group, registry *mmodem.Registry, calls *Calls, media *Media) {
	h := &Handler{registry: registry, calls: calls, media: media}
	group.GET("/call-media/ice-servers", h.WebRTCICEServers)
	group.GET("/modems/:id/calls", h.List)
	group.POST("/modems/:id/calls", h.Dial)
	group.GET("/modems/:id/calls/events", h.Events)
	group.GET("/modems/:id/calls/:callID/webrtc/sessions", h.WebRTCSession)
	group.POST("/modems/:id/calls/:callID/dtmf-events", h.SendDTMF)
	group.PATCH("/modems/:id/calls/:callID", h.Update)
	group.DELETE("/modems/:id/calls/:callID", h.Delete)
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListCallsFailed)
	}
	calls, err := h.calls.List(c.Request().Context(), modem, c.QueryParam("q"))
	if err != nil {
		return httpapi.Internal(c, errorCodeListCallsFailed, err)
	}
	return c.JSON(http.StatusOK, ResponsesFromCalls(calls))
}

func (h *Handler) WebRTCICEServers(c *echo.Context) error {
	servers, err := h.media.WebRTCICEServers(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, errorCodeListWebRTCICEServers, err)
	}
	return c.JSON(http.StatusOK, buildWebRTCICEServersResponse(servers))
}

func (h *Handler) Dial(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDialCallFailed)
	}
	var req DialRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeDialCallInvalidRequest); err != nil {
		return err
	}
	call, err := h.calls.Dial(c.Request().Context(), modem, req.To, req.Route)
	if err != nil {
		return callActionError(c, err, errorCodeDialCallFailed)
	}
	return c.JSON(http.StatusCreated, ResponseFromCall(call))
}

func (h *Handler) Update(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateCallFailed)
	}
	var req UpdateCallRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateCallInvalidRequest); err != nil {
		return err
	}
	call, err := h.calls.Update(c.Request().Context(), modem, callIDParam(c), UpdateRequest{
		State:  req.State,
		Reason: req.Reason,
		Hold:   req.Hold,
	})
	if err != nil {
		return callActionError(c, err, errorCodeUpdateCallFailed)
	}
	return c.JSON(http.StatusOK, ResponseFromCall(call))
}

func (h *Handler) Hangup(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeHangupCallFailed)
	}
	call, err := h.calls.Hangup(c.Request().Context(), modem, callIDParam(c))
	if err != nil {
		return callActionError(c, err, errorCodeHangupCallFailed)
	}
	return c.JSON(http.StatusOK, ResponseFromCall(call))
}

func (h *Handler) SendDTMF(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSendDTMFFailed)
	}
	var req SendDTMFRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeSendDTMFInvalidRequest, err)
	}
	if err := h.calls.SendDTMF(c.Request().Context(), modem, callIDParam(c), req.Digits); err != nil {
		return callActionError(c, err, errorCodeSendDTMFFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteCallFailed)
	}
	if err := h.calls.Delete(c.Request().Context(), modem, callIDParam(c)); err != nil {
		return callActionError(c, err, errorCodeDeleteCallFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func callIDParam(c *echo.Context) string {
	callID := c.Param("callID")
	decoded, err := url.PathUnescape(callID)
	if err != nil {
		return callID
	}
	return decoded
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if sameHost(parsed.Host, host) {
		return true
	}
	originHost := hostName(parsed.Host)
	if isLoopbackHost(originHost) {
		return true
	}
	return sameHost(originHost, r.RemoteAddr)
}

func sameHost(left string, right string) bool {
	leftName := hostName(left)
	rightName := hostName(right)
	return leftName != "" && rightName != "" && strings.EqualFold(leftName, rightName)
}

func hostName(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	name, _, err := net.SplitHostPort(host)
	if err == nil {
		return strings.Trim(name, "[]")
	}
	return strings.Trim(host, "[]")
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) Events(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSubscribeCallEventsFailed)
	}
	events, unsubscribe := h.calls.Subscribe(16)
	defer unsubscribe()
	currentCalls, err := h.calls.List(c.Request().Context(), modem, "")
	if err != nil {
		return httpapi.Internal(c, errorCodeSubscribeCallEventsFailed, err)
	}
	conn, err := callWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeCurrentCallEvents(conn, currentCalls, modem.EquipmentIdentifier); err != nil {
		return nil
	}
	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Call.ModemID != modem.EquipmentIdentifier {
				continue
			}
			if err := writeCallEvent(conn, event.Call); err != nil {
				return nil
			}
		}
	}
}

func (h *Handler) WebRTCSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeCallMediaUnavailable)
	}
	conn, err := callWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	return h.serveWebRTCSession(c.Request().Context(), conn, modem, callIDParam(c))
}

func (h *Handler) serveWebRTCSession(ctx context.Context, conn *websocket.Conn, modem *mmodem.Modem, callID string) error {
	var (
		session *WebRTCSession
		writeMu sync.Mutex
	)
	defer func() {
		if session != nil {
			session.CloseIfNotConnected()
		}
	}()

	for {
		var message WebRTCSignalMessage
		if err := conn.ReadJSON(&message); err != nil {
			return nil
		}
		switch message.Type {
		case "offer":
			if message.Offer == nil || session != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, "invalid WebRTC offer")
				return nil
			}
			nextSession, err := h.media.OpenWebRTCSession(ctx, modem, callID)
			if err != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, callMediaMessage(err))
				return nil
			}
			session = nextSession
			go writeWebRTCIceCandidates(ctx, conn, &writeMu, session.ICECandidates())
			answer, err := session.Answer(ctx, WebRTCSessionDescription{
				Type: message.Offer.Type,
				SDP:  message.Offer.SDP,
			})
			if err != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, callMediaMessage(err))
				return nil
			}
			if err := writeWebRTCSignal(conn, &writeMu, WebRTCSignalMessage{
				Type: "answer",
				Answer: &WebRTCSessionDescription{
					Type: answer.Type,
					SDP:  answer.SDP,
				},
			}); err != nil {
				return nil
			}
		case "candidate":
			if message.Candidate == nil || session == nil {
				continue
			}
			if err := session.AddICECandidate(WebRTCICECandidate{
				Candidate:        message.Candidate.Candidate,
				SDPMid:           message.Candidate.SDPMid,
				SDPMLineIndex:    message.Candidate.SDPMLineIndex,
				UsernameFragment: message.Candidate.UsernameFragment,
			}); err != nil {
				_ = writeWebRTCSignalError(conn, &writeMu, callMediaMessage(err))
				return nil
			}
		default:
			_ = writeWebRTCSignalError(conn, &writeMu, "invalid WebRTC signal")
			return nil
		}
	}
}

func writeCallEvent(conn *websocket.Conn, call storage.Call) error {
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(EventMessage{Type: "call", Call: ResponseFromCall(call)})
}

func writeWebRTCIceCandidates(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, candidates <-chan WebRTCICECandidate) {
	for {
		select {
		case <-ctx.Done():
			return
		case candidate, ok := <-candidates:
			if !ok {
				return
			}
			if err := writeWebRTCSignal(conn, writeMu, WebRTCSignalMessage{
				Type: "candidate",
				Candidate: &WebRTCICECandidate{
					Candidate:        candidate.Candidate,
					SDPMid:           candidate.SDPMid,
					SDPMLineIndex:    candidate.SDPMLineIndex,
					UsernameFragment: candidate.UsernameFragment,
				},
			}); err != nil {
				return
			}
		}
	}
}

func writeWebRTCSignalError(conn *websocket.Conn, writeMu *sync.Mutex, message string) error {
	return writeWebRTCSignal(conn, writeMu, WebRTCSignalMessage{Type: "error", Message: message})
}

func writeWebRTCSignal(conn *websocket.Conn, writeMu *sync.Mutex, message WebRTCSignalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(message)
}

func writeCurrentCallEvents(conn *websocket.Conn, calls []storage.Call, modemID string) error {
	for _, call := range currentCallEvents(calls, modemID) {
		if err := writeCallEvent(conn, call); err != nil {
			return err
		}
	}
	return nil
}

func currentCallEvents(calls []storage.Call, modemID string) []storage.Call {
	current := make([]storage.Call, 0, len(calls))
	for _, call := range calls {
		if call.ModemID != modemID || isTerminalCallState(call.State) {
			continue
		}
		current = append(current, call)
	}
	return current
}

func callActionError(c *echo.Context, err error, fallback string) error {
	switch {
	case errors.Is(err, ErrNumberRequired):
		return httpapi.BadRequest(c, errorCodeCallNumberRequired, err)
	case errors.Is(err, ErrInvalidNumber):
		return httpapi.BadRequest(c, errorCodeCallNumberInvalid, err)
	case errors.Is(err, ErrUSSDDialString):
		return httpapi.BadRequest(c, errorCodeUSSDDialString, err)
	case errors.Is(err, ErrInvalidRoute):
		return httpapi.BadRequest(c, errorCodeInvalidCallRoute, err)
	case errors.Is(err, ErrNoRouteAvailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeNoCallRouteAvailable, err.Error())
	case errors.Is(err, ErrWiFiCallingNotConnected):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeWiFiCallingNotConnected, err.Error())
	case errors.Is(err, ErrVoLTENotConnected):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeVoLTENotConnected, err.Error())
	case errors.Is(err, ErrModemCallingUnavailable):
		return httpapi.Error(c, http.StatusNotImplemented, errorCodeModemCallingUnavailable, err.Error())
	case errors.Is(err, ErrCallNotFound):
		return httpapi.NotFound(c, errorCodeCallNotFound, err)
	case errors.Is(err, ErrInvalidCallState):
		return httpapi.BadRequest(c, errorCodeInvalidCallState, err)
	case errors.Is(err, ErrInvalidCallHold):
		return httpapi.BadRequest(c, errorCodeInvalidCallHold, err)
	case errors.Is(err, ErrCallUpdateConflict):
		return httpapi.BadRequest(c, errorCodeCallUpdateConflict, err)
	case errors.Is(err, ErrDTMFDigitsRequired):
		return httpapi.BadRequest(c, errorCodeDTMFDigitsRequired, err)
	case errors.Is(err, ErrInvalidDTMFDigit):
		return httpapi.BadRequest(c, errorCodeInvalidDTMFDigit, err)
	case errors.Is(err, ErrInvalidDTMFCallState):
		return httpapi.Error(c, http.StatusConflict, errorCodeInvalidDTMFCallState, err.Error())
	case errors.Is(err, ErrCallOnHold):
		return httpapi.Error(c, http.StatusConflict, errorCodeCallOnHold, err.Error())
	case errors.Is(err, ErrCallRecordActive):
		return httpapi.Error(c, http.StatusConflict, errorCodeCallRecordActive, err.Error())
	case errors.Is(err, ErrUnsupportedDTMF):
		return httpapi.Error(c, http.StatusNotImplemented, errorCodeCallDTMFUnsupported, err.Error())
	case fallback == errorCodeDialCallFailed:
		return httpapi.Error(c, http.StatusBadGateway, errorCodeDialCallFailed, callActionMessage(err))
	default:
		return httpapi.Internal(c, fallback, err)
	}
}

func callActionMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	message = strings.TrimPrefix(message, "dial Wi-Fi Calling: ")
	message = strings.TrimPrefix(message, "dial VoLTE: ")
	if message == "" {
		return "call failed"
	}
	return message
}

func callMediaError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrUnsupportedCodec):
		return httpapi.Error(c, http.StatusUnsupportedMediaType, errorCodeCallMediaUnsupportedCodec, err.Error())
	case errors.Is(err, ErrMediaUnavailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeCallMediaUnavailable, err.Error())
	default:
		return callActionError(c, err, errorCodeCallMediaUnavailable)
	}
}

func callMediaMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "call media is not available"
	}
	return message
}

func ResponsesFromCalls(calls []storage.Call) []CallResponse {
	response := make([]CallResponse, 0, len(calls))
	for _, call := range calls {
		response = append(response, ResponseFromCall(call))
	}
	return response
}

func buildWebRTCICEServersResponse(servers []WebRTCICEServer) WebRTCICEServersResponse {
	out := make([]WebRTCICEServerResponse, 0, len(servers))
	for _, server := range servers {
		out = append(out, WebRTCICEServerResponse{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return WebRTCICEServersResponse{ICEServers: out}
}

func ResponseFromCall(call storage.Call) CallResponse {
	hold := strings.TrimSpace(call.Hold)
	if hold == "" {
		hold = HoldNone
	}
	return CallResponse{
		ID:         call.ID,
		Route:      call.Route,
		Direction:  call.Direction,
		Number:     call.Number,
		State:      call.State,
		Hold:       hold,
		Reason:     call.Reason,
		StartedAt:  callTime(call.StartedAt),
		AnsweredAt: callTime(call.AnsweredAt),
		EndedAt:    callTime(call.EndedAt),
		UpdatedAt:  callTime(call.UpdatedAt),
	}
}

func callTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}
