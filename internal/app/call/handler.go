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

var callWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return sameOrigin(r)
	},
}

func RegisterRoutes(group *echo.Group, registry *mmodem.Registry, calls *Calls, media *Media) {
	h := &Handler{registry: registry, calls: calls, media: media}
	group.GET("/calls", h.history)
	group.GET("/calls/:callID", h.historyRecord)
	group.DELETE("/calls/:callID", h.deleteHistory)
	group.GET("/call-media/ice-servers", h.WebRTCICEServers)
	group.GET("/modems/:id/calls", h.List)
	group.POST("/modems/:id/calls", h.Dial)
	group.GET("/modems/:id/calls/events", h.Events)
	group.GET("/modems/:id/calls/:callID", h.Get)
	group.GET("/modems/:id/calls/:callID/webrtc/sessions", h.WebRTCSession)
	group.POST("/modems/:id/calls/:callID/dtmf-events", h.SendDTMF)
	group.PATCH("/modems/:id/calls/:callID", h.Update)
	group.DELETE("/modems/:id/calls/:callID", h.Delete)
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_calls_failed")
	}
	calls, err := h.calls.List(c.Request().Context(), modem, c.QueryParam("q"))
	if err != nil {
		return httpapi.Internal(c, "list_calls_failed", err)
	}
	return c.JSON(http.StatusOK, ResponsesFromCalls(calls))
}

// Get reconciles one call without depending on the history window.
func (h *Handler) Get(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_calls_failed")
	}
	call, err := h.calls.Get(c.Request().Context(), modem, callIDParam(c))
	if err != nil {
		return callActionError(c, err, "list_calls_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromCall(call))
}

func (h *Handler) WebRTCICEServers(c *echo.Context) error {
	servers, err := h.media.WebRTCICEServers(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "list_webrtc_ice_servers_failed", err)
	}
	return c.JSON(http.StatusOK, buildWebRTCICEServersResponse(servers))
}

func (h *Handler) Dial(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "dial_call_failed")
	}
	var req DialRequest
	if err := httpapi.BindAndValidate(c, &req, "dial_call_invalid_request"); err != nil {
		return err
	}
	call, err := h.calls.DialForClient(c.Request().Context(), modem, req)
	if err != nil {
		return callActionError(c, err, "dial_call_failed")
	}
	return c.JSON(http.StatusCreated, ResponseFromCall(call))
}

func (h *Handler) Update(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_call_failed")
	}
	var req UpdateCallRequest
	if err := httpapi.BindAndValidate(c, &req, "update_call_invalid_request"); err != nil {
		return err
	}
	call, err := h.calls.UpdateForClient(c.Request().Context(), modem, ClientUpdate{
		CallID:   callIDParam(c),
		ClientID: req.ClientID,
		Update: UpdateRequest{
			State:  req.State,
			Reason: req.Reason,
			Hold:   req.Hold,
		},
	})
	if err != nil {
		return callActionError(c, err, "update_call_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromCall(call))
}

func (h *Handler) Hangup(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "hangup_call_failed")
	}
	call, err := h.calls.Hangup(c.Request().Context(), modem, callIDParam(c))
	if err != nil {
		return callActionError(c, err, "hangup_call_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromCall(call))
}

func (h *Handler) SendDTMF(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "send_dtmf_failed")
	}
	var req SendDTMFRequest
	if err := httpapi.BindJSON(c, &req, "send_dtmf_invalid_request"); err != nil {
		return err
	}
	if err := h.calls.SendDTMFForClient(c.Request().Context(), modem, ClientDTMF{
		CallID:   callIDParam(c),
		ClientID: req.ClientID,
		Digits:   req.Digits,
	}); err != nil {
		return callActionError(c, err, "send_dtmf_failed")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_call_failed")
	}
	if err := h.calls.Delete(c.Request().Context(), modem, callIDParam(c)); err != nil {
		return callActionError(c, err, "delete_call_failed")
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
		return httpapi.ModemLookupError(c, err, "subscribe_call_events_failed")
	}
	events, unsubscribe := h.calls.Subscribe(SubscriptionConfig{ModemID: modem.EquipmentIdentifier, Buffer: 16})
	defer unsubscribe()
	dtmfEvents, unsubscribeDTMF := h.calls.Subscribe(SubscriptionConfig{
		Kind: EventKindDTMF, ModemID: modem.EquipmentIdentifier, Buffer: 16,
	})
	defer unsubscribeDTMF()
	currentCalls, err := h.calls.List(c.Request().Context(), modem, "")
	if err != nil {
		return httpapi.Internal(c, "subscribe_call_events_failed", err)
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
			if err := writeCallEvent(conn, event); err != nil {
				return nil
			}
		case event, ok := <-dtmfEvents:
			if !ok {
				return nil
			}
			if err := writeCallEvent(conn, event); err != nil {
				return nil
			}
		}
	}
}

