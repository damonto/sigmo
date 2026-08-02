package modem

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type catalog struct {
	store              *settings.Store
	registry           *mmodem.Registry
	internet           internetStatusReader
	overviewExtensions []modemstatus.Extension
	reminders          *reminder.Scheduler
}

type internetStatusReader interface {
	Current(context.Context, *mmodem.Modem) (*internet.Connection, error)
}

func newCatalog(store *settings.Store, registry *mmodem.Registry, overviewExtensions ...modemstatus.Extension) *catalog {
	return &catalog{
		store:              store,
		registry:           registry,
		overviewExtensions: slices.Clone(overviewExtensions),
	}
}

func (c *catalog) List(ctx context.Context) ([]*ModemResponse, error) {
	modems, err := c.registry.Modems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list modems: %w", err)
	}
	return c.buildListResponse(ctx, slices.Collect(maps.Values(modems)))
}

func (c *catalog) buildListResponse(ctx context.Context, devices []*mmodem.Modem) ([]*ModemResponse, error) {
	response := make([]*ModemResponse, len(devices))
	var wg sync.WaitGroup
	for i, device := range devices {
		wg.Go(func() {
			modemResp, err := c.buildResponse(ctx, device)
			if err != nil {
				slog.Warn("build degraded modem overview", "imei", device.EquipmentIdentifier, "path", device.Path(), "error", err)
				modemResp = c.buildBasicResponse(device)
			}
			response[i] = modemResp
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(response, func(a, b *ModemResponse) int {
		return strings.Compare(a.ID, b.ID)
	})
	return response, nil
}

// buildBasicResponse keeps a physically present modem visible when an
// optional overview extension cannot be rendered. Device presence is owned
// by the Registry, while volatile modem data comes from its runtime snapshot.
func (c *catalog) buildBasicResponse(device *mmodem.Modem) *ModemResponse {
	snapshot := device.Snapshot()
	currentSettings := c.store.Snapshot()
	alias := currentSettings.FindModem(device.EquipmentIdentifier).Alias
	name := device.Model
	if alias != "" {
		name = alias
	}
	resp := &ModemResponse{
		Manufacturer:     device.Manufacturer,
		ID:               device.EquipmentIdentifier,
		PrimaryPort:      device.PrimaryPort,
		FirmwareRevision: device.FirmwareRevision,
		HardwareRevision: device.HardwareRevision,
		Name:             name,
		Number:           snapshot.Number,
		State:            modemStateValue(snapshot),
		UnlockRequired:   unlockRequired(snapshot),
		UnlockSupported:  unlockSupported(device),
		SIM:              slotResponse(snapshot.SIM),
		Slots:            slotResponses(snapshot.Slots),
		AirplaneMode:     snapshot.AirplaneMode(),
		SignalQuality:    uint32(snapshot.Status.SignalQuality),
	}
	if snapshot.StatusKnown && !snapshot.AirplaneMode() {
		resp.AccessTechnology = accessTechnologyString(snapshot.Status.Technology)
		resp.RegistrationState = registrationStateName(snapshot.Status.Registration)
		resp.RegisteredOperator = RegisteredOperatorResponse{Name: snapshot.Status.OperatorName, Code: snapshot.Status.OperatorID}
	}
	supportsEsim, err := mmodem.SupportsEUICC(device)
	if err != nil {
		slog.Warn("read cached eSIM support", "imei", device.EquipmentIdentifier, "error", err)
		return resp
	}
	resp.SupportsEsim = supportsEsim
	return resp
}

func (c *catalog) Get(ctx context.Context, modem *mmodem.Modem) (*ModemResponse, error) {
	resp, err := c.buildResponse(ctx, modem)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *catalog) buildResponse(ctx context.Context, device *mmodem.Modem) (*ModemResponse, error) {
	snapshot := device.Snapshot()
	if snapshot.Locked() {
		return c.buildLockedResponse(ctx, device)
	}

	currentSettings := c.store.Snapshot()
	supportsEsim, err := mmodem.SupportsEUICC(device)
	if err != nil {
		return nil, fmt.Errorf("detect eSIM support: %w", err)
	}

	simSlots, err := c.buildSlotsResponse(ctx, snapshot)
	if err != nil {
		return nil, fmt.Errorf("fetch SIM slots: %w", err)
	}

	alias := currentSettings.FindModem(device.EquipmentIdentifier).Alias
	name := device.Model
	if alias != "" {
		name = alias
	}
	sim := slotResponse(snapshot.SIM)
	registrationState := ""
	if snapshot.StatusKnown {
		registrationState = registrationStateName(snapshot.Status.Registration)
	}
	resp := &ModemResponse{
		Manufacturer:      device.Manufacturer,
		ID:                device.EquipmentIdentifier,
		PrimaryPort:       device.PrimaryPort,
		FirmwareRevision:  device.FirmwareRevision,
		HardwareRevision:  device.HardwareRevision,
		Name:              name,
		Number:            snapshot.Number,
		State:             modemStateValue(snapshot),
		UnlockRequired:    unlockRequired(snapshot),
		UnlockSupported:   unlockSupported(device),
		SIM:               sim,
		Slots:             simSlots,
		AccessTechnology:  accessTechnologyString(snapshot.Status.Technology),
		RegistrationState: registrationState,
		RegisteredOperator: RegisteredOperatorResponse{
			Name: snapshot.Status.OperatorName,
			Code: snapshot.Status.OperatorID,
		},
		SignalQuality: uint32(snapshot.Status.SignalQuality),
		AirplaneMode:  snapshot.AirplaneMode(),
		SupportsEsim:  supportsEsim,
	}
	if snapshot.AirplaneMode() || !snapshot.StatusKnown {
		resp.AccessTechnology = ""
		if snapshot.AirplaneMode() {
			resp.RegistrationState = ""
		}
		resp.RegisteredOperator = RegisteredOperatorResponse{}
		resp.SignalQuality = 0
	}
	c.applyInternetStatus(ctx, device, resp)
	if resp.SIM.Reminder, err = c.reminderDetails(ctx, resp.SIM.Identifier); err != nil {
		return nil, fmt.Errorf("read primary SIM reminder: %w", err)
	}
	if err := c.applyOverviewExtensions(ctx, device, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *catalog) applyInternetStatus(ctx context.Context, device *mmodem.Modem, resp *ModemResponse) {
	if c.internet == nil {
		return
	}
	connection, err := c.internet.Current(ctx, device)
	if err != nil {
		slog.Warn("read modem Internet status", "imei", device.EquipmentIdentifier, "error", err)
		return
	}
	resp.InternetConnected = connection != nil && connection.Status == internet.StatusConnected
}

func (c *catalog) buildLockedResponse(ctx context.Context, device *mmodem.Modem) (*ModemResponse, error) {
	snapshot := device.Snapshot()
	currentSettings := c.store.Snapshot()
	alias := currentSettings.FindModem(device.EquipmentIdentifier).Alias
	name := device.Model
	if alias != "" {
		name = alias
	}
	supportsEsim, err := mmodem.SupportsEUICC(device)
	if err != nil {
		slog.Warn("detect eSIM support for locked modem", "imei", device.EquipmentIdentifier, "error", err)
	}
	return &ModemResponse{
		Manufacturer:     device.Manufacturer,
		ID:               device.EquipmentIdentifier,
		PrimaryPort:      device.PrimaryPort,
		FirmwareRevision: device.FirmwareRevision,
		HardwareRevision: device.HardwareRevision,
		Name:             name,
		Number:           snapshot.Number,
		State:            modemStateValue(snapshot),
		UnlockRequired:   unlockRequired(snapshot),
		UnlockSupported:  unlockSupported(device),
		AirplaneMode:     snapshot.AirplaneMode(),
		SupportsEsim:     supportsEsim,
		Slots:            []SlotResponse{},
	}, nil
}

func modemStateValue(snapshot mmodem.ModemSnapshot) string {
	status := snapshot.Status
	switch {
	case status.SIM == wwanmodem.SIMStateFailure:
		return "failed"
	case status.SIM == wwanmodem.SIMStateLocked:
		return "locked"
	case status.Power == wwanmodem.PowerStateOff || status.Power == wwanmodem.PowerStateLow:
		return "disabled"
	case status.OwnBearers > 0:
		return "connected"
	case status.Registration == wwanmodem.RegistrationSearching:
		return "searching"
	case status.Registration == wwanmodem.RegistrationHome || status.Registration == wwanmodem.RegistrationRoaming:
		return "registered"
	case status.Power == wwanmodem.PowerStateOn:
		return "enabled"
	default:
		return "unknown"
	}
}

func unlockRequired(snapshot mmodem.ModemSnapshot) string {
	if snapshot.Locked() {
		return "sim-pin"
	}
	return "none"
}

func unlockSupported(device *mmodem.Modem) bool {
	snapshot := device.Snapshot()
	return snapshot.Locked()
}

func (c *catalog) buildSlotsResponse(ctx context.Context, snapshot mmodem.ModemSnapshot) ([]SlotResponse, error) {
	slots := snapshot.Slots
	if len(slots) == 0 {
		return []SlotResponse{}, nil
	}
	simSlots := make([]SlotResponse, 0, len(slots))
	for _, sim := range slots {
		simSlots = append(simSlots, slotResponse(sim))
		current := &simSlots[len(simSlots)-1]
		reminderDetails, err := c.reminderDetails(ctx, sim.Identifier)
		if err != nil {
			return nil, fmt.Errorf("read SIM reminder for slot %d: %w", sim.Slot, err)
		}
		current.Reminder = reminderDetails
	}
	return simSlots, nil
}

func slotResponses(sims []*mmodem.SIM) []SlotResponse {
	if len(sims) == 0 {
		return []SlotResponse{}
	}
	result := make([]SlotResponse, 0, len(sims))
	for _, sim := range sims {
		result = append(result, slotResponse(sim))
	}
	return result
}

func slotResponse(sim *mmodem.SIM) SlotResponse {
	if sim == nil {
		return SlotResponse{}
	}
	carrierInfo := carrier.Lookup(sim.OperatorIdentifier)
	operatorName := carrierInfo.Name
	if sim.OperatorName != "" {
		operatorName = sim.OperatorName
	}
	return SlotResponse{
		Active:             sim.Active,
		OperatorName:       operatorName,
		OperatorIdentifier: sim.OperatorIdentifier,
		RegionCode:         carrierInfo.Region,
		Identifier:         sim.Identifier,
	}
}

func (c *catalog) reminderDetails(ctx context.Context, profileID string) (*reminder.Details, error) {
	if c.reminders == nil || strings.TrimSpace(profileID) == "" {
		return nil, nil
	}
	value, ok, err := c.reminders.Get(ctx, reminder.ProfileTypePSIM, profileID)
	if err != nil || !ok {
		return nil, err
	}
	details := reminder.DetailsFrom(value)
	return &details, nil
}

func (c *catalog) applyOverviewExtensions(ctx context.Context, device *mmodem.Modem, resp *ModemResponse) error {
	for _, extension := range c.overviewExtensions {
		if extension == nil {
			continue
		}
		if err := extension(ctx, device, &resp.Fields); err != nil {
			return fmt.Errorf("apply modem overview extension: %w", err)
		}
	}
	return nil
}

func accessTechnologyString(access wwanmodem.Technology) string {
	if access == 0 {
		return ""
	}
	priority := []struct {
		technology wwanmodem.Technology
		label      string
	}{
		{wwanmodem.TechnologyNR5GSA, "5GNR"},
		{wwanmodem.TechnologyNR5GNSA, "5GNR"},
		{wwanmodem.TechnologyLTE, "LTE"},
		{wwanmodem.TechnologyLTECatM, "LTE Cat-M"},
		{wwanmodem.TechnologyLTENB, "LTE NB-IoT"},
		{wwanmodem.TechnologyUMTS, "UMTS"},
		{wwanmodem.TechnologyGSM, "GSM"},
	}
	for _, candidate := range priority {
		if access&candidate.technology != 0 {
			return candidate.label
		}
	}
	return ""
}

func registrationStateName(state wwanmodem.RegistrationState) string {
	switch state {
	case wwanmodem.RegistrationIdle:
		return "Idle"
	case wwanmodem.RegistrationSearching:
		return "Searching"
	case wwanmodem.RegistrationHome:
		return "Home"
	case wwanmodem.RegistrationRoaming:
		return "Roaming"
	case wwanmodem.RegistrationDenied:
		return "Denied"
	default:
		return "Unknown"
	}
}
