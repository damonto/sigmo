//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"strings"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/wwan-go/at"
	"github.com/damonto/wwan-go/mbim"
	"github.com/damonto/wwan-go/qcom"
	"github.com/damonto/wwan-go/qcom/qmi"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

// WWANConfig selects the modem access used by an IMS client.
type WWANConfig struct {
	Access            Access
	MuxDataPort       *qcom.WDSMuxDataPort
	LegacyMuxDataPort qcom.WDSSIOPort
	InterfaceName     string
}

// OpenWWAN opens the SIM and packet-data adapter required by the selected IMS access.
func OpenWWAN(ctx context.Context, modem *mmodem.Modem, cfg WWANConfig) (usimcard.Reader, error) {
	switch cfg.Access {
	case AccessVoLTE:
		return openVoLTEWWAN(ctx, modem, cfg)
	case AccessWiFiCalling:
		return openWiFiCallingWWANWith(ctx, modem, openDeviceWWAN, openATWWAN)
	default:
		return nil, fmt.Errorf("open WWAN: unsupported IMS access %q", cfg.Access)
	}
}

type deviceWWANOpener func(context.Context, *mmodem.Modem) (usimcard.Reader, error)
type atWWANOpener func(context.Context, mmodem.ModemPort) (usimcard.Reader, error)

func openWiFiCallingWWANWith(ctx context.Context, modem *mmodem.Modem, openDevice deviceWWANOpener, openAT atWWANOpener) (usimcard.Reader, error) {
	var result error
	reader, err := openDevice(ctx, modem)
	if err == nil {
		return reader, nil
	}
	if !errors.Is(err, wwan.ErrUnsupported) {
		result = errors.Join(result, fmt.Errorf("open modem WWAN: %w", err))
	}

	for _, port := range atReaderPorts(modem) {
		reader, err := openAT(ctx, port)
		if err == nil {
			return reader, nil
		}
		result = errors.Join(result, fmt.Errorf("open AT WWAN on %s: %w", port.Device, err))
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

func openDeviceWWAN(ctx context.Context, modem *mmodem.Modem) (usimcard.Reader, error) {
	device, err := mmodem.OpenDevice(modem)
	if err != nil {
		return nil, err
	}
	return device.USIM(ctx)
}

func openVoLTEWWAN(ctx context.Context, modem *mmodem.Modem, cfg WWANConfig) (usimcard.Reader, error) {
	port, err := voLTEControlPort(modem)
	if err != nil {
		return nil, err
	}
	slot, err := voLTESIMSlot(modem)
	if err != nil {
		return nil, err
	}
	switch port.PortType {
	case mmodem.ModemPortTypeQmi:
		if cfg.MuxDataPort == nil && cfg.LegacyMuxDataPort == 0 {
			return nil, errors.New("QMI VoLTE data path is required")
		}
		if cfg.MuxDataPort != nil && cfg.LegacyMuxDataPort != 0 {
			return nil, errors.New("QMAP and legacy BAM-DMUX data ports are mutually exclusive")
		}
		transport, err := qmi.Open(ctx, qmi.WithProxy(port.Device))
		if err != nil {
			return nil, fmt.Errorf("open QMI proxy: %w", err)
		}
		client, err := qcom.NewClient(transport, qcom.WithSlot(slot))
		if err != nil {
			return nil, errors.Join(err, transport.Close())
		}
		if err := client.ActivateSlot(ctx); err != nil {
			return nil, errors.Join(fmt.Errorf("activate QMI SIM slot: %w", err), client.Close())
		}
		reader, err := imsgo.NewQCOMClient(client, imsgo.QCOMClientConfig{
			MuxDataPort:       cfg.MuxDataPort,
			LegacyMuxDataPort: cfg.LegacyMuxDataPort,
			Network:           newPDNNetwork(cfg.InterfaceName, false),
		})
		if err != nil {
			return nil, errors.Join(err, client.Close())
		}
		return reader, nil
	case mmodem.ModemPortTypeMbim:
		interfaceName, err := voLTEInterfaceName(modem)
		if err != nil {
			return nil, err
		}
		client, err := mbim.Open(ctx, mbim.WithProxy(port.Device), mbim.WithSlot(int(slot)))
		if err != nil {
			return nil, fmt.Errorf("open MBIM proxy: %w", err)
		}
		reader, err := imsgo.NewMBIMClient(client, imsgo.MBIMClientConfig{
			Network: newPDNNetwork(interfaceName, true),
		})
		if err != nil {
			return nil, errors.Join(err, client.Close())
		}
		return reader, nil
	default:
		return nil, ErrUnavailable
	}
}

func isIMSCallAlreadyPresent(err error) bool {
	const callAlreadyPresent int16 = 236

	var startErr *qcom.WDSStartNetworkError
	return errors.As(err, &startErr) &&
		startErr.HasVerboseCallEndReason &&
		startErr.VerboseCallEndReason.Type == qcom.WDSVerboseCallEndReasonTypeInternal &&
		startErr.VerboseCallEndReason.Reason == callAlreadyPresent
}

func voLTEControlPort(modem *mmodem.Modem) (mmodem.ModemPort, error) {
	if modem == nil {
		return mmodem.ModemPort{}, errors.New("modem is required")
	}
	for _, port := range modem.Ports {
		if port.PortType == mmodem.ModemPortTypeQmi && strings.TrimSpace(port.Device) != "" {
			return port, nil
		}
	}
	for _, port := range modem.Ports {
		if port.PortType == mmodem.ModemPortTypeMbim && strings.TrimSpace(port.Device) != "" {
			return port, nil
		}
	}
	return mmodem.ModemPort{}, ErrUnavailable
}

func voLTESIMSlot(modem *mmodem.Modem) (uint8, error) {
	if modem == nil {
		return 0, errors.New("modem is required")
	}
	if modem.PrimarySimSlot == 0 {
		return 1, nil
	}
	if modem.PrimarySimSlot > 5 {
		return 0, fmt.Errorf("sim slot %d is out of range", modem.PrimarySimSlot)
	}
	return uint8(modem.PrimarySimSlot), nil
}

func openATWWAN(_ context.Context, port mmodem.ModemPort) (usimcard.Reader, error) {
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
