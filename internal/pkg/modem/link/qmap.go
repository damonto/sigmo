package link

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

type QMAPConfig struct {
	APN          string
	IPPreference qcom.WDSIPPreference
	ProfileIndex uint8
	MuxID        uint8
}

type QMAPSession struct {
	client        *qcom.Client
	pdn           *qcom.PDNSession
	InterfaceName string
	Info          qcom.PDNInfo
}

type PreparedQMAP struct {
	MuxDataPort   *qcom.WDSMuxDataPort
	InterfaceName string
}

type qmapMuxInterface struct {
	name  string
	index int
}

type wdaDataFormatter interface {
	WDADataFormat(context.Context) (qcom.WDADataFormat, error)
	WDADataFormatForEndpoint(context.Context, *qcom.DataEndpoint) (qcom.WDADataFormat, error)
	SetWDADataFormat(context.Context, qcom.WDADataFormatConfig) (qcom.WDADataFormat, error)
}

var errQMAPMuxNotFound = errors.New("QMAP mux is unavailable")
var errModemRequired = errors.New("modem is required")

func PrepareQMAP(ctx context.Context, modem *mmodem.Modem, muxID uint8) (PreparedQMAP, error) {
	if modem == nil {
		return PreparedQMAP{}, errModemRequired
	}
	if muxID == 0 {
		return PreparedQMAP{}, errors.New("QMAP mux ID is required")
	}
	if muxID > 8 {
		return PreparedQMAP{}, fmt.Errorf("QMAP mux ID %d is outside supported range 1-8", muxID)
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
	client, err := wwan.OpenQMIClient(ctx, wwan.QMIClientConfig{Device: port.Device})
	if err != nil {
		return PreparedQMAP{}, err
	}
	defer func() {
		if err := client.Close(); err != nil {
			slog.Warn("close QMI client after preparing QMAP", "device", port.Device, "error", err)
		}
	}()
	if err := ensureQMAP(ctx, client); err != nil {
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
		InterfaceName: interfaceName,
	}, nil
}

func RestoreNonQMAPDataFormat(ctx context.Context, modem *mmodem.Modem) error {
	if modem == nil {
		return errModemRequired
	}
	port, err := selectQMIDevicePort(modem)
	if err != nil {
		return err
	}
	parent, err := qmapParentInterface(modem)
	if err != nil {
		return err
	}
	linkLayer, rawIPSupported, err := nonQMAPLinkLayer(parent)
	if err != nil {
		return err
	}
	if err := netlink.SetDown(parent); err != nil {
		return fmt.Errorf("set non-QMAP interface down: %w", err)
	}
	client, err := wwan.OpenQMIClient(ctx, wwan.QMIClientConfig{Device: port.Device})
	if err != nil {
		return err
	}
	defer func() {
		if err := client.Close(); err != nil {
			slog.Warn("close QMI client after restoring data format", "device", port.Device, "error", err)
		}
	}()

	var endpoint *qcom.DataEndpoint
	interfaceNumber, endpointErr := qmapInterfaceNumber(parent)
	if endpointErr == nil {
		endpoint = &qcom.DataEndpoint{Type: qcom.DataEndpointHSUSB, InterfaceID: interfaceNumber}
	}
	if err := restoreNonQMAPWDADataFormat(ctx, client, linkLayer, endpoint); err != nil {
		formatErr := fmt.Errorf("restore non-QMAP data format: %w", err)
		if endpoint == nil && errors.Is(err, qcom.QMIErrorMissingArgument) {
			return errors.Join(formatErr, fmt.Errorf("resolve WDA endpoint: %w", endpointErr))
		}
		return formatErr
	}
	if rawIPSupported {
		if err := syncNonQMAPHostDataFormat(filepath.Join("/sys/class/net", parent, "qmi"), linkLayer); err != nil {
			return err
		}
	}
	return nil
}

func restoreNonQMAPWDADataFormat(ctx context.Context, client wdaDataFormatter, linkLayer qcom.WDALinkLayerProtocol, endpoint *qcom.DataEndpoint) error {
	format, err := client.WDADataFormat(ctx)
	var usedEndpoint *qcom.DataEndpoint
	if errors.Is(err, qcom.QMIErrorMissingArgument) && endpoint != nil {
		usedEndpoint = endpoint
		format, err = client.WDADataFormatForEndpoint(ctx, usedEndpoint)
	}
	if err != nil {
		return fmt.Errorf("query QMI WDA data format: %w", err)
	}
	if isNonQMAP(format, linkLayer) {
		return nil
	}

	disabled := qcom.WDAAggregationDisabled
	config := qcom.WDADataFormatConfig{
		LinkLayerProtocol:   &linkLayer,
		UplinkAggregation:   &disabled,
		DownlinkAggregation: &disabled,
		Endpoint:            usedEndpoint,
	}
	_, err = client.SetWDADataFormat(ctx, config)
	if errors.Is(err, qcom.QMIErrorMissingArgument) && usedEndpoint == nil && endpoint != nil {
		usedEndpoint = endpoint
		config.Endpoint = usedEndpoint
		_, err = client.SetWDADataFormat(ctx, config)
	}
	if err != nil {
		current, getErr := queryWDADataFormat(ctx, client, usedEndpoint)
		if getErr == nil && isNonQMAP(current, linkLayer) {
			return nil
		}
		return fmt.Errorf("set QMI WDA data format: %w", err)
	}

	format, err = queryWDADataFormat(ctx, client, usedEndpoint)
	if err != nil {
		return fmt.Errorf("verify QMI WDA data format: %w", err)
	}
	if !isNonQMAP(format, linkLayer) {
		return errors.New("QMI WDA data format did not switch to non-QMAP mode")
	}
	return nil
}

func queryWDADataFormat(ctx context.Context, client wdaDataFormatter, endpoint *qcom.DataEndpoint) (qcom.WDADataFormat, error) {
	if endpoint == nil {
		return client.WDADataFormat(ctx)
	}
	return client.WDADataFormatForEndpoint(ctx, endpoint)
}

func nonQMAPLinkLayer(parent string) (qcom.WDALinkLayerProtocol, bool, error) {
	rawIP, err := os.ReadFile(filepath.Join("/sys/class/net", parent, "qmi", "raw_ip"))
	if err == nil {
		linkLayer, err := nonQMAPLinkLayerForRawIP(string(rawIP))
		return linkLayer, true, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, false, fmt.Errorf("read QMI raw IP mode: %w", err)
	}
	interfaceState, err := net.InterfaceByName(parent)
	if err != nil {
		return 0, false, fmt.Errorf("find non-QMAP interface: %w", err)
	}
	return nonQMAPLinkLayerForFlags(interfaceState.Flags), false, nil
}

func nonQMAPLinkLayerForRawIP(rawIP string) (qcom.WDALinkLayerProtocol, error) {
	switch strings.ToUpper(strings.TrimSpace(rawIP)) {
	case "Y", "N":
		// The sysfs capability means qmi_wwan supports raw IP. Its current
		// value is state, not the preferred format for the next bearer.
		return qcom.WDALinkLayerRawIP, nil
	default:
		return 0, fmt.Errorf("unexpected QMI raw IP mode %q", strings.TrimSpace(rawIP))
	}
}

func nonQMAPLinkLayerForFlags(flags net.Flags) qcom.WDALinkLayerProtocol {
	if flags&net.FlagPointToPoint != 0 {
		return qcom.WDALinkLayerRawIP
	}
	return qcom.WDALinkLayerEthernet
}

func syncNonQMAPHostDataFormat(qmiDir string, linkLayer qcom.WDALinkLayerProtocol) error {
	if err := writeOptionalQMIDataFormat(filepath.Join(qmiDir, "pass_through"), "N"); err != nil {
		return fmt.Errorf("disable QMI pass-through mode: %w", err)
	}
	rawIP := "N"
	if linkLayer == qcom.WDALinkLayerRawIP {
		rawIP = "Y"
	}
	rawIPPath := filepath.Join(qmiDir, "raw_ip")
	if err := os.WriteFile(rawIPPath, []byte(rawIP), 0); err != nil {
		return fmt.Errorf("set QMI raw IP mode to %s: %w", rawIP, err)
	}
	current, err := os.ReadFile(rawIPPath)
	if err != nil {
		return fmt.Errorf("verify QMI raw IP mode: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(current)), rawIP) {
		return fmt.Errorf("QMI raw IP mode is %q, want %s", strings.TrimSpace(string(current)), rawIP)
	}
	return nil
}

func writeOptionalQMIDataFormat(path, value string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(path, []byte(value), 0)
}

func isNonQMAP(format qcom.WDADataFormat, linkLayer qcom.WDALinkLayerProtocol) bool {
	return format.LinkLayerProtocolKnown && format.LinkLayerProtocol == linkLayer &&
		format.UplinkAggregationKnown && format.UplinkAggregation == qcom.WDAAggregationDisabled &&
		format.DownlinkAggregationKnown && format.DownlinkAggregation == qcom.WDAAggregationDisabled
}

func OpenQMAPSession(ctx context.Context, modem *mmodem.Modem, cfg QMAPConfig) (*QMAPSession, error) {
	if modem == nil {
		return nil, errModemRequired
	}
	if cfg.MuxID == 0 {
		return nil, errors.New("QMAP mux ID is required")
	}
	if cfg.IPPreference != qcom.WDSIPPreferenceIPv4 && cfg.IPPreference != qcom.WDSIPPreferenceIPv6 {
		return nil, errors.New("QMAP IP preference is required")
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
	client, err := wwan.OpenQMIClient(ctx, wwan.QMIClientConfig{Device: port.Device})
	if err != nil {
		return nil, err
	}
	if err := ensureQMAP(ctx, client); err != nil {
		return nil, errors.Join(fmt.Errorf("enable QMAP: %w", err), client.Close())
	}
	interfaceName, err := ensureQMIMux(parent, cfg.MuxID)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	if err := netlink.SetUp(parent); err != nil {
		return nil, errors.Join(fmt.Errorf("set QMAP parent up: %w", err), client.Close())
	}
	if cfg.ProfileIndex == 0 && strings.TrimSpace(cfg.APN) != "" {
		profileIndex, profileErr := wdsProfileIndex(ctx, client, cfg.APN, cfg.IPPreference)
		switch {
		case profileErr == nil:
			cfg.ProfileIndex = profileIndex
		case !errors.Is(profileErr, qcom.ErrWDSProfileNotFound):
			return nil, errors.Join(fmt.Errorf("find QMAP profile: %w", profileErr), client.Close())
		}
	}
	pdn, err := client.OpenPDN(ctx, qcom.PDNConfig{
		APN:          cfg.APN,
		IPPreference: cfg.IPPreference,
		ProfileIndex: cfg.ProfileIndex,
		MuxDataPort: &qcom.WDSMuxDataPort{
			Endpoint: &qcom.DataEndpoint{Type: qcom.DataEndpointHSUSB, InterfaceID: interfaceNumber},
			MuxID:    cfg.MuxID,
		},
	})
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return &QMAPSession{client: client, pdn: pdn, InterfaceName: interfaceName, Info: pdn.Info()}, nil
}

func (s *QMAPSession) Close() error {
	if s == nil {
		return nil
	}
	var pdnErr, clientErr error
	if s.pdn != nil {
		pdnErr = qmapStopError(s.pdn.Close())
	}
	if s.client != nil {
		clientErr = s.client.Close()
	}
	return errors.Join(pdnErr, clientErr)
}

func qmapStopError(err error) error {
	if errors.Is(err, qcom.QMIErrorNoEffect) {
		return nil
	}
	return err
}

func ensureQMAP(ctx context.Context, client *qcom.Client) error {
	format, err := client.WDADataFormat(ctx)
	if err == nil && isQMAP(format) {
		return nil
	}
	rawIP := qcom.WDALinkLayerRawIP
	qmap := qcom.WDAAggregationQMAP
	_, err = client.SetWDADataFormat(ctx, qcom.WDADataFormatConfig{
		LinkLayerProtocol: &rawIP, UplinkAggregation: &qmap, DownlinkAggregation: &qmap,
	})
	if err != nil {
		format, getErr := client.WDADataFormat(ctx)
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

func qmapParentInterface(modem *mmodem.Modem) (string, error) {
	for _, port := range modem.Ports {
		if port.PortType == wwanmodem.PortNetwork && strings.TrimSpace(port.Device) != "" {
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

// RemoveQMAPMuxes removes mux netdevs after their WDS sessions have stopped.
func RemoveQMAPMuxes(modem *mmodem.Modem, muxIDs ...uint8) error {
	if modem == nil {
		return errModemRequired
	}
	parent, err := qmapParentInterface(modem)
	if err != nil {
		return err
	}
	current, err := qmapMuxIDs(parent)
	if err != nil {
		return err
	}
	path := filepath.Join("/sys/class/net", parent, "qmi", "del_mux")
	var result error
	for _, muxID := range slices.Backward(muxIDs) {

		if !slices.Contains(current, muxID) {
			continue
		}
		if err := os.WriteFile(path, []byte(strconv.Itoa(int(muxID))), 0); err != nil && !errors.Is(err, syscall.EINVAL) {
			result = errors.Join(result, fmt.Errorf("delete QMAP mux %d: %w", muxID, err))
		}
	}
	return result
}

func selectQMIDevicePort(modem *mmodem.Modem) (mmodem.ModemPort, error) {
	primary := strings.TrimSpace(modem.PrimaryPort)
	if primary == "" {
		return mmodem.ModemPort{}, wwan.ErrUnsupported
	}
	for _, port := range modem.Ports {
		if port.PortType != wwanmodem.PortQMI || strings.TrimSpace(port.Device) == "" {
			continue
		}
		if strings.TrimSpace(port.Device) == primary {
			return port, nil
		}
	}
	return mmodem.ModemPort{}, wwan.ErrUnsupported
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
