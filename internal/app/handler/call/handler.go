package call

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pcall "github.com/damonto/sigmo/internal/pkg/call"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Handler struct {
	registry *mmodem.Registry
	calls    *pcall.Service
}

const (
	errorCodeListCallsFailed           = "list_calls_failed"
	errorCodeDialCallInvalidRequest    = "dial_call_invalid_request"
	errorCodeDialCallFailed            = "dial_call_failed"
	errorCodeUpdateCallInvalidRequest  = "update_call_invalid_request"
	errorCodeUpdateCallFailed          = "update_call_failed"
	errorCodeCallNumberRequired        = "call_number_required"
	errorCodeCallNumberInvalid         = "call_number_invalid"
	errorCodeUSSDDialString            = "ussd_dial_string"
	errorCodeInvalidCallRoute          = "invalid_call_route"
	errorCodeNoCallRouteAvailable      = "no_call_route_available"
	errorCodeWiFiCallingNotConnected   = "wifi_calling_not_connected"
	errorCodeModemCallingUnavailable   = "modem_calling_unavailable"
	errorCodeCallNotFound              = "call_not_found"
	errorCodeInvalidCallState          = "invalid_call_state"
	errorCodeHangupCallFailed          = "hangup_call_failed"
	errorCodeCallMediaUnavailable      = "call_media_unavailable"
	errorCodeCallMediaUnsupportedCodec = "call_media_unsupported_codec"
	errorCodeSubscribeCallEventsFailed = "subscribe_call_events_failed"
)

var callWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return sameOrigin(r)
	},
}

func New(registry *mmodem.Registry, calls *pcall.Service) *Handler {
	return &Handler{registry: registry, calls: calls}
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListCallsFailed)
	}
	calls, err := h.calls.List(c.Request().Context(), modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListCallsFailed, err)
	}
	return c.JSON(http.StatusOK, buildCallResponses(calls))
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
	return c.JSON(http.StatusCreated, buildCallResponse(call))
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
	call, err := h.calls.Update(c.Request().Context(), modem, callIDParam(c), req.State, req.Reason)
	if err != nil {
		return callActionError(c, err, errorCodeUpdateCallFailed)
	}
	return c.JSON(http.StatusOK, buildCallResponse(call))
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
	return c.JSON(http.StatusOK, buildCallResponse(call))
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
	currentCalls, err := h.calls.List(c.Request().Context(), modem)
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

func (h *Handler) Media(c *echo.Context) error {
	conn, err := callWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeMediaError(conn, modemLookupMediaError(c, err))
	}
	media, err := h.calls.OpenMedia(c.Request().Context(), modem, callIDParam(c))
	if err != nil {
		return writeMediaError(conn, callMediaErrorResponse(c, err))
	}

	conn.SetReadLimit(64 * 1024)
	info := buildMediaInfoResponse(media.Info())
	if err := conn.WriteJSON(MediaMessage{Type: "ready", Media: &info}); err != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	errCh := make(chan error, 2)
	packetCh := make(chan []byte, 16)
	go readBrowserMedia(ctx, conn, media, errCh)
	go readCallMedia(ctx, media, packetCh, errCh)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-errCh:
			return nil
		case packet := <-packetCh:
			if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
				return nil
			}
		}
	}
}

func readBrowserMedia(ctx context.Context, conn *websocket.Conn, media pcall.MediaSession, errCh chan<- error) {
	for {
		messageType, packet, err := conn.ReadMessage()
		if err != nil {
			deliverMediaError(ctx, errCh, err)
			return
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		if err := media.WritePacket(ctx, packet); err != nil {
			deliverMediaError(ctx, errCh, err)
			return
		}
	}
}

func readCallMedia(ctx context.Context, media pcall.MediaSession, packetCh chan<- []byte, errCh chan<- error) {
	for {
		packet, err := media.ReadPacket(ctx)
		if err != nil {
			deliverMediaError(ctx, errCh, err)
			return
		}
		select {
		case packetCh <- packet:
		case <-ctx.Done():
			return
		}
	}
}

func deliverMediaError(ctx context.Context, errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-ctx.Done():
	default:
	}
}

func writeMediaError(conn *websocket.Conn, response mediaErrorResponse) error {
	message := MediaMessage{
		Type: "error",
		Error: &httpapi.ErrorResponse{
			ErrorCode: response.code,
			Message:   response.message,
			RequestID: response.requestID,
		},
	}
	if err := conn.WriteJSON(message); err != nil {
		return nil
	}
	closeMessage := websocket.FormatCloseMessage(websocket.CloseUnsupportedData, response.message)
	_ = conn.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(time.Second))
	return nil
}

