package stk

import (
	"context"
	"errors"

	"github.com/damonto/uicc-go/usim"
	stkpkg "github.com/damonto/uicc-go/usim/stk"
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
