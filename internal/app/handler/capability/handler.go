package capability

import (
	"net/http"
	"slices"

	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/labstack/echo/v5"
)

type Handler struct {
	features []feature.Name
	policy   feature.Policy
}

type Response struct {
	Features []feature.Name `json:"features"`
}

func New(features []feature.Name, policy feature.Policy) *Handler {
	out := slices.Clone(features)
	slices.Sort(out)
	return &Handler{features: out, policy: policy}
}

func (h *Handler) List(c *echo.Context) error {
	return c.JSON(http.StatusOK, Response{Features: h.policy.Filter(h.features)})
}
