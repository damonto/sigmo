package server

import (
	"github.com/labstack/echo/v5"

	transfer "github.com/damonto/sigmo/internal/app/esimtransfer"
	"github.com/damonto/sigmo/internal/app/feature"
	coreesim "github.com/damonto/sigmo/internal/app/handler/esim"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/app/websheet"
)

func configureESIMTransfer(runtime *Runtime, websheets *websheet.Broker) {
	runtime.AddFeatures(feature.ESIMTransfer)
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		core := coreesim.New(coreesim.Config{
			Store:    deps.Store,
			Registry: deps.Registry,
			LPA:      deps.LPAClients,
			Internet: deps.InternetConnector,
		})
		cfg := transfer.ConfigFromCore(core, transfer.Config{
			Store:         deps.Store,
			CarrierConfig: runtime.License.CarrierConfig,
			Registry:      deps.Registry,
			LPA:           deps.LPAClients,
			Websheets:     websheets,
			Allowed:       func() bool { return runtime.Policy.Allows(feature.ESIMTransfer) },
		})
		handler := transfer.RegisterWithPeerRoutes(group.Group("", appmiddleware.RequireFeature(runtime.Policy, feature.ESIMTransfer)), deps.ResourceRoutes.Group("", appmiddleware.RequireFeature(runtime.Policy, feature.ESIMTransfer)), cfg)
		runtime.AddCleanup(handler.Close)
		return nil
	})
}
