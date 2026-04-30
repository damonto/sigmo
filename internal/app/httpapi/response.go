package httpapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type ErrorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func Error(c *echo.Context, status int, errorCode, message string) error {
	return c.JSON(status, ErrorResponse{
		ErrorCode: errorCode,
		Message:   message,
		RequestID: requestID(c),
	})
}

func BadRequest(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusBadRequest, errorCode, err.Error())
}

func Unauthorized(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusUnauthorized, errorCode, err.Error())
}

func NotFound(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusNotFound, errorCode, err.Error())
}

func ModemLookupError(c *echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, mmodem.ErrNotFound) {
		return NotFound(c, "modem_not_found", err)
	}
	return Internal(c, internalErrorCode)
}

func RequestTimeout(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusRequestTimeout, errorCode, err.Error())
}

func TooManyRequests(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusTooManyRequests, errorCode, err.Error())
}

func UnprocessableEntity(c *echo.Context, errorCode string, err error) error {
	return Error(c, http.StatusUnprocessableEntity, errorCode, err.Error())
}

func Internal(c *echo.Context, errorCode string) error {
	return Error(c, http.StatusInternalServerError, errorCode, "internal server error")
}

func BindAndValidate[T any](c *echo.Context, dst *T, errorCode string) error {
	if err := c.Bind(dst); err != nil {
		return BadRequest(c, errorCode, err)
	}
	if err := c.Validate(dst); err != nil {
		return UnprocessableEntity(c, errorCode, err)
	}
	return nil
}

func requestID(c *echo.Context) string {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID != "" {
		return requestID
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}
