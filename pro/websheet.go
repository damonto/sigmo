//go:build esim_transfer || ims

package main

import (
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/pro/websheet"
)

type websheetState struct {
	broker           *websheet.Broker
	routesRegistered bool
}

func (p *proApp) Websheets() *websheet.Broker {
	if p.websheet.broker == nil {
		p.websheet.broker = websheet.New(websheet.Config{PolicyID: func() string { return p.runtime.Store.Snapshot().Auth.PolicyID() }})
	}
	return p.websheet.broker
}

func (p *proApp) RegisterWebsheets() {
	if p.websheet.routesRegistered {
		return
	}
	p.websheet.routesRegistered = true
	p.runtime.AddRoute(func(_ *echo.Group, deps router.RegisterConfig) error {
		websheet.RegisterRoutes(deps.ResourceRoutes, p.Websheets())
		return nil
	})
}
