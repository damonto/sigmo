package automation

import (
	"context"
	"errors"
	"time"

	"github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/ims"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

type profileService interface {
	ProfileInventory(context.Context, *mmodem.Modem) (*esim.Inventory, error)
	SwitchForAutomation(context.Context, *mmodem.Modem, esim.AutomationSwitch, func(context.Context, *mmodem.Modem) error) error
}

type connections interface {
	Current(context.Context, *mmodem.Modem) (*internet.Connection, error)
	WithAutomationConnection(context.Context, *mmodem.Modem, mmodem.SIMIdentity, func(*internet.Connection) error) error
	RestoreAutomationConnection(context.Context, *mmodem.Modem, mmodem.SIMIdentity, internet.Preferences) error
	HasOngoingCall(string) bool
	AutomationAccessStatus(context.Context, *mmodem.Modem, ims.Access) (enabled, connected bool, err error)
	SendSMSVia(context.Context, *mmodem.Modem, ims.Access, string, string) (storage.Message, error)
}

type messages interface {
	SendWithProfileVia(context.Context, *mmodem.Modem, message.SendRequest, message.Sender, func(context.Context) error) (string, error)
}

type voiceCalls interface {
	RunTimedCall(context.Context, *mmodem.Modem, mmodem.SIMIdentity, call.DialRequest, time.Duration, func(context.Context) error) error
}

// Execution coordinates the existing eSIM, SMS, IMS and cellular data services.
type Execution struct {
	store    *storage.Store
	registry modemFinder
	esim     profileService
	internet connections
	messages messages
	calls    voiceCalls
	policy   feature.Policy
	download func(context.Context, *internet.Connection, int64) (int64, error)
}

// NewExecution wires production operations for the scheduler.
func NewExecution(store *storage.Store, registry *mmodem.Registry, profiles *esim.Service, connectivity *ims.Connectivity, calls *call.Calls, policy feature.Policy) *Execution {
	return &Execution{
		store: store, registry: registry, esim: profiles, internet: connectivity, calls: calls, policy: policy,
		messages: message.New(store, connectivity.MessageRoute()), download: downloadTraffic,
	}
}

func (e *Execution) bind(ctx context.Context, task storage.SIMAutomation) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	device, err := e.registry.Find(ctx, task.ModemID)
	if err != nil {
		return "", ErrLineUnavailable
	}
	if task.SEID != "" {
		card, err := e.card(ctx, device, task.SEID)
		if err != nil || card.EID == "" || profileOnCard(card, task.ProfileID) == nil {
			return "", ErrLineUnavailable
		}
		return card.EID, nil
	}
	if device.Snapshot().SIMKind() != mmodem.SIMKindPhysical || !matches(device.SIMIdentity(), task.ProfileID) {
		return "", ErrLineUnavailable
	}
	if err := e.store.RecordSIMProfile(ctx, storage.SIMProfile{ProfileID: task.ProfileID, ModemID: task.ModemID, Kind: "physical"}); err != nil {
		return "", err
	}
	return "", nil
}

func (e *Execution) card(ctx context.Context, device *mmodem.Modem, seID string) (esim.SecureElement, error) {
	inventory, err := e.esim.ProfileInventory(ctx, device)
	if err != nil {
		return esim.SecureElement{}, err
	}
	for _, card := range inventory.SEs {
		if card.ID == seID {
			return card, nil
		}
	}
	return esim.SecureElement{}, ErrLineUnavailable
}

func profileOnCard(card esim.SecureElement, iccid string) *esim.Profile {
	if iccid == "" {
		return nil
	}
	for i := range card.Profiles {
		if wwan.ICCIDMatches(card.Profiles[i].ICCID, iccid) {
			return &card.Profiles[i]
		}
	}
	return nil
}

func matches(active mmodem.SIMIdentity, iccid string) bool {
	return active.ICCID != "" && iccid != "" && wwan.ICCIDMatches(active.ICCID, iccid)
}

type restoration struct {
	Previous string
	Slot     uint32
	Internet *internet.Preferences
}

func (e *Execution) execute(ctx context.Context, task storage.SIMAutomation, action Action, journal func(context.Context, restoration) error) outcome {
	// Recheck the current license before any call-related SIM switch.
	if action.Type == actionCall && !e.canCall(action) {
		return failed(action.unavailableCode())
	}
	line, code := e.resolveLine(ctx, task, journal)
	if code != "" {
		return failed(code)
	}
	route, code := e.waitReady(ctx, line, action)
	if code != "" {
		return failed(code)
	}

	switch action.Type {
	case actionSMS:
		return e.sendSMS(ctx, line, action, route)
	case actionCall:
		return e.placeCall(ctx, task.ID, line, action, route)
	case actionData:
		return e.useData(ctx, line.device, line.identity, *action.Bytes)
	default:
		return failed("")
	}
}

