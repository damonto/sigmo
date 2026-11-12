package server

import (
	transfer "github.com/damonto/sigmo/internal/app/esimtransfer"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/app/websheet"
)

func configureESIMTransfer(runtime *Runtime, websheets *websheet.Broker) {
	runtime.AddFeatures(feature.ESIMTransfer)
	cfg := transfer.Config{
		Store:         runtime.Store,
		CarrierConfig: runtime.License.CarrierConfig,
		Registry:      runtime.Registry,
		LPA:           runtime.LPAClients,
		Websheets:     websheets,
		Allowed:       func() bool { return runtime.Policy.Allows(feature.ESIMTransfer) },
		EnableProfile: runtime.ESIM.EnableProfile,
		DeleteProfile: runtime.ESIM.DeleteProfile,
	}
	service := transfer.New(cfg)
	runtime.AddCleanup(service.Close)
	runtime.AddRoute(router.ESIMTransferRoutes(service))
}
