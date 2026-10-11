package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/apierror"
)

// UpstreamError adapts the Pro service's resources to the local API. Both
// pairing and update requests use this boundary; it never invalidates OTP login
// or advertises an upstream resource's Allow or authentication challenge.
func UpstreamError(c *echo.Context, fallbackCode string, err error) error {
	var remote *apierror.Error
	if errors.As(err, &remote) {
		status, code := upstreamStatus(remote)
		slog.WarnContext(c.Request().Context(), "upstream API response",
			"request_id", requestID(c), "upstream_request_id", remote.RequestID,
			"upstream_status", remote.StatusCode, "upstream_type", remote.ProblemType,
			"upstream_code", remote.ErrorCode, "upstream_allow", remote.Allow,
			"response_error", remote.ParseErr, "status", status)
		if (status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable) && !remote.RetryAt.IsZero() {
			c.Response().Header().Set("Retry-After", remote.RetryAfter)
		}
		title, _ := apierror.Title(code) // Every mapping below belongs to the catalog.
		return Error(c, status, code, title)
	}
	var networkErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()) {
		return Error(c, http.StatusGatewayTimeout, "upstream_timeout", "upstream service timed out")
	}
	slog.WarnContext(c.Request().Context(), "contact upstream API", "error", err, "request_id", requestID(c))
	return Error(c, http.StatusBadGateway, fallbackCode, "upstream request did not complete")
}

func upstreamStatus(remote *apierror.Error) (int, string) {
	if remote.StatusCode == http.StatusMethodNotAllowed ||
		(remote.StatusCode == http.StatusNotFound && remote.ErrorCode == "resource_not_found") {
		return http.StatusBadGateway, "upstream_contract_error"
	}
	if remote.ParseErr != nil {
		return http.StatusBadGateway, "upstream_response_invalid"
	}
	// Only explicitly defined business failures survive the boundary. Actual
	// HTTP status must agree before a code is trusted for business decisions.
	switch remote.ErrorCode {
	case "license_pairing_not_found":
		if remote.StatusCode == http.StatusNotFound {
			return http.StatusNotFound, remote.ErrorCode
		}
	case "license_pairing_expired":
		if remote.StatusCode == http.StatusGone {
			return http.StatusGone, remote.ErrorCode
		}
	case "license_device_unauthorized", "license_entitlement_inactive":
		if remote.StatusCode == http.StatusForbidden {
			return http.StatusForbidden, remote.ErrorCode
		}
	case "license_lease_expired":
		if remote.StatusCode == http.StatusUnauthorized || remote.StatusCode == http.StatusForbidden {
			return http.StatusForbidden, remote.ErrorCode
		}
	case "license_session_superseded":
		if remote.StatusCode == http.StatusConflict {
			return http.StatusConflict, remote.ErrorCode
		}
	case "license_pairing_limit_exceeded":
		if remote.StatusCode == http.StatusTooManyRequests {
			return http.StatusTooManyRequests, remote.ErrorCode
		}
	}
	switch remote.StatusCode {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return http.StatusBadGateway, "upstream_request_invalid"
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusBadGateway, "upstream_authentication_failed"
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return http.StatusGatewayTimeout, "upstream_timeout"
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return http.StatusServiceUnavailable, "upstream_unavailable"
	default:
		return http.StatusBadGateway, "upstream_response_invalid"
	}
}