type activeLine struct {
	device   *mmodem.Modem
	identity mmodem.SIMIdentity
}

func (e *Execution) resolveLine(ctx context.Context, task storage.SIMAutomation, journal func(context.Context, restoration) error) (activeLine, string) {
	line, err := e.store.SIMAutomationLine(ctx, task.ProfileID)
	if err != nil || line.ModemID != task.ModemID || line.SEID != task.SEID || line.DeletionState != "" {
		return activeLine{}, "line_unavailable"
	}
	device, err := e.findModem(ctx, task.ModemID)
	if err != nil {
		return activeLine{}, "line_unavailable"
	}
	snapshot := device.Snapshot()
	if snapshot.Locked() {
		return activeLine{}, "sim_locked"
	}
	if task.SEID != "" {
		return e.prepareESIM(ctx, task, device, snapshot.SIMIdentity, journal)
	}
	if snapshot.SIMKind() != mmodem.SIMKindPhysical || !matches(snapshot.SIMIdentity, task.ProfileID) {
		return activeLine{}, "line_unavailable"
	}
	return activeLine{device: device, identity: snapshot.SIMIdentity}, ""
}

func (e *Execution) sendSMS(ctx context.Context, line activeLine, action Action, route string) outcome {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req := message.SendRequest{
		ExpectedProfileID: line.identity.ICCID,
		ExpectedSIM:       line.identity,
		To:                *action.To,
		Text:              *action.Text,
	}
	var sender message.Sender
	if route != call.RouteModem {
		sender = func(ctx context.Context, device *mmodem.Modem, to, text string) (storage.Message, error) {
			return e.internet.SendSMSVia(ctx, device, ims.Access(route), to, text)
		}
	}
	_, err := e.messages.SendWithProfileVia(ctx, line.device, req, sender, func(ctx context.Context) error {
		return e.checkRoute(ctx, line.device, action, route)
	})
	switch {
	case err == nil:
		return outcome{Status: statusSucceeded}
	case errors.Is(err, message.ErrSendUnconfirmed):
		return outcome{Status: statusUnconfirmed, Code: "sms_unconfirmed"}
	case errors.Is(err, message.ErrProfileChanged):
		return failed("line_unavailable")
	case errors.Is(err, errRouteUnavailable), errors.Is(err, message.ErrRouteNotConnected), errors.Is(err, message.ErrRouteUnavailable):
		return failed(action.unavailableCode())
	default:
		return failed("sms_failed")
	}
}

func (e *Execution) placeCall(ctx context.Context, taskID string, line activeLine, action Action, route string) outcome {
	req := call.DialRequest{ExpectedProfileID: line.identity.ICCID, To: *action.To, Route: route, ClientID: "automation-" + taskID}
	duration := time.Duration(*action.DurationSeconds) * time.Second
	err := e.calls.RunTimedCall(ctx, line.device, line.identity, req, duration, func(ctx context.Context) error {
		if e.internet.HasOngoingCall(line.device.EquipmentIdentifier) {
			return call.ErrCallInUse
		}
		return e.checkRoute(ctx, line.device, action, route)
	})
	switch {
	case err == nil:
		return outcome{Status: statusSucceeded}
	case errors.Is(err, call.ErrAutomationHangupUnconfirmed):
		return outcome{Status: statusUnconfirmed, Code: "hangup_unconfirmed"}
	case errors.Is(err, call.ErrAutomationUnanswered):
		return failed("no_answer")
	case errors.Is(err, call.ErrAutomationEarlyEnd):
		return failed("call_ended_early")
	case errors.Is(err, call.ErrCallInUse):
		return failed("call_in_progress")
	case errors.Is(err, call.ErrCallProfileChanged):
		return failed("line_unavailable")
	case errors.Is(err, errRouteUnavailable), errors.Is(err, call.ErrWiFiCallingNotConnected), errors.Is(err, call.ErrVoLTENotConnected):
		return failed(action.unavailableCode())
	default:
		return failed("call_failed")
	}
}

