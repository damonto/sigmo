package communication

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/simprofile"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// SIMProfileHandler exposes inventory, communication preferences, and history handovers.
type SIMProfileHandler struct{ service *simprofile.Service }

// NewSIMProfileHandler creates SIM profile endpoints using the shared inventory service.
func NewSIMProfileHandler(service *simprofile.Service) *SIMProfileHandler {
	return &SIMProfileHandler{service: service}
}

// Lines lists SIM profiles with their current communication availability.
func (h *SIMProfileHandler) Lines(c *echo.Context) error {
	lines, err := h.service.List(c.Request().Context(), c.QueryParam("cached") != "true")
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []storage.SIMLine `json:"items"`
	}{lines})
}

// Preference returns the preferred SIM profile for a participant.
func (h *SIMProfileHandler) Preference(c *echo.Context) error {
	preference, err := h.service.Preference(c.Request().Context(), c.QueryParam("participant"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, preference)
}

// AssignHistory transfers archived communication history between SIM profiles.
func (h *SIMProfileHandler) AssignHistory(c *echo.Context) error {
	var body struct {
		TargetProfileID string `json:"targetProfileId"`
	}
	if err := httpapi.BindJSON(c, &body, "history_assignment_invalid"); err != nil {
		return err
	}
	if err := h.service.AssignHistory(c.Request().Context(), c.Param("profileId"), body.TargetProfileID); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
