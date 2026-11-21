package ims

import (
	"context"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (c *Connectivity) Current(ctx context.Context, modem *mmodem.Modem) (*pinternet.Connection, error) {
	if c == nil || c.internet == nil {
		return nil, ErrUnavailable
	}
	return c.internet.Current(ctx, modem)
}

func (c *Connectivity) Public(ctx context.Context, modem *mmodem.Modem) (pinternet.IPInfo, error) {
	if c == nil || c.internet == nil {
		return pinternet.IPInfo{}, ErrUnavailable
	}
	return c.internet.Public(ctx, modem)
}

func (c *Connectivity) Connect(ctx context.Context, modem *mmodem.Modem, preferences pinternet.Preferences) (*pinternet.Connection, error) {
	if c == nil || c.internet == nil {
		return nil, ErrUnavailable
	}
	var connection *pinternet.Connection
	err := c.change(ctx, modem, func() error {
		var err error
		connection, err = c.internet.Connect(ctx, modem, preferences)
		return err
	})
	return connection, err
}

func (c *Connectivity) UpdatePreferences(ctx context.Context, modem *mmodem.Modem, preferences pinternet.ConnectionPreferences) (*pinternet.Connection, error) {
	if c == nil || c.internet == nil {
		return nil, ErrUnavailable
	}
	var connection *pinternet.Connection
	err := c.change(ctx, modem, func() error {
		var err error
		connection, err = c.internet.UpdatePreferences(ctx, modem, preferences)
		return err
	})
	return connection, err
}

func (c *Connectivity) Disconnect(ctx context.Context, modem *mmodem.Modem) error {
	if c == nil || c.internet == nil {
		return ErrUnavailable
	}
	return c.change(ctx, modem, func() error {
		return c.internet.Disconnect(ctx, modem)
	})
}

// WithAutomationConnection coordinates a scoped cellular connection with IMS configuration.
func (c *Connectivity) WithAutomationConnection(ctx context.Context, modem *mmodem.Modem, profile mmodem.SIMIdentity, use func(*pinternet.Connection) error) error {
	if c == nil || c.internet == nil || modem == nil {
		return ErrUnavailable
	}
	return c.change(ctx, modem, func() error {
		return c.internet.WithAutomationConnection(ctx, modem, profile, use)
	})
}

// RestoreAutomationConnection restores a former SIM's Internet connection through the shared coordinator.
func (c *Connectivity) RestoreAutomationConnection(ctx context.Context, modem *mmodem.Modem, profile mmodem.SIMIdentity, prefs pinternet.Preferences) error {
	if c == nil || c.internet == nil || modem == nil {
		return ErrUnavailable
	}
	return c.change(ctx, modem, func() error {
		return c.internet.RestoreAutomationConnection(ctx, modem, profile, prefs)
	})
}
