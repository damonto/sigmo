package wwan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/pkg/modem/msisdn"
	"github.com/damonto/wwan-go/qcom"
	qmiproto "github.com/damonto/wwan-go/qcom/qmi"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
	"github.com/damonto/wwan-go/sim/simfile"
)

var qmiMSISDNFile = qcom.File{
	Session: qcom.SessionPrimaryGWProvisioning,
	Path:    []byte{0x3F, 0x00, 0x7F, 0xFF, 0x6F, 0x40},
}

var qmiICCIDFilePath = []byte{0x3F, 0x00, 0x2F, 0xE2}

const (
	qmiICCIDFileSize = 10
	qmiICCIDTimeout  = 2 * time.Second
)

type qmiDevice struct {
	slot           uint8
	imei           string
	retainClients  bool
	openClient     func(context.Context, uint8) (qmiClient, error)
	openUSIMClient func(context.Context, uint8) (*qcom.Client, error)
	mu             sync.Mutex
	clients        map[uint8]qmiClient
	closeOnce      sync.Once
	closed         bool
	closeErr       error
}

func newQMIDevice(device string, slot uint8, imei string) *qmiDevice {
	return &qmiDevice{
		slot: slot,
		imei: imei,
		openClient: func(ctx context.Context, slot uint8) (qmiClient, error) {
			return OpenQMIClient(ctx, QMIClientConfig{Device: device, Slot: slot})
		},
		openUSIMClient: func(ctx context.Context, slot uint8) (*qcom.Client, error) {
			return OpenQMIClient(ctx, QMIClientConfig{Device: device, Slot: slot})
		},
	}
}

func newQMISession(cfg Config) *qmiDevice {
	return newQMISessionWithQCOMOpener(cfg, nil)
}

func newQMISessionWithQCOMOpener(cfg Config, open func(context.Context, uint8) (*qcom.Client, error)) *qmiDevice {
	session := newQMIDevice(cfg.Device, cfg.Slot, cfg.IMEI)
	if open != nil {
		session.openClient = func(ctx context.Context, slot uint8) (qmiClient, error) {
			client, err := open(ctx, slot)
			if err != nil {
				return nil, err
			}
			return client, nil
		}
		session.openUSIMClient = open
	}
	session.retainClients = true
	return session
}

func (u *qmiDevice) Close() error {
	u.closeOnce.Do(func() {
		u.mu.Lock()
		u.closed = true
		clients := make([]qmiClient, 0, len(u.clients))
		for _, client := range u.clients {
			clients = append(clients, client)
		}
		u.clients = nil
		u.mu.Unlock()

		for _, client := range clients {
			u.closeErr = errors.Join(u.closeErr, client.Close())
		}
	})
	return u.closeErr
}

func (u *qmiDevice) acquireClient(ctx context.Context, slot uint8) (qmiClient, func(error), error) {
	if !u.retainClients {
		client, err := u.openClient(ctx, slot)
		return client, func(error) {
			if client != nil {
				closeQMIClient(client)
			}
		}, err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil, func(error) {}, errors.New("QMI device is closed")
	}
	if client := u.clients[slot]; client != nil {
		return client, func(err error) { u.evictTerminalClient(slot, client, err) }, nil
	}
	client, err := u.openClient(ctx, slot)
	if err != nil {
		return nil, func(error) {}, err
	}
	if u.clients == nil {
		u.clients = make(map[uint8]qmiClient)
	}
	u.clients[slot] = client
	return client, func(err error) { u.evictTerminalClient(slot, client, err) }, nil
}

func (u *qmiDevice) evictTerminalClient(slot uint8, client qmiClient, err error) {
	if _, ok := errors.AsType[*qmiproto.TransportError](err); !ok {
		return
	}

	u.mu.Lock()
	if u.clients[slot] != client {
		u.mu.Unlock()
		return
	}
	delete(u.clients, slot)
	u.mu.Unlock()
	closeQMIClient(client)
}

func (u *qmiDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	client, err := u.openUSIMClient(ctx, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM client: %w", err)
	}
	return openQMIUSIMReader(ctx, client)
}

