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
	return m.core.SetPowerState(ctx, wwanmodem.PowerStateOn)
}

func (m *Modem) Reset(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.Reset(ctx)
}

func (m *Modem) Disable(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.SetPowerState(ctx, wwanmodem.PowerStateLow)
}

func (m *Modem) SetPrimarySimSlot(ctx context.Context, slot uint32) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if slot == 0 || slot > 255 {
		return fmt.Errorf("set primary SIM slot: slot %d is outside 1..255", slot)
	}
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return fmt.Errorf("reserve primary SIM slot: %w", err)
	}
	defer release()
	return m.core.SetPrimarySIMSlot(ctx, uint8(slot))
}

func (m *Modem) SupportedModes(ctx context.Context) ([]wwanmodem.Mode, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	modes, _, err := m.core.Modes(ctx)
	return slices.Clone(modes), err
}

func (m *Modem) CurrentModes(ctx context.Context) (wwanmodem.Mode, error) {
	if m == nil || m.core == nil {
		return wwanmodem.Mode{}, errModemRequired
	}
	_, current, err := m.core.Modes(ctx)
	return current, err
}

func (m *Modem) SetCurrentModes(ctx context.Context, mode wwanmodem.Mode) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.SetModes(ctx, mode)
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
	return m.core.SetBands(ctx, slices.Clone(bands))
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
