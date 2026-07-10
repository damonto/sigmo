package device

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/damonto/uicc-go/qcom"
	"github.com/damonto/uicc-go/qcom/qmi"
	"github.com/damonto/uicc-go/qcom/uim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"
	"github.com/damonto/uicc-go/usim/simfile"
)

type qmiDevice struct {
	device    string
	slot      uint8
	imei      string
	openUIM   func(context.Context, uint8) (qmiUIMReader, error)
	openRadio func(context.Context) (qmiAirplaneModeReader, error)
}

func newQMIDevice(device string, slot uint8, imei string) qmiDevice {
	return qmiDevice{
		device: device,
		slot:   slot,
		imei:   imei,
		openUIM: func(ctx context.Context, slot uint8) (qmiUIMReader, error) {
			return openQMIUIM(ctx, device, slot)
		},
		openRadio: func(ctx context.Context) (qmiAirplaneModeReader, error) {
			return openQMIUIM(ctx, device, 1)
		},
	}
}

func (u qmiDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	return openQMIUSIMReader(ctx, u.device, u.slot)
}

func (u qmiDevice) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	reader, err := openQMIUIM(ctx, u.device, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	if err := configureQMICAT(ctx, u.imei, uim.NewCAT(reader), profile); err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	adapter, err := usim.NewQCOM(reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return adapter, nil
}

func openQMIUIM(ctx context.Context, device string, slot uint8) (*uim.Reader, error) {
	transport, err := qmi.Open(ctx, qmi.WithProxy(device))
	if err != nil {
		return nil, err
	}
	reader, err := uim.New(ctx, transport, uim.WithSlot(slot))
	if err != nil {
		return nil, errors.Join(err, transport.Close())
	}
	return reader, nil
}

func openQMIUSIMReader(ctx context.Context, device string, slot uint8) (usimcard.Reader, error) {
	if err := validateSIMSlot(slot); err != nil {
		return nil, err
	}
	reader, err := openQMIUIM(ctx, device, slot)
	if err != nil {
		return nil, err
	}
	if err := reader.ActivateSlot(ctx); err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	adapter, err := usim.NewQCOM(reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return adapter, nil
}

var (
	qmiPowerRestoreTimeout = 5 * time.Second
	qmiSIMPowerCycleDelay  = 100 * time.Millisecond
)

type qmiAirplaneModeReader interface {
	OperatingMode(ctx context.Context) (qcom.DMSOperatingMode, error)
	SetOperatingMode(ctx context.Context, mode qcom.DMSOperatingMode) error
	Close() error
}

type qmiUIMReader interface {
	ATR(ctx context.Context) ([]byte, error)
	IMSAStatus(ctx context.Context) (qcom.IMSAStatus, error)
	PowerOffSIM(ctx context.Context, slot uint8) error
	PowerOnSIM(ctx context.Context, req uim.PowerOnSIMRequest) error
	SlotStatus(ctx context.Context) (uim.SlotStatus, error)
	CardStatus(ctx context.Context) (uim.CardStatus, error)
	ChangeProvisioningSession(ctx context.Context, req uim.ChangeProvisioningSessionRequest) error
	Close() error
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

func (u qmiDevice) AirplaneMode(ctx context.Context) (bool, error) {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open QMI airplane mode reader: %w", err)
	}
	defer closeReader("close QMI airplane mode reader", reader)

	mode, err := reader.OperatingMode(ctx)
	if err != nil {
		return false, fmt.Errorf("read QMI operating mode: %w", err)
	}
	return qmiOperatingModeAirplane(mode), nil
}

func (u qmiDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return fmt.Errorf("open QMI airplane mode reader: %w", err)
	}
	defer closeReader("close QMI airplane mode reader", reader)

	return setQMIAirplaneMode(ctx, reader, enabled)
}

func setQMIAirplaneMode(ctx context.Context, reader qmiAirplaneModeReader, enabled bool) error {
	mode := qcom.DMSOperatingModeOnline
	if enabled {
		mode = qcom.DMSOperatingModeLowPower
	}
	if err := reader.SetOperatingMode(ctx, mode); err != nil {
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

func (u qmiDevice) ATR(ctx context.Context) ([]byte, error) {
	reader, err := u.openUIM(ctx, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	atr, err := reader.ATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("read QMI UIM ATR: %w", err)
	}
	return atr, nil
}

func (u qmiDevice) VoLTEStatus(ctx context.Context) (VoLTEStatus, error) {
	reader, err := u.openUIM(ctx, u.slot)
	if err != nil {
		return VoLTEStatus{}, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	status, err := reader.IMSAStatus(ctx)
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
	return VoLTEStatus{
		Supported: true,
		Known:     status.RegistrationKnown,
		CanEnable: status.RegistrationKnown && !status.VoLTERegistered(),
	}, nil
}

func (u qmiDevice) ActivateProvisioningIfSIMMissing(ctx context.Context) error {
	reader, err := u.openUIM(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	status, err := readQMICardStatus(ctx, reader)
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
	if err := changeQMIProvisioningSession(ctx, reader, u.slot, app.AID); err != nil {
		return fmt.Errorf("activate provisioning session: %w", err)
	}
	return nil
}

func (u qmiDevice) PowerCycleSIM(ctx context.Context) error {
	reader, err := u.openUIM(ctx, u.slot)
	if err != nil {
		return fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	if err := reader.PowerOffSIM(ctx, u.slot); err != nil {
		return fmt.Errorf("power off sim: %w", err)
	}
	slog.Info("sim powered off", "imei", u.imei, "slot", u.slot)
	// Once the SIM is off, cancellation must not leave it without power.
	time.Sleep(qmiSIMPowerCycleDelay)

	restoreCtx := context.WithoutCancel(ctx)
	if err := qmiPowerOnSIM(restoreCtx, reader, u.slot); err != nil {
		return fmt.Errorf("power on sim: %w", err)
	}
	slog.Info("sim powered on", "imei", u.imei, "slot", u.slot)
	return nil
}

func (u qmiDevice) SIMState(ctx context.Context, target Target) (SIMState, error) {
	slot, err := targetSIMSlot(u.slot, target)
	if err != nil {
		return SIMState{Supported: true}, err
	}
	target.ICCID = strings.TrimSpace(target.ICCID)

	reader, err := u.openUIM(ctx, slot)
	if err != nil {
		return SIMState{Supported: true, Slot: slot}, fmt.Errorf("open QMI UIM reader: %w", err)
	}
	defer closeQMIUIMReader(reader)

	state := SIMState{Supported: true, Slot: slot}
	var status slotStatus
	var slotStatusRead bool
	status, err = readQMISlotStatus(ctx, reader)
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

	cardStatus, err := readQMICardStatus(ctx, reader)
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

func qmiPowerOnSIM(ctx context.Context, reader qmiUIMReader, slot uint8) error {
	powerCtx, cancel := context.WithTimeout(ctx, qmiPowerRestoreTimeout)
	defer cancel()
	return reader.PowerOnSIM(powerCtx, uim.PowerOnSIMRequest{Slot: slot})
}

func readQMISlotStatus(ctx context.Context, reader qmiUIMReader) (slotStatus, error) {
	status, err := reader.SlotStatus(ctx)
	if err != nil {
		return slotStatus{}, fmt.Errorf("read QMI UIM slot status: %w", err)
	}
	return qmiSlotStatus(status)
}

func readQMICardStatus(ctx context.Context, reader qmiUIMReader) (cardStatus, error) {
	status, err := reader.CardStatus(ctx)
	if err != nil {
		return cardStatus{}, fmt.Errorf("read QMI UIM card status: %w", err)
	}
	return qmiCardStatus(status), nil
}

func changeQMIProvisioningSession(ctx context.Context, reader qmiUIMReader, slot uint8, aid []byte) error {
	if err := reader.ChangeProvisioningSession(ctx, uim.ChangeProvisioningSessionRequest{
		Session:  uim.SessionPrimaryGWProvisioning,
		Activate: true,
		Slot:     slot,
		AID:      slices.Clone(aid),
	}); err != nil {
		return fmt.Errorf("change provisioning session: %w", err)
	}
	return nil
}

func configureQMICAT(ctx context.Context, imei string, cat *uim.CAT, profile CATProfile) error {
	if len(profile.Data) == 0 && profile.EventMask == 0 && profile.FullFunctionMask == 0 {
		return nil
	}
	config, err := cat.Configuration(ctx)
	if err != nil {
		return fmt.Errorf("read QMI CAT configuration: %w", err)
	}
	profileChanged := !slices.Equal(config.CustomProfile, profile.Data)
	if config.Mode != uim.CATConfigCustomRaw || profileChanged {
		slog.Info(
			"set QMI CAT configuration",
			"imei", imei,
			"from", config.Mode,
			"to", uim.CATConfigCustomRaw,
			"profileChanged", profileChanged,
		)
		if err := cat.SetConfiguration(ctx, uim.CATConfiguration{
			Mode:          uim.CATConfigCustomRaw,
			CustomProfile: slices.Clone(profile.Data),
		}); err != nil {
			return fmt.Errorf("set QMI CAT CustomRaw mode: %w", err)
		}
	}

	claim, err := cat.ForceClaimEvents(ctx, uim.CATEventClaimConfig{
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

func closeQMIUIMReader(reader qmiUIMReader) {
	closeReader("close QMI UIM reader", reader)
}

func qmiSlotStatus(status uim.SlotStatus) (slotStatus, error) {
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

func qmiCardStatus(status uim.CardStatus) cardStatus {
	cards := make([]card, len(status.Cards))
	for i, card := range status.Cards {
		cards[i].Present = card.State == uim.CardStatePresent
		for _, app := range card.Applications {
			if app.Type != uim.ApplicationTypeUSIM {
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

func qmiUSIMReady(card uim.Card, app uim.CardApplication) bool {
	return card.State == uim.CardStatePresent &&
		app.Type == uim.ApplicationTypeUSIM &&
		app.State == uim.ApplicationStateReady &&
		app.PersonalizationState == uim.PersonalizationStateReady
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
