//go:build ims

package ims

import (
	"context"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/pro/websheet"
)

func (c *Connectivity) WiFiCallingStatus(ctx context.Context, modem *mmodem.Modem) (WiFiCallingStatus, error) {
	if c == nil || c.wifiCalling == nil {
		return WiFiCallingStatus{}, ErrUnavailable
	}
	settings, err := c.wifiCalling.WiFiCallingSettings(ctx, modem)
	if err != nil {
		return WiFiCallingStatus{}, err
	}
	status, err := c.wifiCalling.SessionStatus(ctx, modem, settings.Enabled)
	if err != nil {
		return WiFiCallingStatus{}, err
	}
	return WiFiCallingStatus{
		WiFiCallingSettings: settings,
		Connected:           status.Connected,
		State:               status.State,
		DurationSeconds:     status.DurationSeconds,
		Websheet:            status.Websheet,
	}, nil
}

func (c *Connectivity) ReplaceWiFiCallingSettings(ctx context.Context, modem *mmodem.Modem, settings WiFiCallingSettings) error {
	if c == nil || c.wifiCalling == nil {
		return ErrUnavailable
	}
	return c.change(modem, func() error {
		settings, err := ResolveWiFiCallingSettings(modem, settings)
		if err != nil {
			return err
		}
		return c.wifiCalling.UpdateWiFiCallingSettings(ctx, modem, settings)
	})
}

func (c *Connectivity) ReconnectWiFiCalling(ctx context.Context, modem *mmodem.Modem) error {
	if c == nil || c.wifiCalling == nil {
		return ErrUnavailable
	}
	return c.change(modem, func() error {
		return c.wifiCalling.ReconnectWiFiCalling(ctx, modem)
	})
}

func (c *Connectivity) DisconnectWiFiCalling(ctx context.Context, modem *mmodem.Modem) error {
	if c == nil || c.wifiCalling == nil {
		return ErrUnavailable
	}
	return c.change(modem, func() error {
		return c.wifiCalling.Disconnect(ctx, modem)
	})
}

func (c *Connectivity) WiFiCallingEmergencyAddressUpdateAvailable(ctx context.Context, modem *mmodem.Modem) bool {
	return c != nil && c.wifiCalling != nil && c.wifiCalling.EmergencyAddressUpdateAvailable(ctx, modem)
}

func (c *Connectivity) StartWiFiCallingWebsheet(ctx context.Context, modem *mmodem.Modem) (websheet.Info, error) {
	if c == nil || c.wifiCalling == nil {
		return websheet.Info{}, ErrUnavailable
	}
	var info websheet.Info
	err := c.change(modem, func() error {
		var err error
		info, err = c.wifiCalling.StartWebsheet(ctx, modem)
		return err
	})
	return info, err
}

func (c *Connectivity) StartWiFiCallingEmergencyAddressUpdate(ctx context.Context, modem *mmodem.Modem) (websheet.Info, error) {
	if c == nil || c.wifiCalling == nil {
		return websheet.Info{}, ErrUnavailable
	}
	var info websheet.Info
	err := c.change(modem, func() error {
		var err error
		info, err = c.wifiCalling.StartEmergencyAddressUpdate(ctx, modem)
		return err
	})
	return info, err
}
