//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
	"github.com/damonto/uicc-go/at"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"
)

func openReader(ctx context.Context, modem *mmodem.Modem) (usimcard.Reader, error) {
	return openReaderWith(ctx, modem, openDeviceReader, openATReader)
}

type deviceReaderOpener func(context.Context, *mmodem.Modem) (usimcard.Reader, error)
type atReaderOpener func(context.Context, mmodem.ModemPort) (usimcard.Reader, error)

func openReaderWith(ctx context.Context, modem *mmodem.Modem, openDevice deviceReaderOpener, openAT atReaderOpener) (usimcard.Reader, error) {
	var result error
	reader, err := openDevice(ctx, modem)
	if err == nil {
		return reader, nil
	}
	if !errors.Is(err, mdevice.ErrUnsupported) {
		result = errors.Join(result, fmt.Errorf("open device reader: %w", err))
	}

	for _, port := range atReaderPorts(modem) {
		reader, err := openAT(ctx, port)
		if err == nil {
			return reader, nil
		}
		result = errors.Join(result, fmt.Errorf("open AT reader on %s: %w", port.Device, err))
	}
	if result == nil {
		return nil, errors.New("Wi-Fi Calling requires modem device or AT modem port")
	}
	return nil, result
}

func atReaderPorts(modem *mmodem.Modem) []mmodem.ModemPort {
	if modem == nil {
		return nil
	}
	var ports []mmodem.ModemPort
	add := func(port mmodem.ModemPort) {
		device := port.Device
		device = strings.TrimSpace(device)
		if device == "" || port.PortType != mmodem.ModemPortTypeAt {
			return
		}
		for _, candidate := range ports {
			if candidate.Device == device {
				return
			}
		}
		port.Device = device
		ports = append(ports, port)
	}

	for _, port := range modem.Ports {
		if port.Device == modem.PrimaryPort {
			add(port)
			break
		}
	}
	for _, port := range modem.Ports {
		if port.PortType == mmodem.ModemPortTypeAt {
			add(port)
		}
	}
	return ports
}

func openDeviceReader(ctx context.Context, modem *mmodem.Modem) (usimcard.Reader, error) {
	device, err := mmodem.OpenDevice(modem)
	if err != nil {
		return nil, err
	}
	return device.USIM(ctx)
}

func openATReader(_ context.Context, port mmodem.ModemPort) (usimcard.Reader, error) {
	tx, err := at.Open(port.Device, 0)
	if err != nil {
		return nil, err
	}
	reader, err := usim.NewReader(tx)
	if err != nil {
		return nil, errors.Join(err, tx.Close())
	}
	return reader, nil
}
