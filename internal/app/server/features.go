package server

import (
	"context"

	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/app/websheet"
)

func configureFeatures(ctx context.Context, runtime *Runtime) error {
	websheets := websheet.New(websheet.Config{PolicyID: func() string { return runtime.Store.Snapshot().Auth.PolicyID() }})
	runtime.AddRoute(router.WebsheetRoutes(websheets))
	if err := configureIMS(ctx, runtime, websheets); err != nil {
		return err
	}
	configureESIMTransfer(runtime, websheets)
	return nil
}
