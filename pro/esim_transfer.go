//go:build esim_transfer

package main

import (
	"github.com/labstack/echo/v5"

	coreesim "github.com/damonto/sigmo/internal/app/handler/esim"
	"github.com/damonto/sigmo/internal/app/router"
	protransfer "github.com/damonto/sigmo/pro/esimtransfer"
)

const esimTransferFeature = "esimTransfer"

var proESIMTransfer = func(app *proApp) error {
	runtime := app.runtime
	app.RegisterWebsheets()
	runtime.AddFeatures(esimTransferFeature)
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		core := coreesim.New(coreesim.Config{
			Store:    deps.Store,
			Registry: deps.Registry,
			Internet: deps.Internet,
		})
		protransfer.RegisterRoutes(group, protransfer.ConfigFromCore(core, protransfer.Config{
			Store:     deps.Store,
			Registry:  deps.Registry,
			Websheets: app.Websheets(),
		}))
		return nil
	})
	return nil
}
