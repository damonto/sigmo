package automation

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// RegisterRoutes exposes task management inside the authenticated API group.
func (s *Service) RegisterRoutes(group *echo.Group) {
	const collection = "/sim-profiles/:profileId/automations"
	group.GET("/modems/:id/automations", s.listSummariesHTTP)
	group.GET(collection, s.listHTTP)
	group.POST(collection, s.createHTTP)
	group.PUT(collection+"/:automationId", s.updateHTTP)
	group.DELETE(collection+"/:automationId", s.deleteHTTP)
}

func (s *Service) listSummariesHTTP(c *echo.Context) error {
	items, err := s.ListSummaries(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []TaskSummary `json:"items"`
	}{Items: items})
}

func (s *Service) listHTTP(c *echo.Context) error {
	items, err := s.List(c.Request().Context(), c.Param("profileId"))
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, struct {
		Items []Task `json:"items"`
	}{Items: items})
}

func (s *Service) createHTTP(c *echo.Context) error {
	var req CreateRequest
	if err := httpapi.BindJSON(c, &req, "invalid_request"); err != nil {
		return err
	}
	task, err := s.Create(c.Request().Context(), c.Param("profileId"), req)
	if err != nil {
		return writeError(c, err)
	}
	c.Response().Header().Set("Location", strings.TrimSuffix(c.Request().URL.EscapedPath(), "/")+"/"+task.ID)
	return c.JSON(http.StatusCreated, task)
}

func (s *Service) updateHTTP(c *echo.Context) error {
	var req Schedule
	if err := httpapi.BindJSON(c, &req, "invalid_request"); err != nil {
		return err
	}
	task, err := s.Update(c.Request().Context(), c.Param("profileId"), c.Param("automationId"), req)
	if err != nil {
		return writeError(c, err)
	}
	return c.JSON(http.StatusOK, task)
}

func (s *Service) deleteHTTP(c *echo.Context) error {
	if err := s.Delete(c.Request().Context(), c.Param("profileId"), c.Param("automationId")); err != nil {
		return writeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func writeError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrInvalidSchedule):
		return httpapi.UnprocessableEntity(c, "unprocessable_entity", err)
	case errors.Is(err, ErrLineUnavailable), errors.Is(err, storage.ErrSIMAutomationLineUnavailable):
		return httpapi.Error(c, http.StatusConflict, "conflict", ErrLineUnavailable.Error())
	case errors.Is(err, storage.ErrSIMAutomationBusy), errors.Is(err, storage.ErrSIMAutomationExists):
		return httpapi.Error(c, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, storage.ErrNotFound):
		return httpapi.Error(c, http.StatusNotFound, "resource_not_found", "automation not found")
	default:
		return httpapi.Internal(c, "internal_server_error", err)
	}
}
