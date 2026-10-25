package call

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func (h *Handler) history(c *echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	items, cursor, err := h.calls.records.store.CallHistory(c.Request().Context(), storage.HistoryFilter{ModemID: c.QueryParam("modemId"), ProfileID: c.QueryParam("profileId"), Query: c.QueryParam("q"), Cursor: c.QueryParam("cursor"), Limit: limit, Missed: c.QueryParam("missed") == "true"})
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
func (h *Handler) historyRecord(c *echo.Context) error {
	record, err := h.calls.records.store.Call(c.Request().Context(), c.Param("callID"))
	if err != nil {
		return callActionError(c, err, "list_calls_failed")
	}
	return c.JSON(http.StatusOK, ResponseFromCall(record))
}
func (h *Handler) deleteHistory(c *echo.Context) error {
	record, err := h.calls.records.store.Call(c.Request().Context(), c.Param("callID"))
	if err != nil {
		return callActionError(c, err, "delete_call_failed")
	}
	if err := h.calls.records.deleteCall(c.Request().Context(), record); err != nil {
		return callActionError(c, err, "delete_call_failed")
	}
	return c.NoContent(http.StatusNoContent)
}