func (u *qmiDevice) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	client, err := u.openUSIMClient(ctx, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM client: %w", err)
	}
	cat := qcom.NewCAT(client)
	configurationChanged, err := configureQMICAT(ctx, u.imei, cat, profile)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	adapter, err := usim.NewQCOM(client)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return newQMICATReader(qmiCATReaderConfig{
		Adapter:              adapter,
		CAT:                  cat,
		Power:                client,
		Slot:                 u.slot,
		IMEI:                 u.imei,
		ConfigurationChanged: configurationChanged,
	}), nil
}

func openQMIUSIMReader(ctx context.Context, client *qcom.Client) (usimcard.Reader, error) {
	if err := client.ActivateSlot(ctx); err != nil {
		return nil, errors.Join(err, client.Close())
	}
	adapter, err := usim.NewQCOM(client)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return adapter, nil
}

var (
	qmiPowerRestoreTimeout = 5 * time.Second
	qmiSIMPowerCycleDelay  = 2 * time.Second
)

type qmiSIMPowerControl interface {
	PowerOffSIM(ctx context.Context, slot uint8) error
	PowerOnSIM(ctx context.Context, req qcom.PowerOnSIMRequest) error
}

type qmiClient interface {
	MSISDN(ctx context.Context) (qcom.DMSGetMSISDNResponse, error)
	FileAttributes(ctx context.Context, file qcom.File) (qcom.FileAttributes, error)
	ReadTransparent(ctx context.Context, req qcom.TransparentRead) ([]byte, error)
	WriteRecord(ctx context.Context, req qcom.RecordWrite) error
	ATR(ctx context.Context) ([]byte, error)
	IMSAStatus(ctx context.Context) (qcom.IMSAStatus, error)
	NASServingSystem(ctx context.Context) (qcom.NASServingSystem, error)
	WDSProfiles(ctx context.Context, profileType qcom.WDSProfileType) ([]qcom.WDSProfile, error)
	WDSProfileSettings(ctx context.Context, id qcom.WDSProfileID) (qcom.WDSProfileSettings, error)
	IMSSTestMode(ctx context.Context) (bool, error)
	SetIMSSTestMode(ctx context.Context, enabled bool) error
	PowerOffSIM(ctx context.Context, slot uint8) error
	PowerOnSIM(ctx context.Context, req qcom.PowerOnSIMRequest) error
	CardStatus(ctx context.Context) (qcom.CardStatus, error)
	ChangeProvisioningSession(ctx context.Context, req qcom.ChangeProvisioningSessionRequest) error
	Close() error
}

func (u *qmiDevice) MSISDN(ctx context.Context) (number string, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return "", fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	result, err := client.MSISDN(ctx)
	if err != nil {
		return "", fmt.Errorf("read QMI MSISDN: %w", err)
	}
	return strings.TrimSpace(result.VoiceNumber), nil
}

func (u *qmiDevice) UpdateMSISDN(ctx context.Context, number string) (err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	attrs, err := client.FileAttributes(ctx, qmiMSISDNFile)
	if err != nil {
		return fmt.Errorf("read QMI MSISDN file attributes: %w", err)
	}
	data, err := msisdn.EncodeRecord("", number, int(attrs.RecordSize))
	if err != nil {
		return fmt.Errorf("encode MSISDN record: %w", err)
	}
	if err := client.WriteRecord(ctx, qcom.RecordWrite{File: qmiMSISDNFile, Record: 1, Data: data}); err != nil {
		return fmt.Errorf("write QMI MSISDN record: %w", err)
	}
	return nil
}

type cardStatus struct {
	Cards []card
}

type card struct {
	Present          bool
	USIMApplications []usimApplication
}

type usimApplication struct {
	Ready                bool
	AID                  []byte
	ApplicationState     string
	PersonalizationState string
}

func (u *qmiDevice) ATR(ctx context.Context) (atr []byte, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	atr, err = client.ATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("read QMI UIM ATR: %w", err)
	}
	return atr, nil
}

func (u *qmiDevice) VoLTEStatus(ctx context.Context) (result VoLTEStatus, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return VoLTEStatus{}, fmt.Errorf("open QMI client: %w", err)
	}
	defer func() { release(err) }()

	status, err := client.IMSAStatus(ctx)
	if err != nil {
		switch {
		case errors.Is(err, qcom.QMIErrorNetworkUnsupported),
			errors.Is(err, qcom.QMIErrorDeviceUnsupported),
			errors.Is(err, qcom.QMIErrorInvalidServiceType),
			errors.Is(err, qcom.QMIErrorInvalidQmiCommand),
			errors.Is(err, qcom.QMIErrorNotSupported):
			return VoLTEStatus{}, ErrUnsupported
		default:
			return VoLTEStatus{}, fmt.Errorf("read QMI IMSA status: %w", err)
		}
	}
	return VoLTEStatus{Occupied: status.IMSRegistered()}, nil
}

