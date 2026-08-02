package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func physicalDeviceKey(device wwanmodem.Device) string {
	if path := strings.TrimSpace(device.PhysicalPath); path != "" {
		return path
	}
	for _, port := range device.Ports {
		if path := strings.TrimSpace(port.SysPath); path != "" {
			return path
		}
	}
	return controlPortPath(device)
}

func samePhysicalModem(a, b *Modem) bool {
	if a == nil || b == nil {
		return false
	}
	if a.EquipmentIdentifier != "" && b.EquipmentIdentifier != "" {
		return a.EquipmentIdentifier == b.EquipmentIdentifier
	}
	return a.Path() != "" && a.Path() == b.Path()
}

func sameControlDevice(a, b wwanmodem.Device) bool {
	for _, aPort := range controlPorts(a) {
		for _, bPort := range controlPorts(b) {
			if strings.TrimSpace(aPort.Path) != "" && strings.TrimSpace(aPort.Path) == strings.TrimSpace(bPort.Path) {
				return true
			}
		}
	}
	return physicalDeviceKey(a) != "" && physicalDeviceKey(a) == physicalDeviceKey(b)
}

func sameDeviceDescription(a, b wwanmodem.Device) bool {
	return a.PhysicalPath == b.PhysicalPath && slices.Equal(a.Ports, b.Ports)
}

func openDiscoveredModem(ctx context.Context, device wwanmodem.Device, generation uint64) (*Modem, error) {
	ports, resolveErr := controlPortsForOpen(ctx, device)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("resolve modem control ports: %w", err)
	}
	if resolveErr != nil {
		// Preserve modem availability on other dual-QMI Qualcomm platforms,
		// while making the fallback visible instead of silently choosing DATA6.
		slog.Warn("use discovered QMI control port order", "physical_path", physicalDeviceKey(device), "error", resolveErr)
	}
	if len(ports) == 0 {
		return nil, errors.New("open discovered modem: no QMI or MBIM control port")
	}
	var openErrs []error
	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("open discovered modem: %w", err)
		}
		core, err := wwanmodem.Open(ctx, port, wwanmodem.AccessAuto)
		if err != nil {
			openErrs = append(openErrs, fmt.Errorf("open control port %s: %w", port.Path, err))
			continue
		}
		info, err := core.Info(ctx)
		if err != nil {
			openErrs = append(openErrs, fmt.Errorf("read modem info from %s: %w", port.Path, errors.Join(err, core.Close())))
			continue
		}
		return newDiscoveredModem(ctx, discoveredModemConfig{
			core:       core,
			device:     device,
			port:       port,
			info:       info,
			generation: generation,
		}), nil
	}
	return nil, fmt.Errorf("open discovered modem: %w", errors.Join(openErrs...))
}

type discoveredModemConfig struct {
	core       *wwanmodem.Modem
	device     wwanmodem.Device
	port       wwanmodem.Port
	info       wwanmodem.Info
	generation uint64
}

func newDiscoveredModem(ctx context.Context, cfg discoveredModemConfig) *Modem {
	m := &Modem{
		core:                cfg.core,
		deviceInfo:          cfg.device,
		deviceKey:           physicalDeviceKey(cfg.device),
		generation:          cfg.generation,
		Device:              cfg.device.PhysicalPath,
		Manufacturer:        cfg.info.Manufacturer,
		EquipmentIdentifier: strings.TrimSpace(cfg.info.EquipmentID),
		Driver:              cfg.port.Driver,
		Model:               cfg.info.Model,
		FirmwareRevision:    cfg.info.Revision,
		HardwareRevision:    cfg.info.HardwareRevision,
		PrimaryPort:         cfg.port.Path,
		ussd:                wwanmodem.USSDMessage{State: wwanmodem.USSDStateIdle},
	}
	if m.Device == "" {
		m.Device = cfg.port.Path
	}
	if len(cfg.info.OwnNumbers) > 0 {
		m.runtimeMu.Lock()
		m.Number = strings.TrimSpace(cfg.info.OwnNumbers[0])
		m.runtimeMu.Unlock()
	}
	for _, candidate := range cfg.device.Ports {
		path := candidate.Path
		if candidate.Type == wwanmodem.PortNetwork {
			path = candidate.Name
		}
		m.Ports = append(m.Ports, ModemPort{PortType: candidate.Type, Device: path})
	}
	// Initial snapshots are best effort; runtime watchers refresh these fields.
	if status, err := cfg.core.Status(ctx); err == nil {
		m.applyStatus(status)
	}
	if simInfo, err := cfg.core.SIMInfo(ctx); err == nil {
		m.applySIMInfo(simInfo)
	}
	if slots, err := cfg.core.SIMSlots(ctx); err == nil {
		m.applySIMSlots(slots)
	}
	return m
}

