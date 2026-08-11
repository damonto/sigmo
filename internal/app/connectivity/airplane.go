package connectivity

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

// AirplaneModeLifecycle coordinates a radio change with services that depend
// on the modem being online. apply reports whether the radio state changed
// before a later operation, such as persistence, returned an error.
type AirplaneModeLifecycle interface {
	ChangeAirplaneMode(ctx context.Context, modem *modem.Modem, targetEnabled bool, apply func() (applied bool, err error)) error
}

var _ AirplaneModeLifecycle = (*internet.Connector)(nil)