func (u *qmiDevice) PacketServiceStatus(ctx context.Context) (result PacketServiceStatus, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open QMI client: %w", err)
	}
	defer func() { release(err) }()

	serving, err := client.NASServingSystem(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read QMI NAS serving system: %w", err)
	}
	return PacketServiceStatus{
		Registered: serving.RegistrationState == qcom.NASRegistrationRegistered,
		PSAttached: serving.PSAttachState == qcom.NASAttachAttached,
		LTE:        slices.Contains(serving.RadioInterfaces, qcom.NASRadioInterfaceLTE),
	}, nil
}

func (u *qmiDevice) IMSProfile(ctx context.Context) (result IMSProfile, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return IMSProfile{}, fmt.Errorf("open QMI client: %w", err)
	}
	defer func() { release(err) }()

	profiles, err := client.WDSProfiles(ctx, qcom.WDSProfileType3GPP)
	if err != nil {
		return IMSProfile{}, fmt.Errorf("read QMI WDS profiles: %w", err)
	}
	for _, profile := range profiles {
		settings, err := client.WDSProfileSettings(ctx, profile.ID)
		if err != nil {
			return IMSProfile{}, fmt.Errorf("read QMI WDS profile %d: %w", profile.ID.Index, err)
		}
		if isIMSProfile(settings) {
			return IMSProfile{
				Index:   profile.ID.Index,
				PDNType: imsProfilePDNType(settings),
			}, nil
		}
	}
	return IMSProfile{}, errors.New("IMS WDS profile is unavailable")
}

func isIMSProfile(settings qcom.WDSProfileSettings) bool {
	// Some carrier-provisioned Qualcomm profiles omit optional IMS metadata. The
	// APN is the portable identifier exposed consistently by QMI and AT interfaces.
	return settings.APNKnown && strings.EqualFold(strings.TrimSpace(settings.APN), "ims")
}

func imsProfilePDNType(settings qcom.WDSProfileSettings) string {
	if !settings.PDPKnown {
		return ""
	}
	switch settings.PDPType {
	case qcom.WDSPDPTypeIPv4:
		return "ipv4"
	case qcom.WDSPDPTypeIPv6:
		return "ipv6"
	case qcom.WDSPDPTypeIPv4v6:
		return "ipv4v6"
	default:
		return ""
	}
}

func (u *qmiDevice) IMSSTestMode(ctx context.Context) (enabled bool, err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return false, fmt.Errorf("open QMI client: %w", err)
	}
	defer func() { release(err) }()

	enabled, err = client.IMSSTestMode(ctx)
	if errors.Is(err, qcom.QMIErrorInvalidServiceType) || errors.Is(err, qcom.QMIErrorNotSupported) {
		return false, ErrUnsupported
	}
	if err != nil {
		return false, fmt.Errorf("read QMI IMSS test mode: %w", err)
	}
	return enabled, nil
}

func (u *qmiDevice) SetIMSSTestMode(ctx context.Context, enabled bool) (err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI client: %w", err)
	}
	defer func() { release(err) }()

	if err := client.SetIMSSTestMode(ctx, enabled); errors.Is(err, qcom.QMIErrorInvalidServiceType) || errors.Is(err, qcom.QMIErrorNotSupported) {
		return ErrUnsupported
	} else if err != nil {
		return fmt.Errorf("set QMI IMSS test mode: %w", err)
	}
	return nil
}

func (u *qmiDevice) ActivateProvisioningIfSIMMissing(ctx context.Context) (err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	status, err := readQMICardStatus(ctx, client)
	if err != nil {
		return err
	}
	card, ok := deviceCardForSlot(status, u.slot)
	if !ok {
		return fmt.Errorf("qmi UIM card status missing slot %d", u.slot)
	}
	app, ok := deviceUSIMApplication(card)
	if !ok {
		return fmt.Errorf("qmi UIM USIM application missing in slot %d", u.slot)
	}
	if app.Ready {
		return nil
	}
	if len(app.AID) == 0 {
		return errors.New("qmi UIM USIM application AID is empty")
	}

	slog.Info(
		"sim missing, activate provisioning session",
		"imei", u.imei,
		"slot", u.slot,
		"applicationState", app.ApplicationState,
		"personalizationState", app.PersonalizationState,
	)
	if err := changeQMIProvisioningSession(ctx, client, u.slot, app.AID); err != nil {
		return fmt.Errorf("activate provisioning session: %w", err)
	}
	return nil
}

