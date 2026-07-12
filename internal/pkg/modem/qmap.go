package modem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
	"github.com/damonto/wwan-go/qcom/qmi"
)

type QMAPConfig struct {
	APN          string
	IPPreference qcom.WDSIPPreference
	ProfileIndex uint8
	MuxID        uint8
}

type QMAPSession struct {
	reader        *qcom.Client
	pdn           *qcom.PDNSession
	InterfaceName string
	Info          qcom.PDNInfo
}

type PreparedQMAP struct {
	MuxDataPort       *qcom.WDSMuxDataPort
	LegacyMuxDataPort qcom.WDSSIOPort
	InterfaceName     string
}

type qmapMuxInterface struct {
	name  string
	index int
}

var errQMAPMuxNotFound = errors.New("QMAP mux is unavailable")

func PrepareQMAP(ctx context.Context, modem *Modem, muxID uint8) (PreparedQMAP, error) {
	if modem == nil {
		return PreparedQMAP{}, errModemRequired
	}
	legacyMuxDataPort, err := legacyQMAPDataPort(muxID)
	if err != nil {
		return PreparedQMAP{}, err
	}
	port, err := selectQMIDevicePort(modem)
	if err != nil {
		return PreparedQMAP{}, err
	}
	parent, err := qmapParentInterface(modem)
	if err != nil {
		return PreparedQMAP{}, err
	}
	interfaceNumber, err := qmapInterfaceNumber(parent)
	if err != nil {
		return PreparedQMAP{}, err
	}
	transport, err := qmi.Open(ctx, qmi.WithProxy(port.device))
	if err != nil {
		return PreparedQMAP{}, fmt.Errorf("open QMI proxy: %w", err)
	}
	reader, err := qcom.NewClient(transport)
	if err != nil {
		return PreparedQMAP{}, errors.Join(err, transport.Close())
	}
	defer reader.Close()
	if err := ensureQMAP(ctx, reader); err != nil {
		return PreparedQMAP{}, fmt.Errorf("enable QMAP: %w", err)
	}
	interfaceName, err := ensureQMIMux(parent, muxID)
	if err != nil {
		return PreparedQMAP{}, err
	}
	if err := netlink.SetUp(parent); err != nil {
		return PreparedQMAP{}, fmt.Errorf("set QMAP parent up: %w", err)
	}
	return PreparedQMAP{
		MuxDataPort: &qcom.WDSMuxDataPort{
			Endpoint: &qcom.DataEndpoint{Type: qcom.DataEndpointHSUSB, InterfaceID: interfaceNumber},
			MuxID:    muxID,
		},
		LegacyMuxDataPort: legacyMuxDataPort,
		InterfaceName:     interfaceName,
	}, nil
}

func legacyQMAPDataPort(muxID uint8) (qcom.WDSSIOPort, error) {
	if muxID < 1 || muxID > 8 {
		return 0, fmt.Errorf("QMAP mux ID %d is outside legacy RMNET range 1-8", muxID)
	}
	return qcom.WDSSIOPort(uint16(qcom.WDSSIOPortA2MuxRMNET0) + uint16(muxID-1)), nil
}

func OpenQMAPSession(ctx context.Context, modem *Modem, cfg QMAPConfig) (*QMAPSession, error) {
	if modem == nil {
		return nil, errModemRequired
	}
	if cfg.MuxID == 0 {
		return nil, errors.New("QMAP mux ID is required")
	}
	port, err := selectQMIDevicePort(modem)
	if err != nil {
		return nil, err
	}
	parent, err := qmapParentInterface(modem)
	if err != nil {
		return nil, err
	}
	interfaceNumber, err := qmapInterfaceNumber(parent)
	if err != nil {
		return nil, err
	}
	transport, err := qmi.Open(ctx, qmi.WithProxy(port.device))
	if err != nil {
		return nil, fmt.Errorf("open QMI proxy: %w", err)
	}
	reader, err := qcom.NewClient(transport)
	if err != nil {
		return nil, errors.Join(err, transport.Close())
	}
	if err := ensureQMAP(ctx, reader); err != nil {
		return nil, errors.Join(fmt.Errorf("enable QMAP: %w", err), reader.Close())
	}
	if cfg.ProfileIndex == 0 && strings.TrimSpace(cfg.APN) != "" {
		profileIndex, profileErr := reader.WDSProfileIndex(ctx, cfg.APN)
		switch {
		case profileErr == nil:
			cfg.ProfileIndex = profileIndex
		case !errors.Is(profileErr, qcom.ErrWDSProfileNotFound):
			return nil, errors.Join(fmt.Errorf("find QMAP profile: %w", profileErr), reader.Close())
		}
	}
	interfaceName, err := ensureQMIMux(parent, cfg.MuxID)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	if err := netlink.SetUp(parent); err != nil {
		return nil, errors.Join(fmt.Errorf("set QMAP parent up: %w", err), reader.Close())
	}
	pdn, err := reader.OpenPDN(ctx, qcom.PDNConfig{
		APN:          cfg.APN,
		IPPreference: cfg.IPPreference,
		ProfileIndex: cfg.ProfileIndex,
		MuxDataPort: &qcom.WDSMuxDataPort{
			Endpoint: &qcom.DataEndpoint{Type: qcom.DataEndpointHSUSB, InterfaceID: interfaceNumber},
			MuxID:    cfg.MuxID,
		},
	})
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return &QMAPSession{
		reader:        reader,
		pdn:           pdn,
		InterfaceName: interfaceName,
		Info:          pdn.Info(),
	}, nil
}

