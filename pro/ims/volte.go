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
