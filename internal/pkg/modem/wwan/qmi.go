package wwan

import (
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
	"github.com/damonto/wwan-go/qcom/qmi"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
	"github.com/damonto/wwan-go/sim/simfile"
)

var qmiMSISDNFile = qcom.File{
	Session: qcom.SessionPrimaryGWProvisioning,
	Path:    []byte{0x3F, 0x00, 0x7F, 0xFF, 0x6F, 0x40},
}

type qmiDevice struct {
	device       string
	slot         uint8
	imei         string
	reuseClients bool
	openClient   func(context.Context, uint8) (qmiClient, error)
	openRadio    func(context.Context) (qmiRadio, error)
	mu           sync.Mutex
	clients      map[uint8]qmiClient
	closeOnce    sync.Once
	closed       bool
	closeErr     error
}

func newQMIDevice(device string, slot uint8, imei string, reuseClients bool) *qmiDevice {
	return &qmiDevice{
		device:       device,
		slot:         slot,
		imei:         imei,
		reuseClients: reuseClients,
		openClient: func(ctx context.Context, slot uint8) (qmiClient, error) {
			return openQMIClient(ctx, device, slot)
		},
		openRadio: func(ctx context.Context) (qmiRadio, error) {
			return openQMIClient(ctx, device, 1)
		},
	}
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

func (u *qmiDevice) acquireClient(ctx context.Context, slot uint8) (qmiClient, func(), error) {
	if !u.reuseClients {
		client, err := u.openClient(ctx, slot)
		return client, func() {
			if client != nil {
				closeQMIClient(client)
			}
		}, err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil, func() {}, errors.New("QMI device is closed")
	}
	if client := u.clients[slot]; client != nil {
		return client, func() {}, nil
	}
	client, err := u.openClient(ctx, slot)
	if err != nil {
		return nil, func() {}, err
	}
	if u.clients == nil {
		u.clients = make(map[uint8]qmiClient)
	}
	u.clients[slot] = client
	return client, func() {}, nil
}

func (u *qmiDevice) acquireRadio(ctx context.Context) (qmiRadio, func(), error) {
	if !u.reuseClients {
		client, err := u.openRadio(ctx)
		return client, func() {
			if client != nil {
				closeClient("close QMI radio client", client)
			}
		}, err
	}

	client, _, err := u.acquireClient(ctx, 1)
	if err != nil {
		return nil, func() {}, err
	}
	radio, ok := client.(qmiRadio)
	if !ok {
		return nil, func() {}, errors.New("QMI client does not support radio control")
	}
	return radio, func() {}, nil
}

func (u *qmiDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	return openQMIUSIM(ctx, u.device, u.slot)
}

func (u *qmiDevice) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	client, err := openQMIClient(ctx, u.device, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM client: %w", err)
	}
	if err := configureQMICAT(ctx, u.imei, qcom.NewCAT(client), profile); err != nil {
		return nil, errors.Join(err, client.Close())
	}
	adapter, err := usim.NewQCOM(client)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return adapter, nil
}

func openQMIClient(ctx context.Context, device string, slot uint8) (*qcom.Client, error) {
	transport, err := qmi.Open(ctx, qmi.WithProxy(device))
	if err != nil {
		return nil, err
	}
	client, err := qcom.NewClient(transport, qcom.WithSlot(slot))
	if err != nil {
		return nil, errors.Join(err, transport.Close())
	}
	return client, nil
}

func openQMIUSIM(ctx context.Context, device string, slot uint8) (usimcard.Reader, error) {
	if err := validateSIMSlot(slot); err != nil {
		return nil, err
	}
	client, err := openQMIClient(ctx, device, slot)
	if err != nil {
		return nil, err
	}
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
	qmiSIMPowerCycleDelay  = 100 * time.Millisecond
)

type qmiRadio interface {
	OperatingMode(ctx context.Context) (qcom.DMSOperatingMode, error)
	SetOperatingMode(ctx context.Context, mode qcom.DMSOperatingMode) error
	Close() error
}

type qmiClient interface {
	MSISDN(ctx context.Context) (qcom.DMSGetMSISDNResponse, error)
	FileAttributes(ctx context.Context, file qcom.File) (qcom.FileAttributes, error)
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
	SlotStatus(ctx context.Context) (qcom.SlotStatus, error)
	CardStatus(ctx context.Context) (qcom.CardStatus, error)
	ChangeProvisioningSession(ctx context.Context, req qcom.ChangeProvisioningSessionRequest) error
	Close() error
}

func (u *qmiDevice) MSISDN(ctx context.Context) (string, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return "", fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	result, err := client.MSISDN(ctx)
	if err != nil {
		return "", fmt.Errorf("read QMI MSISDN: %w", err)
	}
	return strings.TrimSpace(result.VoiceNumber), nil
}

