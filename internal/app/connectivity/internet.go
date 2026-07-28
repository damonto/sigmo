package connectivity

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

// InternetConnections is the application boundary for user-initiated Internet
// operations. Pro builds implement it with the same per-modem coordinator used
// by Wi-Fi Calling and VoLTE.
type InternetConnections interface {
	Current(context.Context, *modem.Modem) (*internet.Connection, error)
	Public(context.Context, *modem.Modem) (internet.IPInfo, error)
	Connect(context.Context, *modem.Modem, internet.Preferences) (*internet.Connection, error)
	UpdatePreferences(context.Context, *modem.Modem, internet.ConnectionPreferences) (*internet.Connection, error)
	Disconnect(context.Context, *modem.Modem) error
}

var _ InternetConnections = (*internet.Connector)(nil)
