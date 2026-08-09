//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
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
	status, err = managedVoLTEStatus(ctx, device)
	if errors.Is(err, qcom.QMIErrorInvalidOperation) {
		return wwan.VoLTEStatus{}, nil
	}
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

	status, err := managedVoLTEStatus(ctx, device)
	if err != nil {
		return fmt.Errorf("read VoLTE status: %w", err)
	}
	if _, err := device.IMSProfile(ctx); err != nil {
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

func managedVoLTEStatus(ctx context.Context, device managedVoLTEDevice) (wwan.VoLTEStatus, error) {
	status, err := device.VoLTEStatus(ctx)
	if errors.Is(err, wwan.ErrUnsupported) {
		// MBIM can validate the IMS context and packet service, but cannot report
		// whether the modem already owns native IMS registration.
		return wwan.VoLTEStatus{}, nil
	}
	return status, err
}

func ResolveVoLTESettings(modem *mmodem.Modem, settings VoLTESettings) (VoLTESettings, error) {
	port, err := voLTEPort(modem)
	if err != nil {
		return VoLTESettings{}, err
	}
	switch port.PortType {
	case wwanmodem.PortQMI:
		if settings.DataPath == "" {
			return VoLTESettings{}, ErrVoLTEDataPathRequired
		}
		switch settings.DataPath {
		case DataPathQMAP, DataPathLegacyBAMDMUX, DataPathQualcomm410:
		default:
			return VoLTESettings{}, fmt.Errorf("%w: %q", ErrVoLTEDataPathUnsupported, settings.DataPath)
		}
	case wwanmodem.PortMBIM:
		settings.DataPath = DataPathMBIM
	default:
		return VoLTESettings{}, ErrUnavailable
	}
	return settings, nil
}

type voLTESettingsUpdater interface {
	UpdateVoLTESettings(context.Context, *mmodem.Modem, VoLTESettings) error
}

func updateVoLTESettings(ctx context.Context, modem *mmodem.Modem, updater voLTESettingsUpdater, settings VoLTESettings) error {
	settings, err := ResolveVoLTESettings(modem, settings)
	if err != nil {
		return err
	}
	return updateResolvedVoLTESettings(ctx, modem, updater, settings)
}

func updateResolvedVoLTESettings(ctx context.Context, modem *mmodem.Modem, updater voLTESettingsUpdater, settings VoLTESettings) error {
	if settings.Enabled {
		if err := validateManagedVoLTE(ctx, modem); err != nil {
			return err
		}
	}
	return updater.UpdateVoLTESettings(ctx, modem, settings)
}
