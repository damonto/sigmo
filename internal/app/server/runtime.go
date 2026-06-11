package server

import (
	"context"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/message"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/ussd"
)

type Extension func(*Runtime) error

type Runner func(context.Context) error

type Runtime struct {
	Store              *settings.Store
	Registry           *modem.Registry
	Internet           *internet.Connector
	Relay              *forwarder.Relay
	NetworkPreferences *modem.NetworkPreferences
	Storage            *storage.Store

	messageRoute message.Route
	ussdRoute    ussd.Route
	routes       []router.Extension
	runners      []Runner
	features     []string
}

func (r *Runtime) SetMessageRoute(route message.Route) {
	r.messageRoute = route
}

func (r *Runtime) SetUSSDRoute(route ussd.Route) {
	r.ussdRoute = route
}

func (r *Runtime) AddRoute(route router.Extension) {
	r.routes = append(r.routes, route)
}

func (r *Runtime) AddRunner(runner Runner) {
	r.runners = append(r.runners, runner)
}

func (r *Runtime) AddFeatures(features ...string) {
	r.features = append(r.features, features...)
}