func (u *qmiDevice) UpdateMSISDN(ctx context.Context, number string) error {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

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

type slotStatus struct {
	ActiveSlot uint8
	Slots      []slot
}

type slot struct {
	ICCID string
	ATR   []byte
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

func (u *qmiDevice) AirplaneMode(ctx context.Context) (bool, error) {
	client, release, err := u.acquireRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open QMI radio client: %w", err)
	}
	defer release()

	mode, err := client.OperatingMode(ctx)
	if err != nil {
		return false, fmt.Errorf("read QMI operating mode: %w", err)
	}
	return qmiOperatingModeAirplane(mode), nil
}

func (u *qmiDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	client, release, err := u.acquireRadio(ctx)
	if err != nil {
		return fmt.Errorf("open QMI radio client: %w", err)
	}
	defer release()

	return setQMIAirplaneMode(ctx, client, enabled)
}

func setQMIAirplaneMode(ctx context.Context, client qmiRadio, enabled bool) error {
	mode := qcom.DMSOperatingModeOnline
	if enabled {
		mode = qcom.DMSOperatingModeLowPower
	}
	if err := client.SetOperatingMode(ctx, mode); err != nil {
		return fmt.Errorf("set QMI operating mode: %w", err)
	}
	return nil
}

func qmiOperatingModeAirplane(mode qcom.DMSOperatingMode) bool {
	switch mode {
	case qcom.DMSOperatingModeLowPower,
		qcom.DMSOperatingModeOffline,
		qcom.DMSOperatingModePersistentLowPower,
		qcom.DMSOperatingModeModeOnlyLowPower:
		return true
	default:
		return false
	}
}

func (u *qmiDevice) ATR(ctx context.Context) ([]byte, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	atr, err := client.ATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("read QMI UIM ATR: %w", err)
	}
	return atr, nil
}

func (u *qmiDevice) VoLTEStatus(ctx context.Context) (VoLTEStatus, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return VoLTEStatus{}, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	status, err := client.IMSAStatus(ctx)
	if err != nil {
		switch {
		case errors.Is(err, qcom.QMIErrorNetworkUnsupported),
			errors.Is(err, qcom.QMIErrorDeviceUnsupported),
			errors.Is(err, qcom.QMIErrorInvalidServiceType),
			errors.Is(err, qcom.QMIErrorInvalidQmiCommand),
			errors.Is(err, qcom.QMIErrorNotSupported):
			return VoLTEStatus{}, nil
		default:
			return VoLTEStatus{}, fmt.Errorf("read QMI IMSA status: %w", err)
		}
	}
	return VoLTEStatus{Occupied: status.IMSRegistered()}, nil
}

func (u *qmiDevice) PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

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

func (u *qmiDevice) IMSProfileIndex(ctx context.Context) (uint8, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return 0, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	profiles, err := client.WDSProfiles(ctx, qcom.WDSProfileType3GPP)
	if err != nil {
		return 0, fmt.Errorf("read QMI WDS profiles: %w", err)
	}
	for _, profile := range profiles {
		settings, err := client.WDSProfileSettings(ctx, profile.ID)
		if err != nil {
			return 0, fmt.Errorf("read QMI WDS profile %d: %w", profile.ID.Index, err)
		}
		if isIMSProfile(settings) {
			return profile.ID.Index, nil
		}
	}
	return 0, errors.New("IMS WDS profile is unavailable")
}

func isIMSProfile(settings qcom.WDSProfileSettings) bool {
	// Some carrier-provisioned Qualcomm profiles omit optional IMS metadata. The
	// APN is the portable identifier exposed consistently by QMI and AT interfaces.
	return settings.APNKnown && strings.EqualFold(strings.TrimSpace(settings.APN), "ims")
}

func (u *qmiDevice) IMSSTestMode(ctx context.Context) (bool, error) {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return false, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	enabled, err := client.IMSSTestMode(ctx)
	if err != nil {
		return false, fmt.Errorf("read QMI IMSS test mode: %w", err)
	}
	return enabled, nil
}

func (u *qmiDevice) SetIMSSTestMode(ctx context.Context, enabled bool) error {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	if err := client.SetIMSSTestMode(ctx, enabled); err != nil {
		return fmt.Errorf("set QMI IMSS test mode: %w", err)
	}
	return nil
}

