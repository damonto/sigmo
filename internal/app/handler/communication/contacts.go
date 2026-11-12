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

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

// ContactHandler manages contacts and their avatars.
type ContactHandler struct{ store *storage.Store }

// NewContactHandler creates contact endpoints backed only by persistence.
func NewContactHandler(store *storage.Store) *ContactHandler { return &ContactHandler{store: store} }

// Contacts lists contacts matching the optional search query.
func (h *ContactHandler) Contacts(c *echo.Context) error {
	items, err := h.store.Contacts(c.Request().Context(), c.QueryParam("q"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []storage.Contact `json:"items"`
	}{items})
}

// Contact returns one contact.
func (h *ContactHandler) Contact(c *echo.Context) error {
	item, err := h.store.Contact(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// SaveContact creates or replaces a contact.
func (h *ContactHandler) SaveContact(c *echo.Context) error {
	var item storage.Contact
	if err := httpapi.BindJSON(c, &item, "contact_invalid"); err != nil {
		return err
	}
	item.ID = c.Param("id")
	item, err := h.store.SaveContact(c.Request().Context(), item)
	if err != nil {
		return writeError(c, err)
	}
	status := http.StatusOK
	if c.Request().Method == http.MethodPost {
		status = http.StatusCreated
		httpapi.SetResourceLocation(c, item.ID)
	}
	return c.JSON(status, item)
}

// DeleteContact removes a contact.
func (h *ContactHandler) DeleteContact(c *echo.Context) error {
	if err := h.store.DeleteContact(c.Request().Context(), c.Param("id")); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Avatar returns a contact's stored PNG thumbnail.
func (h *ContactHandler) Avatar(c *echo.Context) error {
	data, err := h.store.ContactAvatar(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-cache")
	return c.Blob(http.StatusOK, "image/png", data)
}

// SetAvatar replaces or removes a contact's avatar.
func (h *ContactHandler) SetAvatar(c *echo.Context) error {
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
	contact, err := h.store.SetContactAvatar(c.Request().Context(), c.Param("id"), data, tint)
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
