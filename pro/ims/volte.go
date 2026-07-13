//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

func readVoLTEStatus(ctx context.Context, modem *mmodem.Modem) (status wwan.VoLTEStatus, err error) {
	device, err := openManagedVoLTEDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return wwan.VoLTEStatus{}, nil
	}
	if err != nil {
		return wwan.VoLTEStatus{}, fmt.Errorf("open VoLTE status device: %w", err)
	}
	defer func() {
		err = errors.Join(err, device.Close())
	}()
	status, err = device.VoLTEStatus(ctx)
	if err != nil {
		return wwan.VoLTEStatus{}, fmt.Errorf("read VoLTE status: %w", err)
	}
	return status, nil
}

func validateManagedVoLTE(ctx context.Context, modem *mmodem.Modem) (err error) {
	device, err := openManagedVoLTEDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return ErrUnavailable
	}
	if err != nil {
		return fmt.Errorf("open VoLTE validation device: %w", err)
	}
	defer func() {
		err = errors.Join(err, device.Close())
	}()

	status, err := device.VoLTEStatus(ctx)
	if err != nil {
		return fmt.Errorf("read VoLTE status: %w", err)
	}
	if _, err := device.IMSProfileIndex(ctx); err != nil {
		return fmt.Errorf("find IMS profile: %w", err)
	}
	if status.Occupied {
		if _, err := device.IMSSTestMode(ctx); err != nil {
			return fmt.Errorf("read IMSS test mode: %w", err)
		}
	}
	if _, err := device.PacketServiceStatus(ctx); err != nil {
		return fmt.Errorf("read packet service status: %w", err)
	}
	return nil
}