func writeCallEvent(conn *websocket.Conn, call storage.Call) error {
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(EventMessage{Type: "call", Call: buildCallResponse(call)})
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

func isTerminalCallState(state string) bool {
	return state == pcall.StateEnded || state == pcall.StateFailed
}

func callActionError(c *echo.Context, err error, fallback string) error {
	switch {
	case errors.Is(err, pcall.ErrNumberRequired):
		return httpapi.BadRequest(c, errorCodeCallNumberRequired, err)
	case errors.Is(err, pcall.ErrInvalidNumber):
		return httpapi.BadRequest(c, errorCodeCallNumberInvalid, err)
	case errors.Is(err, pcall.ErrUSSDDialString):
		return httpapi.BadRequest(c, errorCodeUSSDDialString, err)
	case errors.Is(err, pcall.ErrInvalidRoute):
		return httpapi.BadRequest(c, errorCodeInvalidCallRoute, err)
	case errors.Is(err, pcall.ErrNoRouteAvailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeNoCallRouteAvailable, err.Error())
	case errors.Is(err, pcall.ErrWiFiCallingNotConnected):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeWiFiCallingNotConnected, err.Error())
	case errors.Is(err, pcall.ErrModemCallingUnavailable):
		return httpapi.Error(c, http.StatusNotImplemented, errorCodeModemCallingUnavailable, err.Error())
	case errors.Is(err, pcall.ErrCallNotFound):
		return httpapi.NotFound(c, errorCodeCallNotFound, err)
	case errors.Is(err, pcall.ErrInvalidCallState):
		return httpapi.BadRequest(c, errorCodeInvalidCallState, err)
	case fallback == errorCodeDialCallFailed:
		return httpapi.Error(c, http.StatusBadGateway, errorCodeDialCallFailed, callActionMessage(err))
	default:
		return httpapi.Internal(c, fallback, err)
	}
}

func callActionMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	message = strings.TrimPrefix(message, "dial Wi-Fi Calling: ")
	if message == "" {
		return "call failed"
	}
	return message
}

func callMediaError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, pcall.ErrUnsupportedCodec):
		return httpapi.Error(c, http.StatusUnsupportedMediaType, errorCodeCallMediaUnsupportedCodec, err.Error())
	case errors.Is(err, pcall.ErrMediaUnavailable):
		return httpapi.Error(c, http.StatusServiceUnavailable, errorCodeCallMediaUnavailable, err.Error())
	default:
		return callActionError(c, err, errorCodeCallMediaUnavailable)
	}
}

type mediaErrorResponse struct {
	code      string
	message   string
	requestID string
}

func modemLookupMediaError(c *echo.Context, err error) mediaErrorResponse {
	if errors.Is(err, mmodem.ErrNotFound) {
		return newMediaErrorResponse(c, "modem_not_found", err.Error())
	}
	return newMediaErrorResponse(c, errorCodeCallMediaUnavailable, "internal server error")
}

func callMediaErrorResponse(c *echo.Context, err error) mediaErrorResponse {
	switch {
	case errors.Is(err, pcall.ErrUnsupportedCodec):
		return newMediaErrorResponse(c, errorCodeCallMediaUnsupportedCodec, err.Error())
	case errors.Is(err, pcall.ErrMediaUnavailable):
		return newMediaErrorResponse(c, errorCodeCallMediaUnavailable, err.Error())
	case errors.Is(err, pcall.ErrWiFiCallingNotConnected):
		return newMediaErrorResponse(c, errorCodeWiFiCallingNotConnected, err.Error())
	case errors.Is(err, pcall.ErrModemCallingUnavailable):
		return newMediaErrorResponse(c, errorCodeModemCallingUnavailable, err.Error())
	case errors.Is(err, pcall.ErrCallNotFound):
		return newMediaErrorResponse(c, errorCodeCallNotFound, err.Error())
	case errors.Is(err, pcall.ErrInvalidCallState):
		return newMediaErrorResponse(c, errorCodeInvalidCallState, err.Error())
	default:
		return newMediaErrorResponse(c, errorCodeCallMediaUnavailable, "internal server error")
	}
}

func newMediaErrorResponse(c *echo.Context, code string, message string) mediaErrorResponse {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID == "" {
		requestID = c.Request().Header.Get(echo.HeaderXRequestID)
	}
	return mediaErrorResponse{
		code:      code,
		message:   message,
		requestID: requestID,
	}
}

func buildCallResponses(calls []storage.Call) []CallResponse {
	response := make([]CallResponse, 0, len(calls))
	for _, call := range calls {
		response = append(response, buildCallResponse(call))
	}
	return response
}

func buildCallResponse(call storage.Call) CallResponse {
	return CallResponse{
		ID:         call.ID,
		Route:      call.Route,
		Direction:  call.Direction,
		Number:     call.Number,
		State:      call.State,
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

func buildMediaInfoResponse(info pcall.MediaInfo) MediaInfoResponse {
	return MediaInfoResponse{
		Codec:           info.Codec,
		PayloadType:     info.PayloadType,
		ClockRate:       info.ClockRate,
		Channels:        info.Channels,
		OctetAlign:      info.OctetAlign,
		DTMFPayloadType: info.DTMFPayloadType,
		DTMFClockRate:   info.DTMFClockRate,
		PTimeMillis:     info.PTimeMillis,
	}
}