func (e *Execution) prepareESIM(ctx context.Context, task storage.SIMAutomation, device *mmodem.Modem, active mmodem.SIMIdentity, journal func(context.Context, restoration) error) (activeLine, string) {
	inventoryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	card, err := e.card(inventoryCtx, device, task.SEID)
	cancel()
	target := profileOnCard(card, task.ProfileID)
	if err != nil || card.EID != task.EID || target == nil {
		return activeLine{}, "line_unavailable"
	}
	if matches(active, task.ProfileID) {
		if target.ProfileStateName != "enabled" {
			return activeLine{}, "line_unavailable"
		}
		return activeLine{device: device, identity: active}, ""
	}
	prior := profileOnCard(card, active.ICCID)
	if prior == nil || prior.ProfileStateName != "enabled" || target.ProfileStateName != "disabled" || active.Slot == 0 || device.Snapshot().AirplaneMode() {
		return activeLine{}, "switch_unavailable"
	}
	if e.internet.HasOngoingCall(task.ModemID) {
		return activeLine{}, "call_in_progress"
	}

	state := restoration{Previous: prior.ICCID, Slot: active.Slot}
	change := esim.AutomationSwitch{SEID: task.SEID, EID: task.EID, FromICCID: prior.ICCID, ToICCID: target.ICCID, Slot: active.Slot}
	err = e.esim.SwitchForAutomation(ctx, device, change, func(ctx context.Context, current *mmodem.Modem) error {
		if e.internet.HasOngoingCall(task.ModemID) {
			return call.ErrCallInUse
		}
		currentInternet, err := e.internet.Current(ctx, current)
		if err != nil {
			return err
		}
		if currentInternet.Status == internet.StatusConnected {
			state.Internet = &internet.Preferences{
				APN: currentInternet.APN, IPType: currentInternet.IPType, APNUsername: currentInternet.APNUsername,
				APNPassword: currentInternet.APNPassword, APNAuth: currentInternet.APNAuth, DefaultRoute: currentInternet.DefaultRoute,
				ProxyEnabled: currentInternet.ProxyEnabled, AlwaysOn: currentInternet.AlwaysOn,
			}
		}
		return journal(ctx, state)
	})
	if errors.Is(err, call.ErrCallInUse) {
		return activeLine{}, "call_in_progress"
	}
	if err != nil {
		return activeLine{}, "switch_failed"
	}
	device, err = e.findModem(ctx, task.ModemID)
	if err != nil {
		return activeLine{}, "switch_failed"
	}
	selected := device.SIMIdentity()
	if !matches(selected, task.ProfileID) || selected.Slot != active.Slot {
		return activeLine{}, "switch_failed"
	}
	return activeLine{device: device, identity: selected}, ""
}

func (e *Execution) useData(ctx context.Context, device *mmodem.Modem, identity mmodem.SIMIdentity, bytes int64) outcome {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var received int64
	err := e.internet.WithAutomationConnection(ctx, device, identity, func(connection *internet.Connection) error {
		var err error
		received, err = e.download(ctx, connection, bytes)
		return err
	})
	result := outcome{Status: statusSucceeded, BytesReceived: &received}
	if err != nil {
		result.Status = statusFailed
		switch {
		case errors.Is(err, internet.ErrAutomationProfileChanged):
			result.Code = "line_unavailable"
		case errors.Is(err, internet.ErrAutomationConnectionCleanup):
			result.Code = "data_cleanup_failed"
		default:
			result.Code = "data_failed"
		}
	}
	return result
}

func (e *Execution) restore(ctx context.Context, task storage.SIMAutomation, state restoration) (string, bool) {
	device, err := e.findModem(ctx, task.ModemID)
	if err != nil {
		return "restore_failed", true
	}
	active := device.SIMIdentity()
	if active.ICCID == "" || device.SIMReloadPending() {
		return "restore_failed", true
	}
	if active.Slot != state.Slot || (!matches(active, state.Previous) && !matches(active, task.ProfileID)) {
		return "restore_skipped", false
	}
	cardCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	card, err := e.card(cardCtx, device, task.SEID)
	cancel()
	if err != nil {
		return "restore_failed", !errors.Is(err, ErrLineUnavailable)
	}
	if card.EID != task.EID {
		return "restore_skipped", false
	}
	if profileOnCard(card, state.Previous) == nil {
		return "restore_failed", false
	}
	if matches(active, task.ProfileID) {
		change := esim.AutomationSwitch{SEID: task.SEID, EID: task.EID, FromICCID: task.ProfileID, ToICCID: state.Previous, Slot: state.Slot}
		err := e.esim.SwitchForAutomation(ctx, device, change, func(_ context.Context, _ *mmodem.Modem) error {
			if e.internet.HasOngoingCall(task.ModemID) {
				return call.ErrCallInUse
			}
			return nil
		})
		if err != nil {
			return "restore_failed", true
		}
		device, err = e.findModem(ctx, task.ModemID)
		if err != nil {
			return "restore_failed", true
		}
		active = device.SIMIdentity()
	}
	if !matches(active, state.Previous) || active.Slot != state.Slot {
		return "restore_skipped", false
	}
	if state.Internet != nil {
		if err := e.internet.RestoreAutomationConnection(ctx, device, active, *state.Internet); err != nil {
			return "restore_failed", true
		}
	}
	return "restored", false
}

func (e *Execution) findModem(ctx context.Context, modemID string) (*mmodem.Modem, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return e.registry.Find(ctx, modemID)
}
