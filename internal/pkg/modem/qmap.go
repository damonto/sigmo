package modem

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	client        *qcom.Client
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
	client, err := qcom.NewClient(transport)
	if err != nil {
		return PreparedQMAP{}, errors.Join(err, transport.Close())
	}
	defer client.Close()
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
		LegacyMuxDataPort: legacyMuxDataPort,
		InterfaceName:     interfaceName,
	}, nil
}

func RestoreNonQMAPDataFormat(ctx context.Context, modem *Modem) error {
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
	linkLayer, err := nonQMAPLinkLayer(parent)
	if err != nil {
		return err
	}
	transport, err := qmi.Open(ctx, qmi.WithProxy(port.device))
	if err != nil {
		return fmt.Errorf("open QMI proxy: %w", err)
	}
	client, err := qcom.NewClient(transport)
	if err != nil {
		return errors.Join(err, transport.Close())
	}
	defer client.Close()
	disabled := qcom.WDAAggregationDisabled
	_, err = client.SetWDADataFormat(ctx, qcom.WDADataFormatConfig{
		LinkLayerProtocol:   &linkLayer,
		UplinkAggregation:   &disabled,
		DownlinkAggregation: &disabled,
	})
	if err != nil {
		format, getErr := client.WDADataFormat(ctx)
		if getErr == nil && isNonQMAP(format, linkLayer) {
			return nil
		}
		return fmt.Errorf("restore non-QMAP data format: %w", err)
	}
	return nil
}

func nonQMAPLinkLayer(parent string) (qcom.WDALinkLayerProtocol, error) {
	rawIP, err := os.ReadFile(filepath.Join("/sys/class/net", parent, "qmi", "raw_ip"))
	if err == nil {
		return nonQMAPLinkLayerForState(string(rawIP), true, 0), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("read QMI raw IP mode: %w", err)
	}
	interfaceState, err := net.InterfaceByName(parent)
	if err != nil {
		return 0, fmt.Errorf("find non-QMAP interface: %w", err)
	}
	return nonQMAPLinkLayerForState("", false, interfaceState.Flags), nil
}

func nonQMAPLinkLayerForRawIP(rawIP string) qcom.WDALinkLayerProtocol {
	return nonQMAPLinkLayerForState(rawIP, true, 0)
}

func nonQMAPLinkLayerForState(rawIP string, rawIPKnown bool, flags net.Flags) qcom.WDALinkLayerProtocol {
	if !rawIPKnown {
		if flags&net.FlagPointToPoint != 0 {
			return qcom.WDALinkLayerRawIP
		}
		return qcom.WDALinkLayerEthernet
	}
	if strings.EqualFold(strings.TrimSpace(rawIP), "Y") {
		return qcom.WDALinkLayerRawIP
	}
	return qcom.WDALinkLayerEthernet
}

func isNonQMAP(format qcom.WDADataFormat, linkLayer qcom.WDALinkLayerProtocol) bool {
	return format.LinkLayerProtocolKnown && format.LinkLayerProtocol == linkLayer &&
		format.UplinkAggregationKnown && format.UplinkAggregation == qcom.WDAAggregationDisabled &&
		format.DownlinkAggregationKnown && format.DownlinkAggregation == qcom.WDAAggregationDisabled
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
	transport, err := qmi.Open(ctx, qmi.WithProxy(port.device))
	if err != nil {
		return nil, fmt.Errorf("open QMI proxy: %w", err)
	}
	client, err := qcom.NewClient(transport)
	if err != nil {
		return nil, errors.Join(err, transport.Close())
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
		profileIndex, profileErr := qmapProfileIndex(ctx, client, cfg.APN, cfg.IPPreference)
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

func qmapProfileIndex(ctx context.Context, client *qcom.Client, apn string, preference qcom.WDSIPPreference) (uint8, error) {
	profiles, err := client.WDSProfiles(ctx, qcom.WDSProfileType3GPP)
	if err != nil {
		return 0, err
	}
	settings := make([]qcom.WDSProfileSettings, 0, len(profiles))
	for _, profile := range profiles {
		profileSettings, err := client.WDSProfileSettings(ctx, profile.ID)
		if err != nil {
			return 0, err
		}
		settings = append(settings, profileSettings)
	}
	return selectQMAPProfileIndex(apn, preference, settings)
}

func selectQMAPProfileIndex(apn string, preference qcom.WDSIPPreference, profiles []qcom.WDSProfileSettings) (uint8, error) {
	apn = strings.TrimSpace(apn)
	if apn == "" {
		return 0, errors.New("QMAP APN is required")
	}
	var want qcom.WDSPDPType
	switch preference {
	case qcom.WDSIPPreferenceIPv4:
		want = qcom.WDSPDPTypeIPv4
	case qcom.WDSIPPreferenceIPv6:
		want = qcom.WDSPDPTypeIPv6
	default:
		return 0, fmt.Errorf("unsupported QMAP IP preference %d", preference)
	}
	var compatible uint8
	for _, profile := range profiles {
		if !profile.APNKnown || !strings.EqualFold(strings.TrimSpace(profile.APN), apn) || !profile.PDPKnown {
			continue
		}
		if profile.PDPType == want {
			return profile.ID.Index, nil
		}
		if profile.PDPType == qcom.WDSPDPTypeIPv4v6 && compatible == 0 {
			compatible = profile.ID.Index
		}
	}
	if compatible != 0 {
		return compatible, nil
	}
	return 0, fmt.Errorf("%w: APN %q with IP preference %d", qcom.ErrWDSProfileNotFound, apn, preference)
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

// RemoveQMAPMuxes removes mux netdevs after their WDS sessions have stopped.
func RemoveQMAPMuxes(modem *Modem, muxIDs ...uint8) error {
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
