package call

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// HistoryHandler serves archived calls without live modem or media dependencies.
type HistoryHandler struct{ history *History }

// NewHistoryHandler creates archived call endpoints.
func NewHistoryHandler(history *History) *HistoryHandler { return &HistoryHandler{history: history} }

// History lists archived calls across SIM profiles.
func (h *HistoryHandler) History(c *echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	items, cursor, err := h.history.List(c.Request().Context(), storage.HistoryFilter{ModemID: c.QueryParam("modemId"), ProfileID: c.QueryParam("profileId"), Query: c.QueryParam("q"), Cursor: c.QueryParam("cursor"), Limit: limit, Missed: c.QueryParam("missed") == "true"})
	if errors.Is(err, storage.ErrInvalidHistoryCursor) {
		return httpapi.BadRequest(c, "communication_invalid", err)
	}
	if err != nil {
		return httpapi.Internal(c, "list_calls_failed", err)
	}
	return c.JSON(http.StatusOK, struct {
		Items      []CallResponse `json:"items"`
		NextCursor string         `json:"nextCursor"`
	}{ResponsesFromCalls(items), cursor})
}

// HistoryRecord returns one archived call.
func (h *HistoryHandler) HistoryRecord(c *echo.Context) error {
	record, err := h.history.Get(c.Request().Context(), c.Param("callID"))
	if err != nil {
		return callActionError(c, err, "list_calls_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromCall(record))
}

// DeleteHistory removes a terminal call from history.
func (h *HistoryHandler) DeleteHistory(c *echo.Context) error {
	if err := h.history.Delete(c.Request().Context(), c.Param("callID")); err != nil {
		return callActionError(c, err, "delete_call_failed")
	}
	return c.NoContent(http.StatusNoContent)
}