func (u *qmiDevice) ActivateProvisioningIfSIMMissing(ctx context.Context) error {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

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

func (u *qmiDevice) PowerCycleSIM(ctx context.Context) error {
	client, release, err := u.acquireClient(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	if err := client.PowerOffSIM(ctx, u.slot); err != nil {
		return fmt.Errorf("power off sim: %w", err)
	}
	slog.Info("sim powered off", "imei", u.imei, "slot", u.slot)
	// Once the SIM is off, cancellation must not leave it without power.
	time.Sleep(qmiSIMPowerCycleDelay)

	restoreCtx := context.WithoutCancel(ctx)
	if err := qmiPowerOnSIM(restoreCtx, client, u.slot); err != nil {
		return fmt.Errorf("power on sim: %w", err)
	}
	slog.Info("sim powered on", "imei", u.imei, "slot", u.slot)
	return nil
}

func (u *qmiDevice) SIMState(ctx context.Context, target Target) (SIMState, error) {
	slot, err := targetSIMSlot(u.slot, target)
	if err != nil {
		return SIMState{Supported: true}, err
	}
	target.ICCID = strings.TrimSpace(target.ICCID)

	client, release, err := u.acquireClient(ctx, slot)
	if err != nil {
		return SIMState{Supported: true, Slot: slot}, fmt.Errorf("open QMI UIM client: %w", err)
	}
	defer release()

	state := SIMState{Supported: true, Slot: slot}
	var status slotStatus
	var slotStatusRead bool
	status, err = readQMISlotStatus(ctx, client)
	if err != nil && !errors.Is(err, qcom.QMIErrorNotSupported) {
		return state, fmt.Errorf("read device slot status: %w", err)
	}
	if err == nil {
		slotStatusRead = true
		iccid := deviceICCIDForSlot(status, slot)
		state.ICCID = iccid
		state.Matches = deviceSlotMatchesTarget(status, slot, state.ICCID, target)
		state.ICCIDMismatch = target.ICCID != "" && state.ICCID != "" && state.ICCID != target.ICCID
	}

	cardStatus, err := readQMICardStatus(ctx, client)
	if err != nil {
		return state, fmt.Errorf("read device card status: %w", err)
	}
	state.Ready = deviceUSIMReadyForSlot(cardStatus, slot)
	state.Recoverable = state.Matches
	if !state.Recoverable && deviceUSIMPresentForSlot(cardStatus, slot) {
		slotContradicted := target.Slot == 0 && slotStatusRead && status.ActiveSlot != 0 && status.ActiveSlot != slot
		state.Recoverable = !slotContradicted
	}
	return state, nil
}

func qmiPowerOnSIM(ctx context.Context, client qmiClient, slot uint8) error {
	powerCtx, cancel := context.WithTimeout(ctx, qmiPowerRestoreTimeout)
	defer cancel()
	return client.PowerOnSIM(powerCtx, qcom.PowerOnSIMRequest{Slot: slot})
}

func readQMISlotStatus(ctx context.Context, client qmiClient) (slotStatus, error) {
	status, err := client.SlotStatus(ctx)
	if err != nil {
		return slotStatus{}, fmt.Errorf("read QMI UIM slot status: %w", err)
	}
	return qmiSlotStatus(status)
}

func readQMICardStatus(ctx context.Context, client qmiClient) (cardStatus, error) {
	status, err := client.CardStatus(ctx)
	if err != nil {
		return cardStatus{}, fmt.Errorf("read QMI UIM card status: %w", err)
	}
	return qmiCardStatus(status), nil
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

func configureQMICAT(ctx context.Context, imei string, cat *qcom.CAT, profile CATProfile) error {
	if len(profile.Data) == 0 && profile.EventMask == 0 && profile.FullFunctionMask == 0 {
		return nil
	}
	config, err := cat.Configuration(ctx)
	if err != nil {
		return fmt.Errorf("read QMI CAT configuration: %w", err)
	}
	profileChanged := !slices.Equal(config.CustomProfile, profile.Data)
	if config.Mode != qcom.CATConfigCustomRaw || profileChanged {
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
			return fmt.Errorf("set QMI CAT CustomRaw mode: %w", err)
		}
	}

	claim, err := cat.ForceClaimEvents(ctx, qcom.CATEventClaimConfig{
		RawMask:          profile.EventMask,
		FullFunctionMask: profile.FullFunctionMask,
	})
	if err != nil {
		return fmt.Errorf("claim QMI CAT events: %w", err)
	}
	if claim.ReleasedClientID != 0 {
		slog.Info(
			"claimed QMI CAT events",
			"imei", imei,
			"clientID", claim.ClientID,
			"releasedClientID", claim.ReleasedClientID,
		)
	}
	return nil
}

func closeQMIClient(client qmiClient) {
	closeClient("close QMI UIM client", client)
}

func qmiSlotStatus(status qcom.SlotStatus) (slotStatus, error) {
	slots := make([]slot, len(status.Slots))
	for i, slot := range status.Slots {
		if len(slot.ICCID) > 0 {
			iccid, err := decodeQMIICCID(slot.ICCID)
			if err != nil {
				return slotStatus{}, fmt.Errorf("decode device slot %d ICCID: %w", i+1, err)
			}
			slots[i].ICCID = iccid
		}
		slots[i].ATR = slices.Clone(slot.ATR)
	}
	return slotStatus{ActiveSlot: status.ActiveSlot, Slots: slots}, nil
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

func deviceSlotMatchesTarget(status slotStatus, slot uint8, iccid string, target Target) bool {
	if target.Slot != 0 && status.ActiveSlot != slot {
		return false
	}
	if target.ICCID != "" && iccid != target.ICCID {
		return false
	}
	return true
}

func deviceICCIDForSlot(status slotStatus, slot uint8) string {
	if slot == 0 || int(slot) > len(status.Slots) {
		return ""
	}
	return strings.TrimSpace(status.Slots[slot-1].ICCID)
}
