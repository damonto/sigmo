package server

import (
	"context"

	"github.com/damonto/sigmo/internal/app/feature"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/app/websheet"
	"github.com/labstack/echo/v5"
)

func configureFeatures(ctx context.Context, runtime *Runtime) error {
	websheets := websheet.New(websheet.Config{PolicyID: func() string { return runtime.Store.Snapshot().Auth.PolicyID() }})
	runtime.AddRoute(func(_ *echo.Group, deps router.RegisterConfig) error {
		group := deps.ResourceRoutes.Group("", appmiddleware.RequireFeature(runtime.Policy, feature.WiFiCalling))
		websheet.RegisterRoutes(group, websheets)
		return nil
	})
	if err := configureIMS(ctx, runtime, websheets); err != nil {
		return err
	}
	configureESIMTransfer(runtime, websheets)
	return nil
}
