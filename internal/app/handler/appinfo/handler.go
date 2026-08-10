package appinfo

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/buildinfo"
)

type Handler struct {
	build buildinfo.Info
}

type Response struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Channel      string `json:"channel"`
	Edition      string `json:"edition"`
	Target       string `json:"target"`
	Distribution string `json:"distribution"`
}

func New(build buildinfo.Info) *Handler {
	return &Handler{build: build}
}

func (h *Handler) Get(c *echo.Context) error {
	return c.JSON(http.StatusOK, Response{
		Version:      h.build.Version,
		Commit:       h.build.Commit,
		Channel:      h.build.Channel,
		Edition:      h.build.Edition,
		Target:       h.build.Target,
		Distribution: h.build.Distribution,
	})
}
