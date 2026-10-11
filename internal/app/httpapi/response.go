package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/apierror"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// ErrorResponse is the Problem Details representation of a local API failure.
type ErrorResponse = apierror.Problem

// Error writes a registered problem, preserving resource-specific HTTP headers.
func Error(c *echo.Context, status int, errorCode, message string) error {
	title, known := apierror.Title(errorCode)
	if !known {
		slog.Error("unregistered HTTP problem", "error_code", errorCode)
		errorCode = "remote_error"
		title = "Remote Error"
	}
	headers := c.Response().Header()
	headers.Set("Cache-Control", "no-store")
	headers.Set("Content-Type", apierror.MediaType+"; charset=utf-8")
	headers.Set("X-Content-Type-Options", "nosniff")
	id := requestID(c)
	if id == "" {
		id = rand.Text()
	}
	headers.Set("X-Request-ID", id)
	if status == http.StatusUnauthorized && headers.Get("WWW-Authenticate") == "" {
		headers.Set("WWW-Authenticate", `Bearer realm="sigmo-local"`)
	}
	if c.Request().Method == http.MethodHead || status == http.StatusNoContent {
		return c.NoContent(status)
	}
	return c.JSON(status, ErrorResponse{
		Type:      apierror.TypePrefix + errorCode,
		Title:     title,
		Status:    status,
		ErrorCode: errorCode,
		Detail:    message,
		RequestID: id,
	})
}

// ResponseHeaders prevents caching API success responses as well as failures.
// Download handlers can set a more specific private/no-store policy.
func ResponseHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			c.Response().Header().Set("Cache-Control", "no-store")
		}
		return next(c)
	}
}

// HTTPErrorHandler gives framework errors the same representation as business
// errors. Both the activation server and the regular server install this exit.
func HTTPErrorHandler(c *echo.Context, err error) {
	if response, _ := echo.UnwrapResponse(c.Response()); response != nil && response.Committed {
		return
	}
	status := http.StatusInternalServerError
	var coded echo.HTTPStatusCoder
	if errors.As(err, &coded) {
		if code := coded.StatusCode(); code >= 400 && code <= 599 {
			status = code
		}
	}
	code := statusErrorCode(status)
	if status >= 500 {
		logInternalError(c, code, err)
	}
	if writeErr := Error(c, status, code, http.StatusText(status)); writeErr != nil {
		slog.Error("write HTTP problem", "error", writeErr, "request_id", requestID(c))
	}
}

func statusErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authorization_required"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "resource_not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout:
		return "request_timeout"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "request_body_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	case http.StatusInternalServerError:
		return "internal_server_error"
	case http.StatusNotImplemented:
		return "method_not_implemented"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusGatewayTimeout:
		return "gateway_timeout"
	default:
		return "remote_error"
	}
}

// BadRequest reports malformed client input.
func BadRequest(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusBadRequest, errorCode, err.Error())
}

// Unauthorized reports invalid credentials for the local resource.
func Unauthorized(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusUnauthorized, errorCode, err.Error())
}

// NotFound reports a missing business resource.
func NotFound(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusNotFound, errorCode, err.Error())
}

// ModemLookupError distinguishes an absent modem from a lookup failure.
func ModemLookupError(c *echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, mmodem.ErrNotFound) {
		return NotFound(c, "modem_not_found", err)
	}
	return Internal(c, internalErrorCode, err)
}

// RequestTimeout reports an expired client operation.
func RequestTimeout(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusRequestTimeout, errorCode, err.Error())
}

// TooManyRequests reports a limit specific to the caller.
func TooManyRequests(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusTooManyRequests, errorCode, err.Error())
}

// UnprocessableEntity reports valid JSON with invalid fields or semantics.
func UnprocessableEntity(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusUnprocessableEntity, errorCode, err.Error())
}

// Internal keeps diagnostic causes in server logs, including in debug mode.
func Internal(c *echo.Context, errorCode string, err error) error {
	logInternalError(c, errorCode, err)
	return Error(c, http.StatusInternalServerError, errorCode, "internal server error")
}

func logInternalError(c *echo.Context, errorCode string, err error) {
	req := c.Request()
	slog.Error("http internal error",
		"error", err,
		"error_code", errorCode,
		"request_id", requestID(c),
		"method", req.Method,
		"route", c.Path(),
		"host", req.Host,
		"remote_ip", c.RealIP(),
		"imei", c.Param("id"),
	)
}

// BindAndValidate returns a non-nil error after writing a rejected request so
// callers cannot continue into business logic after a successful error write.
func BindAndValidate[T any](c *echo.Context, dst *T, errorCode string) error {
	if err := BindJSON(c, dst, errorCode); err != nil {
		return err
	}
	if err := c.Validate(dst); err != nil {
		return rejectRequest(c, http.StatusUnprocessableEntity, errorCode, err)
	}
	return nil
}

// BindJSON decodes one bounded, non-null JSON document. Semantic validation remains with
// the caller; request data is never included in parser error details.
func BindJSON[T any](c *echo.Context, dst *T, errorCode string) error {
	req := c.Request()
	if req.ContentLength > apierror.MaxBodySize {
		return rejectRequest(c, http.StatusRequestEntityTooLarge, "request_body_too_large", errors.New("request body exceeds size limit"))
	}
	if req.Body == nil || req.ContentLength == 0 {
		return rejectRequest(c, http.StatusBadRequest, errorCode, errors.New("JSON body is required"))
	}
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return rejectRequest(c, http.StatusUnsupportedMediaType, "unsupported_media_type", errors.New("request requires application/json"))
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, apierror.MaxBodySize+1))
	if err != nil {
		return rejectRequest(c, http.StatusBadRequest, errorCode, errors.New("read JSON body"))
	}
	if len(data) > apierror.MaxBodySize {
		return rejectRequest(c, http.StatusRequestEntityTooLarge, "request_body_too_large", errors.New("request body exceeds size limit"))
	}
	// Unmarshaling null into a struct succeeds and leaves every field unchanged.
	// Reject it before a settings update can mistake zero values for user input.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return rejectRequest(c, http.StatusUnprocessableEntity, errorCode, errors.New("JSON body must not be null"))
	}
	if err := json.Unmarshal(data, dst); err != nil {
		var invalidType *json.UnmarshalTypeError
		if errors.As(err, &invalidType) {
			return rejectRequest(c, http.StatusUnprocessableEntity, errorCode, errors.New("invalid JSON field type"))
		}
		return rejectRequest(c, http.StatusBadRequest, errorCode, errors.New("invalid JSON body"))
	}
	return nil
}

func rejectRequest(c *echo.Context, status int, code string, cause error) error {
	if err := Error(c, status, code, cause.Error()); err != nil {
		return fmt.Errorf("write invalid request response: %w", err)
	}
	return cause
}

func requestID(c *echo.Context) string {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID != "" {
		return requestID
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}
