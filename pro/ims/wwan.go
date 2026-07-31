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
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

// WWANConfig selects the modem access used by an IMS client.
type WWANConfig struct {
	Access            Access
	QMIControlPort    string
	MuxDataPort       *qcom.WDSMuxDataPort
	LegacyMuxDataPort qcom.WDSSIOPort
	InterfaceName     string
}

var validateQualcomm410Layout = mmodem.ValidateQualcomm410Layout

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
	endpoint, err := voLTEEndpoint(modem)
	if err != nil {
		return nil, err
	}
	port := endpoint.Port
	if strings.TrimSpace(cfg.QMIControlPort) != "" {
		if port.PortType != mmodem.ModemPortTypeQmi {
			return nil, errors.New("dedicated QMI control port requires a QMI VoLTE modem")
		}
		port.Device = strings.TrimSpace(cfg.QMIControlPort)
	}
	dedicatedQMI := strings.TrimSpace(cfg.QMIControlPort) != ""
	slot := endpoint.SIMSlot
	switch port.PortType {
	case mmodem.ModemPortTypeQmi:
		if dedicatedQMI {
			if cfg.MuxDataPort != nil || cfg.LegacyMuxDataPort != 0 {
				return nil, errors.New("dedicated QMI cannot bind a data port")
			}
			if err := validateDedicatedQMIWWANConfig(cfg); err != nil {
				return nil, err
			}
		} else {
			if cfg.MuxDataPort == nil && cfg.LegacyMuxDataPort == 0 {
				return nil, errors.New("QMI VoLTE data path is required")
			}
			if cfg.MuxDataPort != nil && cfg.LegacyMuxDataPort != 0 {
				return nil, errors.New("QMAP and legacy BAM-DMUX data ports are mutually exclusive")
			}
		}
		clientConfig := wwan.QMIClientConfig{Device: port.Device, Slot: slot}
		client, err := wwan.OpenQMIClient(ctx, clientConfig)
		if err != nil {
			return nil, fmt.Errorf("open QMI client: %w", err)
		}
		if err := client.ActivateSlot(ctx); err != nil {
			return nil, errors.Join(fmt.Errorf("activate QMI SIM slot: %w", err), client.Close())
		}
		if dedicatedQMI {
			if err := ensureVoLTEQMIDataFormat(ctx, client); err != nil {
				return nil, errors.Join(fmt.Errorf("set dedicated QMI raw IP: %w", err), client.Close())
			}
		}
		network := newPDNNetwork(cfg.InterfaceName, false)
		if dedicatedQMI {
			network = newDedicatedPDNNetwork(cfg.InterfaceName)
		}
		reader, err := imsgo.NewQCOMClient(client, imsgo.QCOMClientConfig{
			MuxDataPort:       cfg.MuxDataPort,
			LegacyMuxDataPort: cfg.LegacyMuxDataPort,
			Network:           network,
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
		client, err := mbim.Open(ctx, mbim.WithAutoDetect(port.Device), mbim.WithSlot(int(slot)))
		if err != nil {
			return nil, fmt.Errorf("open MBIM transport: %w", err)
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

func validateDedicatedQMIWWANConfig(cfg WWANConfig) error {
	controlPort := strings.TrimSpace(cfg.QMIControlPort)
	if controlPort == "" {
		return nil
	}
	if controlPort != mmodem.Qualcomm410IMSQMI {
		return fmt.Errorf("unsupported dedicated QMI control port %q", controlPort)
	}
	if strings.TrimSpace(cfg.InterfaceName) != mmodem.Qualcomm410IMSInterface {
		return fmt.Errorf("Qualcomm 410 IMS interface must be %s", mmodem.Qualcomm410IMSInterface)
	}
	if err := validateQualcomm410Layout(); err != nil {
		return fmt.Errorf("validate Qualcomm 410 layout: %w", err)
	}
	return nil
}

func ensureVoLTEQMIDataFormat(ctx context.Context, client *qcom.Client) error {
	format, err := client.WDADataFormat(ctx)
	if err == nil && format.LinkLayerProtocolKnown && format.LinkLayerProtocol == qcom.WDALinkLayerRawIP {
		return nil
	}
	if err := client.SetWDALinkLayerProtocol(ctx, qcom.WDALinkLayerRawIP); err != nil {
		format, readErr := client.WDADataFormat(ctx)
		if readErr == nil && format.LinkLayerProtocolKnown && format.LinkLayerProtocol == qcom.WDALinkLayerRawIP {
			return nil
		}
		return err
	}
	return nil
}

func isIMSCallAlreadyPresent(err error) bool {
	var startErr *qcom.WDSStartNetworkError
	return errors.As(err, &startErr) &&
		startErr.HasVerboseCallEndReason &&
		startErr.VerboseCallEndReason.Type == qcom.WDSVerboseCallEndReasonTypeInternal &&
		startErr.VerboseCallEndReason.Reason == qcom.WDSVerboseCallEndReasonInternalCallAlreadyPresent
}

func voLTEEndpoint(modem *mmodem.Modem) (mmodem.DeviceEndpoint, error) {
	endpoint, err := mmodem.ResolveVoLTEEndpoint(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return mmodem.DeviceEndpoint{}, ErrUnavailable
	}
	return endpoint, err
}

func voLTEPort(modem *mmodem.Modem) (mmodem.ModemPort, error) {
	port, err := mmodem.ResolveVoLTEPort(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return mmodem.ModemPort{}, ErrUnavailable
	}
	return port, err
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
