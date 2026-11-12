package httpapi

import (
	"errors"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/lpa"
)

// LPAError maps secure-element lookup failures and writes exactly one response.
func LPAError(c *echo.Context, err error, internalErrorCode string) error {
	switch {
	case errors.Is(err, lpa.ErrSERequired):
		return BadRequest(c, "se_required", err)
	case errors.Is(err, lpa.ErrSENotFound):
		return NotFound(c, "se_not_found", err)
	case errors.Is(err, lpa.ErrNoSupportedAID):
		return NotFound(c, "euicc_not_supported", err)
	default:
		return Internal(c, internalErrorCode, err)
	}
}
