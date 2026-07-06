package device

import (
	"context"
	"errors"
	"fmt"

	uiccmbim "github.com/damonto/uicc-go/mbim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"
)

type mbimDevice struct {
	device    string
	slot      int
	openRadio func(context.Context) (mbimAirplaneModeReader, error)
}

func newMBIMDevice(device string, slot int) mbimDevice {
	return mbimDevice{
		device: device,
		slot:   slot,
		openRadio: func(ctx context.Context) (mbimAirplaneModeReader, error) {
			return openMBIMReader(ctx, device, 1)
		},
	}
}

type mbimAirplaneModeReader interface {
	RadioState(ctx context.Context) (uiccmbim.RadioStateInfo, error)
	SetRadioState(ctx context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error)
	Close() error
}

func (u mbimDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	return openMBIMUSIMReader(ctx, u.device, u.slot)
}

func (u mbimDevice) USIMWithCAT(ctx context.Context, _ CATProfile) (usimcard.Reader, error) {
	return u.USIM(ctx)
}

func (u mbimDevice) ATR(ctx context.Context) ([]byte, error) {
	reader, err := openMBIMReader(ctx, u.device, u.slot)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()
	atr, err := reader.QueryUiccATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("query MBIM UICC ATR: %w", err)
	}
	return atr, nil
}

func (u mbimDevice) AirplaneMode(ctx context.Context) (bool, error) {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open MBIM airplane mode reader: %w", err)
	}
	defer closeReader("close MBIM airplane mode reader", reader)

	state, err := reader.RadioState(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM radio state: %w", err)
	}
	return state.SwRadioState == uiccmbim.RadioSwitchStateOff, nil
}

func (u mbimDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return fmt.Errorf("open MBIM airplane mode reader: %w", err)
	}
	defer closeReader("close MBIM airplane mode reader", reader)

	return setMBIMAirplaneMode(ctx, reader, enabled)
}

func (u mbimDevice) ToggleAirplaneMode(ctx context.Context) (bool, error) {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open MBIM airplane mode reader: %w", err)
	}
	defer closeReader("close MBIM airplane mode reader", reader)

	state, err := reader.RadioState(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM radio state: %w", err)
	}
	enabled := state.SwRadioState != uiccmbim.RadioSwitchStateOff
	if err := setMBIMAirplaneMode(ctx, reader, enabled); err != nil {
		return false, err
	}
	return enabled, nil
}

func setMBIMAirplaneMode(ctx context.Context, reader mbimAirplaneModeReader, enabled bool) error {
	state := uiccmbim.RadioSwitchStateOn
	if enabled {
		state = uiccmbim.RadioSwitchStateOff
	}
	if _, err := reader.SetRadioState(ctx, state); err != nil {
		return fmt.Errorf("set MBIM radio state: %w", err)
	}
	return nil
}

func openMBIMReader(ctx context.Context, device string, slot int) (*uiccmbim.Reader, error) {
	return uiccmbim.Open(ctx, uiccmbim.WithProxy(device), uiccmbim.WithSlot(slot))
}

func openMBIMUSIMReader(ctx context.Context, device string, slot int) (usimcard.Reader, error) {
	reader, err := openMBIMReader(ctx, device, slot)
	if err != nil {
		return nil, err
	}
	adapter, err := usim.NewMBIM(reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return adapter, nil
}
