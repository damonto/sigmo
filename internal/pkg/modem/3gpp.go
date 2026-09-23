package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
	modemmbim "github.com/damonto/wwan-go/modem/mbim"
	modemqmi "github.com/damonto/wwan-go/modem/qmi"

	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

var errOperatorCodeRequired = errors.New("operator code is required")

type ThreeGPP struct{ modem *Modem }

func (m *Modem) ThreeGPP() *ThreeGPP { return &ThreeGPP{modem: m} }

func (g *ThreeGPP) IMEI(context.Context) (string, error) {
	if g == nil || g.modem == nil {
		return "", errModemRequired
	}
	return g.modem.EquipmentIdentifier, nil
}

func (g *ThreeGPP) RegistrationState(ctx context.Context) (wwanmodem.RegistrationState, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return wwanmodem.RegistrationUnknown, err
	}
	return status.Registration, nil
}

func (g *ThreeGPP) OperatorCode(ctx context.Context) (string, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status.OperatorID), nil
}

func (g *ThreeGPP) OperatorName(ctx context.Context) (string, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status.OperatorName), nil
}

func (g *ThreeGPP) ScanNetworks(ctx context.Context) ([]wwanmodem.Operator, error) {
	if g == nil || g.modem == nil {
		return nil, errModemRequired
	}
	return g.modem.scanNetworks(ctx)
}

// scanNetworks uses a short-lived, independently owned protocol client for
// the modem-wide scan. QMI and MBIM request dispatchers can then continue
// serving ordinary status/settings requests while the firmware performs the
// slow full-band operation. QMI and MBIM deliberately do not fall back to the
// generation client: doing so would silently restore the head-of-line blocking
// this path exists to avoid.
func (m *Modem) scanNetworks(ctx context.Context) ([]wwanmodem.Operator, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	select {
	case <-m.Done():
		return nil, wwanmodem.ErrClosed
	default:
	}

	protocol := m.core.Protocol()
	if protocol != wwanmodem.ProtocolQMI && protocol != wwanmodem.ProtocolMBIM {
		return m.core.ScanNetworks(ctx)
	}

	slot := uint8(m.Snapshot().PrimarySIMSlot)
	if slot == 0 || slot > maxSIMSlot {
		slot = 1
	}
	return runIsolatedNetworkScan(ctx, isolatedNetworkScanConfig{
		protocol: protocol,
		portPath: m.core.Port().Path,
		slot:     slot,
		openQMI: func(ctx context.Context, path string, slot uint8) (networkScanner, error) {
			client, err := m.core.QMIClient(ctx, slot)
			if err != nil {
				return nil, err
			}
			return modemqmi.New(client, path), nil
		},
		openMBIM: func(ctx context.Context, path string, slot uint8) (networkScanner, error) {
			client, err := m.core.MBIMClient(ctx, slot)
			if err != nil {
				return nil, err
			}
			return modemmbim.New(client, path), nil
		},
	})
}

type networkScanner interface {
	ScanNetworks(context.Context) ([]wwanmodem.Operator, error)
	Close() error
}

type networkScannerOpener func(context.Context, string, uint8) (networkScanner, error)

type isolatedNetworkScanConfig struct {
	protocol wwanmodem.Protocol
	portPath string
	slot     uint8
	openQMI  networkScannerOpener
	openMBIM networkScannerOpener
}

func runIsolatedNetworkScan(ctx context.Context, cfg isolatedNetworkScanConfig) ([]wwanmodem.Operator, error) {
	path := strings.TrimSpace(cfg.portPath)
	if path == "" {
		return nil, errors.New("network scan control port is required")
	}

	var open networkScannerOpener
	switch cfg.protocol {
	case wwanmodem.ProtocolQMI:
		open = cfg.openQMI
	case wwanmodem.ProtocolMBIM:
		open = cfg.openMBIM
	default:
		return nil, fmt.Errorf("network scan protocol %s: %w", cfg.protocol, wwanmodem.ErrNotSupported)
	}
	if open == nil {
		return nil, fmt.Errorf("open isolated %s network scanner: opener is required", cfg.protocol)
	}

	scanner, err := open(ctx, path, cfg.slot)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("open isolated %s network scanner: %w", cfg.protocol, err)
	}
	if scanner == nil {
		return nil, fmt.Errorf("open isolated %s network scanner: scanner is nil", cfg.protocol)
	}
	networks, scanErr := scanner.ScanNetworks(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		scanErr = ctxErr
	}
	if closeErr := scanner.Close(); closeErr != nil {
		// The scan result is already known. A cleanup failure must be visible to
		// operators, but it must not turn a successful scan into a user failure.
		slog.Warn("close isolated network scanner", "protocol", cfg.protocol, "path", path, "error", closeErr)
	}
	return networks, scanErr
}

// RegisterNetwork pins the modem to one operator. The selection persists in
// modem firmware across SIM profile switches and restarts; use
// RegisterNetworkAutomatically to release it.
func (g *ThreeGPP) RegisterNetwork(ctx context.Context, operatorCode string) error {
	if g == nil || g.modem == nil || g.modem.core == nil {
		return errModemRequired
	}
	operatorCode = strings.TrimSpace(operatorCode)
	if operatorCode == "" {
		return errOperatorCodeRequired
	}
	return g.register(ctx, wwanmodem.RegisterConfig{OperatorID: operatorCode})
}

// RegisterNetworkAutomatically hands network selection back to the modem.
func (g *ThreeGPP) RegisterNetworkAutomatically(ctx context.Context) error {
	if g == nil || g.modem == nil || g.modem.core == nil {
		return errModemRequired
	}
	return g.register(ctx, wwanmodem.RegisterConfig{})
}

func (g *ThreeGPP) register(ctx context.Context, cfg wwanmodem.RegisterConfig) error {
	if err := g.modem.core.Register(ctx, cfg); err != nil {
		return err
	}
	g.modem.markNetworkStateChanged()
	return nil
}

// NetworkSelection reads the modem-wide selection preference through the
// generation-scoped QMI or MBIM control session. wwan-go's NetworkStatus does
// not expose it, yet restore logic must tell "registered on this operator
// automatically" apart from "pinned to this operator".
func (g *ThreeGPP) NetworkSelection(ctx context.Context) (wwan.NetworkSelection, error) {
	if g == nil || g.modem == nil {
		return wwan.NetworkSelection{}, errModemRequired
	}
	device, err := OpenDevice(g.modem)
	if err != nil {
		return wwan.NetworkSelection{}, fmt.Errorf("open modem control session: %w", err)
	}
	return device.NetworkSelection(ctx)
}