func (u *qmiDevice) PowerCycleSIM(ctx context.Context) (err error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	if err := client.PowerOffSIM(ctx, u.slot); err != nil {
		return fmt.Errorf("power off sim: %w", err)
	}
	slog.Info("sim powered off", "imei", u.imei, "slot", u.slot)
	// Once the SIM is off, cancellation must not leave it without power.
	time.Sleep(qmiSIMPowerCycleDelay)

	restoreCtx := context.WithoutCancel(ctx)
	if err := qmiPowerOnSIM(restoreCtx, client, qcom.PowerOnSIMRequest{Slot: u.slot}); err != nil {
		return fmt.Errorf("power on sim: %w", err)
	}
	slog.Info("sim powered on", "imei", u.imei, "slot", u.slot)
	return nil
}

func (u *qmiDevice) SIMState(ctx context.Context, target Target) (state SIMState, err error) {
	slot, err := targetSIMSlot(u.slot, target)
	if err != nil {
		return SIMState{Supported: true}, err
	}
	target.ICCID = strings.TrimSpace(target.ICCID)

	client, release, err := u.acquireClient(ctx, slot)
	if err != nil {
		return SIMState{Supported: true, Slot: slot}, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer func() { release(err) }()

	state = SIMState{Supported: true, Slot: slot}
	cardStatus, err := readQMICardStatus(ctx, client)
	if err != nil {
		return state, fmt.Errorf("read device card status: %w", err)
	}
	state.Ready = deviceUSIMReadyForSlot(cardStatus, slot)
	state.Recoverable = deviceUSIMPresentForSlot(cardStatus, slot)
	if !state.Recoverable || !state.Ready {
		return state, nil
	}

	state.ICCID, err = readQMIICCID(ctx, client, slot)
	if err != nil {
		return state, err
	}
	state.Matches = target.ICCID == "" || state.ICCID == target.ICCID
	state.ICCIDMismatch = target.ICCID != "" && state.ICCID != "" && state.ICCID != target.ICCID
	return state, nil
}

func qmiPowerOnSIM(ctx context.Context, client qmiSIMPowerControl, req qcom.PowerOnSIMRequest) error {
	powerCtx, cancel := context.WithTimeout(ctx, qmiPowerRestoreTimeout)
	defer cancel()
	return client.PowerOnSIM(powerCtx, req)
}

func readQMICardStatus(ctx context.Context, client qmiClient) (cardStatus, error) {
	status, err := client.CardStatus(ctx)
	if err != nil {
		return cardStatus{}, fmt.Errorf("read QMI UIM card status: %w", err)
	}
	return qmiCardStatus(status), nil
}

func readQMIICCID(ctx context.Context, client qmiClient, slot uint8) (string, error) {
	session, err := qmiCardSession(slot)
	if err != nil {
		return "", err
	}
	readCtx, cancel := context.WithTimeout(ctx, qmiICCIDTimeout)
	defer cancel()
	raw, err := client.ReadTransparent(readCtx, qcom.TransparentRead{
		File: qcom.File{
			Session: session,
			Path:    qmiICCIDFilePath,
		},
		Length: qmiICCIDFileSize,
	})
	if err != nil {
		return "", fmt.Errorf("read QMI UIM EF_ICCID: %w", err)
	}
	iccid, err := decodeQMIICCID(raw)
	if err != nil {
		return "", fmt.Errorf("decode QMI UIM EF_ICCID: %w", err)
	}
	return strings.TrimSpace(iccid), nil
}

func qmiCardSession(slot uint8) (qcom.Session, error) {
	switch slot {
	case 1:
		return qcom.SessionCardSlot1, nil
	case 2:
		return qcom.SessionCardSlot2, nil
	case 3:
		return qcom.SessionCardSlot3, nil
	case 4:
		return qcom.SessionCardSlot4, nil
	case 5:
		return qcom.SessionCardSlot5, nil
	default:
		return 0, fmt.Errorf("map QMI UIM card session: slot %d is out of range", slot)
	}
}

func changeQMIProvisioningSession(ctx context.Context, client qmiClient, slot uint8, aid []byte) error {
	if err := client.ChangeProvisioningSession(ctx, qcom.ChangeProvisioningSessionRequest{
		Session:  qcom.SessionPrimaryGWProvisioning,
		Activate: true,
		Slot:     slot,
		AID:      slices.Clone(aid),
	}); err != nil {
		return fmt.Errorf("change provisioning session: %w", err)
	}
	return nil
}

func configureQMICAT(ctx context.Context, imei string, cat *qcom.CAT, profile CATProfile) (bool, error) {
	if len(profile.Data) == 0 && profile.EventMask == 0 && profile.FullFunctionMask == 0 {
		return false, nil
	}
	config, err := cat.Configuration(ctx)
	if err != nil {
		return false, fmt.Errorf("read QMI CAT configuration: %w", err)
	}
	profileChanged := !equalCATProfile(config.CustomProfile, profile.Data)
	configurationChanged := config.Mode != qcom.CATConfigCustomRaw || profileChanged
	if configurationChanged {
		slog.Info(
			"set QMI CAT configuration",
			"imei", imei,
			"from", config.Mode,
			"to", qcom.CATConfigCustomRaw,
			"profileChanged", profileChanged,
		)
		if err := cat.SetConfiguration(ctx, qcom.CATConfiguration{
			Mode:          qcom.CATConfigCustomRaw,
			CustomProfile: slices.Clone(profile.Data),
		}); err != nil {
			return false, fmt.Errorf("set QMI CAT CustomRaw mode: %w", err)
		}
	}
	return configurationChanged, nil
}

func equalCATProfile(current, wanted []byte) bool {
	// Qualcomm reports its fixed-size profile buffer, including zero padding.
	return slices.Equal(
		bytes.TrimRight(current, "\x00"),
		bytes.TrimRight(wanted, "\x00"),
	)
}

func closeQMIClient(client qmiClient) {
	closeClient(client, "QMI UIM")
}

func decodeQMIICCID(raw []byte) (string, error) {
	var iccid simfile.ICCID
	if err := iccid.UnmarshalBinary(raw); err != nil {
		return "", err
	}
	return iccid.String(), nil
}

func qmiCardStatus(status qcom.CardStatus) cardStatus {
	cards := make([]card, len(status.Cards))
	for i, card := range status.Cards {
		cards[i].Present = card.State == qcom.CardStatePresent
		for _, app := range card.Applications {
			if app.Type != qcom.ApplicationTypeUSIM {
				continue
			}
			cards[i].USIMApplications = append(cards[i].USIMApplications, usimApplication{
				Ready:                qmiUSIMReady(card, app),
				AID:                  slices.Clone(app.AID),
				ApplicationState:     fmt.Sprint(app.State),
				PersonalizationState: fmt.Sprint(app.PersonalizationState),
			})
		}
	}
	return cardStatus{Cards: cards}
}

func deviceCardForSlot(status cardStatus, slot uint8) (card, bool) {
	index := int(slot) - 1
	if index < 0 || index >= len(status.Cards) {
		return card{}, false
	}
	return status.Cards[index], true
}

func deviceUSIMApplication(card card) (usimApplication, bool) {
	if len(card.USIMApplications) == 0 {
		return usimApplication{}, false
	}
	return card.USIMApplications[0], true
}

func qmiUSIMReady(card qcom.Card, app qcom.CardApplication) bool {
	return card.State == qcom.CardStatePresent &&
		app.Type == qcom.ApplicationTypeUSIM &&
		app.State == qcom.ApplicationStateReady &&
		app.PersonalizationState == qcom.PersonalizationStateReady
}

func deviceUSIMPresentForSlot(status cardStatus, slot uint8) bool {
	card, ok := deviceCardForSlot(status, slot)
	if !ok || !card.Present {
		return false
	}
	_, ok = deviceUSIMApplication(card)
	return ok
}

func deviceUSIMReadyForSlot(status cardStatus, slot uint8) bool {
	card, ok := deviceCardForSlot(status, slot)
	if !ok {
		return false
	}
	app, ok := deviceUSIMApplication(card)
	return ok && app.Ready
}
