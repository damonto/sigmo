package communication

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func writeError(c *echo.Context, err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return httpapi.NotFound(c, "communication_not_found", err)
	}
	if errors.Is(err, storage.ErrInvalidContact) || errors.Is(err, storage.ErrInvalidReadUpdate) || errors.Is(err, storage.ErrInvalidHistoryCursor) {
		return httpapi.BadRequest(c, "communication_invalid", err)
	}
	if errors.Is(err, storage.ErrInvalidAssignment) {
		return httpapi.Error(c, http.StatusConflict, "history_assignment_invalid", err.Error())
	}
	return httpapi.Internal(c, "communication_failed", err)
}
