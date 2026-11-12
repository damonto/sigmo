package settings

import (
	"net/http"

	"github.com/labstack/echo/v5"

	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

// Reader serves the aggregate settings representation.
type Reader struct{ store *appsettings.Store }

// NewReader creates the aggregate settings endpoint.
func NewReader(store *appsettings.Store) *Reader { return &Reader{store: store} }

func (h *Reader) Get(c *echo.Context) error {
	current := h.store.Snapshot()
	return c.JSON(http.StatusOK, responseFromSettings(current))
}
