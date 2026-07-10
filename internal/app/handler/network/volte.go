package network

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

var errVoLTEUnavailable = errors.New("volte cannot be enabled")

type volteDevice interface {
	VoLTEStatus(context.Context) (mdevice.VoLTEStatus, error)
}

var openVoLTEDevice = func(modem *mmodem.Modem) (volteDevice, error) {
	return mmodem.OpenVoLTEStatusDevice(modem)
}

func (n *network) VoLTE(ctx context.Context, modem *mmodem.Modem) (*VoLTEResponse, error) {
	managed, _, err := n.preferences.SavedVoLTE(ctx, modem.EquipmentIdentifier)
	if err != nil {
		return nil, fmt.Errorf("read volte preference: %w", err)
	}
	status, err := n.readVoLTEStatus(ctx, modem)
	if err != nil {
		return nil, err
	}
	return &VoLTEResponse{
		Managed:   managed,
		CanEnable: status.CanEnable,
	}, nil
}

func (n *network) SetVoLTE(ctx context.Context, modem *mmodem.Modem, req SetVoLTERequest) error {
	if req.Managed {
		status, err := n.readVoLTEStatus(ctx, modem)
		if err != nil {
			return err
		}
		if !status.CanEnable {
			return errVoLTEUnavailable
		}
	}
	if err := n.preferences.SaveVoLTE(ctx, modem.EquipmentIdentifier, req.Managed); err != nil {
		return fmt.Errorf("save volte preference: %w", err)
	}
	return nil
}

func (n *network) readVoLTEStatus(ctx context.Context, modem *mmodem.Modem) (mdevice.VoLTEStatus, error) {
	device, err := openVoLTEDevice(modem)
	if errors.Is(err, mdevice.ErrUnsupported) {
		return mdevice.VoLTEStatus{}, nil
	}
	if err != nil {
		return mdevice.VoLTEStatus{}, fmt.Errorf("open device: %w", err)
	}
	status, err := device.VoLTEStatus(ctx)
	if err != nil {
		return mdevice.VoLTEStatus{}, fmt.Errorf("read volte status: %w", err)
	}
	return status, nil
}