func (h *Handler) WebRTCSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "call_media_unavailable")
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
			session.CloseIfNotConnected(ctx)
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
			nextSession, err := h.media.OpenClientSession(ctx, modem, ClientMedia{CallID: callID, ClientID: message.ClientID})
			if err != nil {
				code := "call_media_unavailable"
				if errors.Is(err, ErrCallInUse) {
					code = "call_in_use"
				}
				// The connection is closing; a second write cannot recover this error.
				_ = writeWebRTCSignal(conn, &writeMu, WebRTCSignalMessage{Type: "error", Code: code, Message: callMediaMessage(err)})
				return nil
			}
			session = nextSession
			go writeWebRTCICECandidates(ctx, conn, &writeMu, session.ICECandidates())
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

func writeCallEvent(conn *websocket.Conn, event Event) error {
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if event.DTMF != nil {
		return conn.WriteJSON(DTMFMessage{
			Type:   "dtmf",
			CallID: event.Call.ID,
			Digit:  event.DTMF.Digit,
			At:     event.DTMF.At.Format(time.RFC3339Nano),
		})
	}
	return conn.WriteJSON(EventMessage{Type: "call", Call: ResponseFromCall(event.Call)})
}

func writeWebRTCICECandidates(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, candidates <-chan WebRTCICECandidate) {
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
		if err := writeCallEvent(conn, Event{Call: call}); err != nil {
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
	case errors.Is(err, ErrCallInUse):
		return httpapi.Error(c, http.StatusConflict, "call_in_use", err.Error())
	case errors.Is(err, ErrCallProfileChanged):
		return httpapi.Error(c, http.StatusConflict, "call_profile_changed", err.Error())
	case errors.Is(err, ErrInvalidCallClient):
		return httpapi.BadRequest(c, "invalid_call_client", err)
	case errors.Is(err, ErrNumberRequired):
		return httpapi.BadRequest(c, "call_number_required", err)
	case errors.Is(err, ErrInvalidNumber):
		return httpapi.BadRequest(c, "call_number_invalid", err)
	case errors.Is(err, ErrUSSDDialString):
		return httpapi.BadRequest(c, "ussd_dial_string", err)
	case errors.Is(err, ErrInvalidRoute):
		return httpapi.BadRequest(c, "invalid_call_route", err)
	case errors.Is(err, ErrNoRouteAvailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, "no_call_route_available", err.Error())
	case errors.Is(err, ErrWiFiCallingNotConnected):
		return httpapi.Error(c, http.StatusServiceUnavailable, "wifi_calling_not_connected", err.Error())
	case errors.Is(err, ErrVoLTENotConnected):
		return httpapi.Error(c, http.StatusServiceUnavailable, "volte_not_connected", err.Error())
	case errors.Is(err, ErrModemCallingUnavailable):
		return httpapi.Error(c, http.StatusNotImplemented, "modem_calling_unavailable", err.Error())
	case errors.Is(err, ErrCallNotFound), errors.Is(err, storage.ErrNotFound):
		return httpapi.NotFound(c, "call_not_found", err)
	case errors.Is(err, ErrInvalidCallState):
		return httpapi.BadRequest(c, "invalid_call_state", err)
	case errors.Is(err, ErrInvalidCallHold):
		return httpapi.BadRequest(c, "invalid_call_hold", err)
	case errors.Is(err, ErrCallUpdateConflict):
		return httpapi.BadRequest(c, "call_update_conflict", err)
	case errors.Is(err, ErrDTMFDigitsRequired):
		return httpapi.BadRequest(c, "dtmf_digits_required", err)
	case errors.Is(err, ErrInvalidDTMFDigit):
		return httpapi.BadRequest(c, "invalid_dtmf_digit", err)
	case errors.Is(err, ErrInvalidDTMFCallState):
		return httpapi.Error(c, http.StatusConflict, "invalid_dtmf_call_state", err.Error())
	case errors.Is(err, ErrCallOnHold):
		return httpapi.Error(c, http.StatusConflict, "call_on_hold", err.Error())
	case errors.Is(err, ErrCallRecordActive):
		return httpapi.Error(c, http.StatusConflict, "call_record_active", err.Error())
	case errors.Is(err, ErrUnsupportedDTMF):
		return httpapi.Error(c, http.StatusNotImplemented, "call_dtmf_unsupported", err.Error())
	case fallback == "dial_call_failed":
		return httpapi.Error(c, http.StatusBadGateway, "dial_call_failed", callActionMessage(err))
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
		return httpapi.Error(c, http.StatusUnsupportedMediaType, "call_media_unsupported_codec", err.Error())
	case errors.Is(err, ErrMediaUnavailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, "call_media_unavailable", err.Error())
	default:
		return callActionError(c, err, "call_media_unavailable")
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
		LineLabel: call.LineLabel, ModemID: call.ModemID, HistoryProfileID: call.HistoryProfileID,
		BackendID:     call.BackendID,
		CallUUID:      call.CallUUID,
		ProfileID:     call.ProfileID,
		OwnerClientID: call.OwnerClientID,
		ID:            call.ID,
		Route:         call.Route,
		Direction:     call.Direction,
		Number:        call.Number,
		State:         call.State,
		Hold:          hold,
		Reason:        call.Reason,
		StartedAt:     callTime(call.StartedAt),
		AnsweredAt:    callTime(call.AnsweredAt),
		EndedAt:       callTime(call.EndedAt),
		UpdatedAt:     callTime(call.UpdatedAt),
	}
}

func callTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}
