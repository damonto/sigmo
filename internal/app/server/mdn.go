package server

import (
	"github.com/damonto/sigmo/internal/app/feature"
	appmdn "github.com/damonto/sigmo/internal/app/mdn"
)

func configureMDN(runtime *Runtime) *appmdn.Service {
	return appmdn.New(appmdn.Config{
		Registry: runtime.Registry, Store: runtime.Storage,
		CarrierConfig: runtime.License.CarrierConfig,
		Allowed:       func() bool { return runtime.Policy.Allows(feature.MDN) },
	})
}
