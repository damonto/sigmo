package stk

import (
	"context"
	"errors"

	usim "github.com/damonto/wwan-go/sim"
	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

var errModemRequired = errors.New("modem is required")

type Runner interface {
	Run(context.Context, usim.STKCallbacks) error
	SendEnvelope(context.Context, stkpkg.Envelope) (stkpkg.EnvelopeResponse, error)
}

type Card struct {
	ICCID string
	STK   Runner
	Close func() error
}
