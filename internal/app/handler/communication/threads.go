package communication

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// ThreadHandler serves archived message threads independently of live modems.
type ThreadHandler struct{ store *storage.Store }

// NewThreadHandler creates archive endpoints backed only by persistence.
func NewThreadHandler(store *storage.Store) *ThreadHandler { return &ThreadHandler{store: store} }

func historyFilter(c *echo.Context) storage.HistoryFilter {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	return storage.HistoryFilter{ModemID: c.QueryParam("modemId"), ProfileID: c.QueryParam("profileId"), Query: c.QueryParam("q"), Cursor: c.QueryParam("cursor"), Limit: limit}
}

// Threads lists archived message threads or resolves a live conversation.
func (h *ThreadHandler) Threads(c *echo.Context) error {
	if id := c.QueryParam("conversationId"); id != "" {
		thread, err := h.store.ThreadForConversation(c.Request().Context(), id)
		if err != nil {
			return writeError(c, err)
		}
		return c.JSON(http.StatusOK, struct {
			ID string `json:"id"`
		}{thread})
	}
	page, err := h.store.MessageThreads(c.Request().Context(), historyFilter(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, page)
}

// Thread returns one archived message thread.
func (h *ThreadHandler) Thread(c *echo.Context) error {
	thread, err := h.store.Thread(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, thread)
}

// ThreadMessages lists messages in an archived thread.
func (h *ThreadHandler) ThreadMessages(c *echo.Context) error {
	page, err := h.store.ThreadMessages(c.Request().Context(), c.Param("id"), historyFilter(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, page)
}

// ReadThread updates a thread's read state and returns the message badge state.
func (h *ThreadHandler) ReadThread(c *echo.Context) error {
	var read storage.ThreadRead
	if err := httpapi.BindJSON(c, &read, "thread_read_invalid"); err != nil {
		return err
	}
	if err := h.store.ReadThread(c.Request().Context(), c.Param("id"), read); err != nil {
		return writeError(c, err)
	}
	badge, err := h.store.MessagesState(c.Request().Context())
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, badge)
}

// DeleteThread removes an archived message thread.
func (h *ThreadHandler) DeleteThread(c *echo.Context) error {
	if err := h.store.DeleteThread(c.Request().Context(), c.Param("id")); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