func controlPorts(device wwanmodem.Device) []wwanmodem.Port {
	return controlPortsWithSameDevice(device, sameDeviceNode)
}

func controlPortsWithSameDevice(device wwanmodem.Device, sameDevice func(string, string) (bool, error)) []wwanmodem.Port {
	ports := listedControlPorts(device)
	if device.Bus != wwanmodem.BusPlatform || !hasMultipleQMIControlPorts(ports) {
		return ports
	}
	// This best-effort path is also used by logging and identity checks. The
	// modem open path performs the bounded retry needed during device reload.
	_ = preferQualcomm410InternetControlPort(ports, sameDevice)
	return ports
}

func listedControlPorts(device wwanmodem.Device) []wwanmodem.Port {
	ports := make([]wwanmodem.Port, 0, len(device.Ports))
	for _, portType := range []wwanmodem.PortType{wwanmodem.PortQMI, wwanmodem.PortMBIM} {
		for _, port := range device.Ports {
			if port.Type == portType && strings.TrimSpace(port.Path) != "" {
				ports = append(ports, port)
			}
		}
	}
	return ports
}

func hasMultipleQMIControlPorts(ports []wwanmodem.Port) bool {
	count := 0
	for _, port := range ports {
		if port.Type != wwanmodem.PortQMI {
			break
		}
		count++
		if count == 2 {
			return true
		}
	}
	return false
}

type controlPortResolver struct {
	sameDevice func(string, string) (bool, error)
	wait       func(context.Context) error
}

func controlPortsForOpen(ctx context.Context, device wwanmodem.Device) ([]wwanmodem.Port, error) {
	return controlPortsForOpenWithResolver(ctx, device, controlPortResolver{
		sameDevice: sameDeviceNode,
		wait: func(ctx context.Context) error {
			return sleepContext(ctx, qualcomm410ControlPortResolveInterval)
		},
	})
}

func controlPortsForOpenWithResolver(ctx context.Context, device wwanmodem.Device, resolver controlPortResolver) ([]wwanmodem.Port, error) {
	ports := listedControlPorts(device)
	if device.Bus != wwanmodem.BusPlatform || !hasMultipleQMIControlPorts(ports) {
		return ports, nil
	}

	attempts := 0
	var resolveErr error
	for attempts < qualcomm410ControlPortResolveAttempts {
		attempts++
		resolveErr = preferQualcomm410InternetControlPort(ports, resolver.sameDevice)
		if resolveErr == nil {
			return ports, nil
		}
		if attempts == qualcomm410ControlPortResolveAttempts || resolver.wait == nil {
			break
		}
		if err := resolver.wait(ctx); err != nil {
			return ports, fmt.Errorf("wait for Qualcomm 410 DATA5 QMI control port: %w", err)
		}
	}
	return ports, fmt.Errorf("resolve Qualcomm 410 DATA5 QMI control port after %d attempts: %w", attempts, resolveErr)
}

func preferQualcomm410InternetControlPort(ports []wwanmodem.Port, sameDevice func(string, string) (bool, error)) error {
	preferred := slices.IndexFunc(ports, func(port wwanmodem.Port) bool {
		return port.Type == wwanmodem.PortQMI && strings.TrimSpace(port.Path) == Qualcomm410InternetQMI
	})
	if preferred < 0 && sameDevice != nil {
		var compareErr error
		for i, port := range ports {
			if port.Type != wwanmodem.PortQMI {
				break
			}
			match, err := sameDevice(Qualcomm410InternetQMI, strings.TrimSpace(port.Path))
			if err != nil {
				compareErr = errors.Join(compareErr, fmt.Errorf("compare DATA5 with QMI control port %s: %w", port.Path, err))
				continue
			}
			if match {
				preferred = i
				break
			}
		}
		if preferred < 0 && compareErr != nil {
			return errors.Join(errQualcomm410Data5PortNotDiscovered, compareErr)
		}
	}
	if preferred < 0 {
		return errQualcomm410Data5PortNotDiscovered
	}
	if preferred <= 0 {
		return nil
	}
	port := ports[preferred]
	copy(ports[1:preferred+1], ports[:preferred])
	ports[0] = port
	return nil
}

func controlPortPath(device wwanmodem.Device) string {
	ports := controlPorts(device)
	if len(ports) == 0 {
		return ""
	}
	return strings.TrimSpace(ports[0].Path)
}