func (s *QMAPSession) Close() error {
	if s == nil {
		return nil
	}
	stopErr := s.pdn.Close()
	return errors.Join(stopErr, s.reader.Close())
}

func ensureQMAP(ctx context.Context, reader *qcom.Client) error {
	format, err := reader.WDADataFormat(ctx)
	if err == nil && isQMAP(format) {
		return nil
	}
	rawIP := qcom.WDALinkLayerRawIP
	qmap := qcom.WDAAggregationQMAP
	_, err = reader.SetWDADataFormat(ctx, qcom.WDADataFormatConfig{
		LinkLayerProtocol: &rawIP, UplinkAggregation: &qmap, DownlinkAggregation: &qmap,
	})
	if err != nil {
		format, getErr := reader.WDADataFormat(ctx)
		if getErr == nil && isQMAP(format) {
			return nil
		}
		// Some Qualcomm firmware keeps QMAP active while WDA starts returning
		// Internal for every new service client. Binding a WDS client to an
		// existing mux remains valid, so let that operation be authoritative.
		if errors.Is(err, qcom.QMIErrorInternal) {
			return nil
		}
	}
	return err
}

func isQMAP(format qcom.WDADataFormat) bool {
	return format.UplinkAggregationKnown && format.UplinkAggregation == qcom.WDAAggregationQMAP &&
		format.DownlinkAggregationKnown && format.DownlinkAggregation == qcom.WDAAggregationQMAP
}

func qmapParentInterface(modem *Modem) (string, error) {
	for _, port := range modem.Ports {
		if port.PortType == ModemPortTypeNet && strings.TrimSpace(port.Device) != "" {
			return filepath.Base(strings.TrimSpace(port.Device)), nil
		}
	}
	return "", errors.New("QMAP parent interface is unavailable")
}

func qmapInterfaceNumber(parent string) (uint32, error) {
	target, err := os.Readlink(filepath.Join("/sys/class/net", parent, "device"))
	if err != nil {
		return 0, fmt.Errorf("read QMAP USB interface: %w", err)
	}
	_, value, ok := strings.Cut(filepath.Base(target), ":1.")
	if !ok {
		return 0, fmt.Errorf("parse QMAP USB interface %q", target)
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse QMAP USB interface number: %w", err)
	}
	return uint32(n), nil
}

func ensureQMIMux(parent string, muxID uint8) (string, error) {
	name, err := qmapMuxInterfaceName(parent, muxID)
	if err == nil {
		return name, nil
	}
	if !errors.Is(err, errQMAPMuxNotFound) {
		return "", err
	}
	path := filepath.Join("/sys/class/net", parent, "qmi", "add_mux")
	if err := os.WriteFile(path, []byte(strconv.Itoa(int(muxID))), 0); err != nil && !errors.Is(err, syscall.EINVAL) {
		return "", fmt.Errorf("create QMAP mux %d: %w", muxID, err)
	}
	name, err = qmapMuxInterfaceName(parent, muxID)
	if err != nil {
		return "", fmt.Errorf("find QMAP mux %d interface: %w", muxID, err)
	}
	return name, nil
}

func qmapMuxInterfaceName(parent string, muxID uint8) (string, error) {
	ids, err := qmapMuxIDs(parent)
	if err != nil {
		return "", err
	}
	interfaces, err := qmapMuxInterfaces(parent)
	if err != nil {
		return "", err
	}
	return matchQMAPMuxInterface(muxID, ids, interfaces)
}

func qmapMuxIDs(parent string) ([]uint8, error) {
	path := filepath.Join("/sys/class/net", parent, "qmi", "add_mux")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read QMAP mux IDs: %w", err)
	}
	fields := strings.Fields(string(data))
	ids := make([]uint8, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.ParseUint(field, 0, 8)
		if err != nil {
			return nil, fmt.Errorf("parse QMAP mux ID %q: %w", field, err)
		}
		ids = append(ids, uint8(value))
	}
	return ids, nil
}

func qmapMuxInterfaces(parent string) ([]qmapMuxInterface, error) {
	dir := filepath.Join("/sys/class/net", parent)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list QMAP interfaces: %w", err)
	}
	var interfaces []qmapMuxInterface
	for _, entry := range entries {
		name, ok := strings.CutPrefix(entry.Name(), "upper_")
		if !ok || !strings.HasPrefix(name, "qmimux") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/sys/class/net", name, "ifindex"))
		if err != nil {
			return nil, fmt.Errorf("read QMAP interface %s index: %w", name, err)
		}
		index, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parse QMAP interface %s index: %w", name, err)
		}
		interfaces = append(interfaces, qmapMuxInterface{name: name, index: index})
	}
	slices.SortFunc(interfaces, func(a, b qmapMuxInterface) int {
		return a.index - b.index
	})
	return interfaces, nil
}

func matchQMAPMuxInterface(muxID uint8, ids []uint8, interfaces []qmapMuxInterface) (string, error) {
	if len(ids) != len(interfaces) {
		return "", fmt.Errorf("QMAP mux count %d does not match interface count %d", len(ids), len(interfaces))
	}
	for i, id := range ids {
		if id == muxID {
			return interfaces[i].name, nil
		}
	}
	return "", fmt.Errorf("%w: %d", errQMAPMuxNotFound, muxID)
}
