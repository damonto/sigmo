//go:build esim_transfer || wifi_calling

package main

import (
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/router"
	hwebsheet "github.com/damonto/sigmo/pro/internal/app/handler/websheet"
	"github.com/damonto/sigmo/pro/internal/pkg/websheet"
)

type websheetState struct {
	broker           *websheet.Broker
	routesRegistered bool
}

func (p *proApp) Websheets() *websheet.Broker {
	if p.websheet.broker == nil {
		p.websheet.broker = websheet.New(websheet.Config{})
	}
	return p.websheet.broker
}

func (p *proApp) RegisterWebsheets() {
	if p.websheet.routesRegistered {
		return
	}
	p.websheet.routesRegistered = true
	p.runtime.AddRoute(func(group *echo.Group, _ router.RegisterConfig) error {
		hwebsheet.New(p.Websheets()).Register(group)
		return nil
	})
}
