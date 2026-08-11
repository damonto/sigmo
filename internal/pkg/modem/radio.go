package modem

import (
	"context"
	"fmt"
	"slices"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func (m *Modem) AirplaneMode(ctx context.Context) (bool, error) {
	if m == nil {
		return false, errModemRequired
	}
	if m.core == nil {
		return false, wwanmodem.ErrNotSupported
	}
	state, err := m.core.PowerState(ctx)
	if err != nil {
		return false, fmt.Errorf("read modem power state: %w", err)
	}
	return airplaneModeEnabled(state), nil
}

func (m *Modem) SetAirplaneMode(ctx context.Context, enabled bool) error {
	if m == nil {
		return errModemRequired
	}
	if m.core == nil {
		return wwanmodem.ErrNotSupported
	}
	if enabled {
		return m.Disable(ctx)
	}
	return m.Enable(ctx)
}

func airplaneModeEnabled(state wwanmodem.PowerState) bool {
	return state == wwanmodem.PowerStateOff || state == wwanmodem.PowerStateLow
}

func (m *Modem) Enable(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if err := m.core.SetPowerState(ctx, wwanmodem.PowerStateOn); err != nil {
		return err
	}
	m.applyPowerState(wwanmodem.PowerStateOn)
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) Reset(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if err := m.core.Reset(ctx); err != nil {
		return err
	}
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) Disable(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if err := m.core.SetPowerState(ctx, wwanmodem.PowerStateLow); err != nil {
		return err
	}
	m.applyPowerState(wwanmodem.PowerStateLow)
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) SetPrimarySIMSlot(ctx context.Context, slot uint32) error {
	if err := m.validatePrimarySIMSlot(slot); err != nil {
		return fmt.Errorf("set primary SIM slot: %w", err)
	}
	return m.withReservedSIMSlot(ctx, func() error {
		if err := m.setPrimarySIMSlot(ctx, slot); err != nil {
			return fmt.Errorf("set primary SIM slot: %w", err)
		}
		return nil
	})
}

func (m *Modem) validatePrimarySIMSlot(slot uint32) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if slot == 0 || slot > 255 {
		return fmt.Errorf("slot %d is outside 1..255", slot)
	}
	return nil
}

func (m *Modem) setPrimarySIMSlot(ctx context.Context, slot uint32) error {
	if err := m.core.SetPrimarySIMSlot(ctx, uint8(slot)); err != nil {
		return err
	}
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) Modes(ctx context.Context) ([]wwanmodem.Mode, wwanmodem.Mode, error) {
	if m == nil || m.core == nil {
		return nil, wwanmodem.Mode{}, errModemRequired
	}
	modes, current, err := m.core.Modes(ctx)
	return slices.Clone(modes), current, err
}

func (m *Modem) SetCurrentModes(ctx context.Context, mode wwanmodem.Mode) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if err := m.core.SetModes(ctx, mode); err != nil {
		return err
	}
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) SupportedBands(ctx context.Context) ([]wwanmodem.Band, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	bands, err := m.core.SupportedBands(ctx)
	return slices.Clone(bands), err
}

func (m *Modem) CurrentBands(ctx context.Context) ([]wwanmodem.Band, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	bands, err := m.core.Bands(ctx)
	return slices.Clone(bands), err
}

func (m *Modem) SetCurrentBands(ctx context.Context, bands []wwanmodem.Band) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if err := m.core.SetBands(ctx, slices.Clone(bands)); err != nil {
		return err
	}
	m.markNetworkStateChanged()
	return nil
}

func (m *Modem) AccessTechnology(ctx context.Context) (wwanmodem.Technology, error) {
	if m == nil || m.core == nil {
		return 0, errModemRequired
	}
	status, err := m.core.NetworkStatus(ctx)
	if err != nil {
		return 0, err
	}
	return status.Technology, nil
}

func (m *Modem) SignalQuality(ctx context.Context) (percent uint32, recent bool, err error) {
	if m == nil || m.core == nil {
		return 0, false, errModemRequired
	}
	signal, err := m.core.Signal(ctx)
	if err != nil {
		return 0, false, err
	}
	return uint32(signal.Quality), true, nil
}
