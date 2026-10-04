//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/damonto/ts43-go"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// activationChannel opens the installed profile only when TS.43 needs to
// refresh credentials, after the LPA lease has been released and SIM enabled.
// The transfer runner owns it and uses it from a single goroutine.
type activationChannel struct {
	modem   *mmodem.Modem
	iccid   string
	channel ts43.Channel
	release func()
}

func (c *activationChannel) Identity(ctx context.Context) (*ts43.Identity, error) {
	if c.iccid == "" {
		return nil, errors.New("target profile is not installed")
	}
	if c.channel == nil {
		channel, release, err := openModemSource(ctx, c.modem)
		if err != nil {
			return nil, fmt.Errorf("open activated target SIM: %w", err)
		}
		c.channel, c.release = channel, release
	}
	identity, err := c.channel.Identity(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("read activated target SIM: %w", err)
	}
	if identity == nil || identity.ICCID != c.iccid {
		c.Close()
		return nil, errors.New("target SIM does not match the installed profile")
	}
	return identity, nil
}

func (c *activationChannel) AuthenticateAKA(ctx context.Context, req *ts43.AKARequest) (*ts43.AKAResponse, error) {
	if _, err := c.Identity(ctx); err != nil {
		return nil, err
	}
	return c.channel.AuthenticateAKA(ctx, req)
}

func (c *activationChannel) Close() {
	if c.release != nil {
		c.release()
	}
	c.channel, c.release = nil, nil
}
