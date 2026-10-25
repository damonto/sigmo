package server

import (
	"context"
	"errors"
	"slices"

	appconnectivity "github.com/damonto/sigmo/internal/app/connectivity"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/license"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	"github.com/damonto/sigmo/internal/pkg/message"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/ussd"
	"github.com/damonto/sigmo/internal/pkg/webpush"
)

type Runner func(context.Context) error

type namedRunner struct {
	name string
	run  Runner
}

type Cleanup func(context.Context) error

type Runtime struct {
	Store               *settings.Store
	Registry            *modem.Registry
	InternetConnector   *internet.Connector
	LPAClients          *lpa.Pool
	internetConnections appconnectivity.InternetConnections
	Relay               *forwarder.Relay
	NetworkPreferences  *networkprefs.Store
	Storage             *storage.Store
	WebPush             *webpush.Client
	Reminders           *reminder.Scheduler
	License             *license.Controller
	Policy              feature.Policy

	messageRoute          message.Route
	ussdRoute             ussd.Route
	modemOverview         []modemstatus.Extension
	mcpTools              []mcpserver.Extension
	routes                []router.Extension
	runners               []namedRunner
	cleanups              []Cleanup
	features              []feature.Name
	airplaneModeLifecycle appconnectivity.AirplaneModeLifecycle
}

func (r *Runtime) SetInternetConnections(connections appconnectivity.InternetConnections) {
	r.internetConnections = connections
}

func (r *Runtime) SetMessageRoute(route message.Route) {
	r.messageRoute = route
}

func (r *Runtime) SetUSSDRoute(route ussd.Route) {
	r.ussdRoute = route
}

func (r *Runtime) SetAirplaneModeLifecycle(lifecycle appconnectivity.AirplaneModeLifecycle) {
	r.airplaneModeLifecycle = lifecycle
}

func (r *Runtime) AddModemOverview(extensions ...modemstatus.Extension) {
	r.modemOverview = append(r.modemOverview, extensions...)
}

func (r *Runtime) AddMCPTools(extensions ...mcpserver.Extension) {
	r.mcpTools = append(r.mcpTools, extensions...)
}

func (r *Runtime) AddRoute(route router.Extension) {
	r.routes = append(r.routes, route)
}

// AddRunner registers a background task with a name used in failure reports.
func (r *Runtime) AddRunner(name string, runner Runner) {
	r.runners = append(r.runners, namedRunner{name: name, run: runner})
}

func (r *Runtime) AddCleanup(cleanups ...Cleanup) {
	r.cleanups = append(r.cleanups, cleanups...)
}

// close releases extension-owned resources after all extension runners stop.
func (r *Runtime) close(ctx context.Context) error {
	var result error
	for _, v := range slices.Backward(r.cleanups) {
		if v == nil {
			continue
		}
		result = errors.Join(result, v(ctx))
	}
	r.cleanups = nil
	return result
}

func (r *Runtime) AddFeatures(features ...feature.Name) {
	r.features = append(r.features, features...)
}
