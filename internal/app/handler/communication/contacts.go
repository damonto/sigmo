// Package communication serves backend-owned contacts, archived messages and SIM history.
package communication

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // Accept camera images after client-side orientation and cropping.
	"image/png"
	"io"
	"net/http"
	"strconv"

	"github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

// Config keeps persistence and live inventory in their existing ownership boundaries.
type Config struct {
	Storage  *storage.Store
	Registry *modem.Registry
	ESIM     *esim.Handler
}

// Handler exposes authenticated, backend-scoped communication resources.
type Handler struct{ config Config }

// New uses the same store and modem registry as the existing device routes.
func New(config Config) *Handler { return &Handler{config: config} }

// Register adds public-module routes to the existing protected Echo group.
func (h *Handler) Register(group *echo.Group) {
	group.GET("/contacts", h.contacts)
	group.POST("/contacts", h.saveContact)
	group.GET("/contacts/:id", h.contact)
	group.PUT("/contacts/:id", h.saveContact)
	group.DELETE("/contacts/:id", h.deleteContact)
	group.GET("/contacts/:id/avatar", h.avatar)
	group.PUT("/contacts/:id/avatar", h.setAvatar)
	group.DELETE("/contacts/:id/avatar", h.setAvatar)
	group.GET("/message-threads", h.threads)
	group.GET("/message-threads/:id", h.thread)
	group.GET("/message-threads/:id/messages", h.threadMessages)
	group.PATCH("/message-threads/:id", h.readThread)
	group.DELETE("/message-threads/:id", h.deleteThread)
	group.GET("/sim-profiles", h.lines)
	group.GET("/communication-preferences", h.preference)
	group.PUT("/sim-profiles/:profileId/history-assignment", h.assign)
}

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

func (h *Handler) contacts(c *echo.Context) error {
	items, err := h.config.Storage.Contacts(c.Request().Context(), c.QueryParam("q"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []storage.Contact `json:"items"`
	}{items})
}
func (h *Handler) contact(c *echo.Context) error {
	item, err := h.config.Storage.Contact(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}
func (h *Handler) saveContact(c *echo.Context) error {
	var item storage.Contact
	if err := httpapi.BindJSON(c, &item, "contact_invalid"); err != nil {
		return err
	}
	item.ID = c.Param("id")
	item, err := h.config.Storage.SaveContact(c.Request().Context(), item)
	if err != nil {
		return writeError(c, err)
	}
	status := http.StatusOK
	if c.Request().Method == http.MethodPost {
		status = http.StatusCreated
	}
	return c.JSON(status, item)
}
func (h *Handler) deleteContact(c *echo.Context) error {
	if err := h.config.Storage.DeleteContact(c.Request().Context(), c.Param("id")); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
func (h *Handler) avatar(c *echo.Context) error {
	data, err := h.config.Storage.ContactAvatar(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-cache")
	return c.Blob(http.StatusOK, "image/png", data)
}
func (h *Handler) setAvatar(c *echo.Context) error {
	var data []byte
	var tint string
	if c.Request().Method != http.MethodDelete {
		const maxBytes = 5 << 20
		input, err := io.ReadAll(io.LimitReader(c.Request().Body, maxBytes+1))
		if err != nil {
			return httpapi.BadRequest(c, "avatar_invalid", err)
		}
		if len(input) > maxBytes {
			return httpapi.Error(c, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar exceeds 5 MiB")
		}
		data, tint, err = thumbnail(input)
		if err != nil {
			return httpapi.BadRequest(c, "avatar_invalid", err)
		}
	}
	contact, err := h.config.Storage.SetContactAvatar(c.Request().Context(), c.Param("id"), data, tint)
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, contact)
}

func thumbnail(input []byte) ([]byte, string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil {
		return nil, "", fmt.Errorf("decode avatar: %w", err)
	}
	if (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return nil, "", errors.New("avatar must be a PNG or JPEG no larger than 4096 pixels per side")
	}
	source, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, "", fmt.Errorf("decode avatar: %w", err)
	}
	side := min(config.Width, config.Height)
	size := min(side, 512)
	dest := image.NewNRGBA(image.Rect(0, 0, size, size))
	var bins [4096]int
	for y := range size {
		for x := range size {
			pixel := color.NRGBAModel.Convert(source.At(source.Bounds().Min.X+(config.Width-side)/2+x*side/size, source.Bounds().Min.Y+(config.Height-side)/2+y*side/size)).(color.NRGBA)
			dest.SetNRGBA(x, y, pixel)
			if pixel.A > 127 {
				bins[(int(pixel.R)>>4)<<8|(int(pixel.G)>>4)<<4|int(pixel.B)>>4]++
			}
		}
	}
	dominant := 0
	for index, count := range bins {
		if count > bins[dominant] {
			dominant = index
		}
	}
	tint := ""
	if bins[dominant] != 0 {
		tint = fmt.Sprintf("#%02x%02x%02x", (dominant>>8)*17, ((dominant>>4)&15)*17, (dominant&15)*17)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dest); err != nil {
		return nil, "", fmt.Errorf("encode avatar: %w", err)
	}
	return out.Bytes(), tint, nil
}

func historyFilter(c *echo.Context) storage.HistoryFilter {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	return storage.HistoryFilter{ModemID: c.QueryParam("modemId"), ProfileID: c.QueryParam("profileId"), Query: c.QueryParam("q"), Cursor: c.QueryParam("cursor"), Limit: limit}
}
func (h *Handler) threads(c *echo.Context) error {
	if id := c.QueryParam("conversationId"); id != "" {
		thread, err := h.config.Storage.ThreadForConversation(c.Request().Context(), id)
		if err != nil {
			return writeError(c, err)
		}
		return c.JSON(http.StatusOK, struct {
			ID string `json:"id"`
		}{thread})
	}
	page, err := h.config.Storage.MessageThreads(c.Request().Context(), historyFilter(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, page)
}
func (h *Handler) thread(c *echo.Context) error {
	thread, err := h.config.Storage.Thread(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, thread)
}
func (h *Handler) threadMessages(c *echo.Context) error {
	page, err := h.config.Storage.ThreadMessages(c.Request().Context(), c.Param("id"), historyFilter(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, page)
}
func (h *Handler) readThread(c *echo.Context) error {
	var read storage.ThreadRead
	if err := httpapi.BindJSON(c, &read, "thread_read_invalid"); err != nil {
		return err
	}
	if err := h.config.Storage.ReadThread(c.Request().Context(), c.Param("id"), read); err != nil {
		return writeError(c, err)
	}
	badge, err := h.config.Storage.MessagesState(c.Request().Context())
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, badge)
}
func (h *Handler) deleteThread(c *echo.Context) error {
	if err := h.config.Storage.DeleteThread(c.Request().Context(), c.Param("id")); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
func (h *Handler) preference(c *echo.Context) error {
	preference, err := h.config.Storage.PreferredCommunication(c.Request().Context(), c.QueryParam("participant"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, preference)
}
