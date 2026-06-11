package capability

import (
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"
)

type Handler struct {
	features []string
}

type Response struct {
	Features []string `json:"features"`
}

func New(features []string) *Handler {
	out := slices.Clone(features)
	slices.Sort(out)
	return &Handler{features: out}
}

func (h *Handler) List(c *echo.Context) error {
	return c.JSON(http.StatusOK, Response{Features: slices.Clone(h.features)})
}
