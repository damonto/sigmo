package modem

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type catalog struct {
	store              *settings.Store
	registry           *mmodem.Registry
	overviewExtensions []modemstatus.Extension
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
	response := make([]*ModemResponse, 0, len(modems))
	for _, device := range modems {
		modemResp, err := c.buildResponse(ctx, device)
		if err != nil {
			return nil, err
		}
		response = append(response, modemResp)
	}
	slices.SortFunc(response, func(a, b *ModemResponse) int {
		return strings.Compare(a.ID, b.ID)
	})
	return response, nil
}

func (c *catalog) Get(ctx context.Context, modem *mmodem.Modem) (*ModemResponse, error) {
	resp, err := c.buildResponse(ctx, modem)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *catalog) buildResponse(ctx context.Context, device *mmodem.Modem) (*ModemResponse, error) {
	if device.State == mmodem.ModemStateLocked {
		return c.buildLockedResponse(ctx, device)
	}

	sim, err := device.SIMs().Primary(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch primary SIM: %w", err)
	}

	percent, _, err := device.SignalQuality(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch signal quality: %w", err)
	}

	access, err := device.AccessTechnologies(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch access technologies: %w", err)
	}

	threeGpp := device.ThreeGPP()
	registrationState, err := threeGpp.RegistrationState(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch registration state: %w", err)
	}

	registeredOperatorName, err := threeGpp.OperatorName(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch operator name: %w", err)
	}

	operatorCode, err := threeGpp.OperatorCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch operator code: %w", err)
	}

	carrierInfo := carrier.Lookup(sim.OperatorIdentifier)
	supportsEsim, err := supportsEsim(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("detect eSIM support: %w", err)
	}

	simSlots, err := c.buildSlotsResponse(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("fetch SIM slots: %w", err)
	}

	alias := c.store.FindModem(device.EquipmentIdentifier).Alias
	name := device.Model
	if alias != "" {
		name = alias
	}
	simOperatorName := carrierInfo.Name
	if sim.OperatorName != "" {
		simOperatorName = sim.OperatorName
	}
	resp := &ModemResponse{
		Manufacturer:     device.Manufacturer,
		ID:               device.EquipmentIdentifier,
		FirmwareRevision: device.FirmwareRevision,
		HardwareRevision: device.HardwareRevision,
		Name:             name,
		Number:           device.Number,
		State:            modemStateValue(device.State),
		UnlockRequired:   device.UnlockRequired.String(),
		UnlockSupported:  unlockSupported(device),
		SIM: SlotResponse{
			Active:             sim.Active,
			OperatorName:       simOperatorName,
			OperatorIdentifier: sim.OperatorIdentifier,
			RegionCode:         carrierInfo.Region,
			Identifier:         sim.Identifier,
		},
		Slots:             simSlots,
		AccessTechnology:  accessTechnologyString(access),
		RegistrationState: registrationState.String(),
		RegisteredOperator: RegisteredOperatorResponse{
			Name: registeredOperatorName,
			Code: operatorCode,
		},
		SignalQuality: percent,
		SupportsEsim:  supportsEsim,
	}
	if err := c.applyOverviewExtensions(ctx, device, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *catalog) buildLockedResponse(ctx context.Context, device *mmodem.Modem) (*ModemResponse, error) {
	alias := c.store.FindModem(device.EquipmentIdentifier).Alias
	name := device.Model
	if alias != "" {
		name = alias
	}
	supportsEsim, err := supportsEsim(ctx, device)
	if err != nil {
		slog.Warn("detect eSIM support for locked modem", "imei", device.EquipmentIdentifier, "error", err)
	}
	return &ModemResponse{
		Manufacturer:     device.Manufacturer,
		ID:               device.EquipmentIdentifier,
		FirmwareRevision: device.FirmwareRevision,
		HardwareRevision: device.HardwareRevision,
		Name:             name,
		Number:           device.Number,
		State:            modemStateValue(device.State),
		UnlockRequired:   device.UnlockRequired.String(),
		UnlockSupported:  unlockSupported(device),
		SupportsEsim:     supportsEsim,
		Slots:            []SlotResponse{},
	}, nil
}

func modemStateValue(state mmodem.ModemState) string {
	switch state {
	case mmodem.ModemStateFailed:
		return "failed"
	case mmodem.ModemStateUnknown:
		return "unknown"
	case mmodem.ModemStateInitializing:
		return "initializing"
	case mmodem.ModemStateLocked:
		return "locked"
	case mmodem.ModemStateDisabled:
		return "disabled"
	case mmodem.ModemStateDisabling:
		return "disabling"
	case mmodem.ModemStateEnabling:
		return "enabling"
	case mmodem.ModemStateEnabled:
		return "enabled"
	case mmodem.ModemStateSearching:
		return "searching"
	case mmodem.ModemStateRegistered:
		return "registered"
	case mmodem.ModemStateDisconnecting:
		return "disconnecting"
	case mmodem.ModemStateConnecting:
		return "connecting"
	case mmodem.ModemStateConnected:
		return "connected"
	default:
		return "unknown"
	}
}

func unlockSupported(device *mmodem.Modem) bool {
	return device.State == mmodem.ModemStateLocked && device.UnlockRequired == mmodem.ModemLockSimPin
}

func (c *catalog) buildSlotsResponse(ctx context.Context, device *mmodem.Modem) ([]SlotResponse, error) {
	if len(device.SimSlots) == 0 {
		return []SlotResponse{}, nil
	}
	simSlots := make([]SlotResponse, 0, len(device.SimSlots))
	for _, slotPath := range device.SimSlots {
		sim, err := device.SIMs().Get(ctx, slotPath)
		if err != nil {
			return nil, fmt.Errorf("fetch SIM for slot %s: %w", slotPath, err)
		}
		carrierInfo := carrier.Lookup(sim.OperatorIdentifier)
		operatorName := carrierInfo.Name
		if sim.OperatorName != "" {
			operatorName = sim.OperatorName
		}
		simSlots = append(simSlots, SlotResponse{
			Active:             sim.Active,
			OperatorName:       operatorName,
			OperatorIdentifier: sim.OperatorIdentifier,
			RegionCode:         carrierInfo.Region,
			Identifier:         sim.Identifier,
		})
	}
	return simSlots, nil
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

func supportsEsim(ctx context.Context, m *mmodem.Modem) (bool, error) {
	return mmodem.SupportsEUICC(ctx, m)
}

func accessTechnologyString(access []mmodem.ModemAccessTechnology) string {
	if len(access) == 0 {
		return ""
	}
	priority := []mmodem.ModemAccessTechnology{
		mmodem.ModemAccessTechnology5GNR,
		mmodem.ModemAccessTechnologyLte,
		mmodem.ModemAccessTechnologyLteCatM,
		mmodem.ModemAccessTechnologyLteNBIot,
		mmodem.ModemAccessTechnologyHspaPlus,
		mmodem.ModemAccessTechnologyHspa,
		mmodem.ModemAccessTechnologyHsupa,
		mmodem.ModemAccessTechnologyHsdpa,
		mmodem.ModemAccessTechnologyUmts,
		mmodem.ModemAccessTechnologyEdge,
		mmodem.ModemAccessTechnologyGprs,
		mmodem.ModemAccessTechnologyGsm,
		mmodem.ModemAccessTechnologyGsmCompact,
		mmodem.ModemAccessTechnologyEvdob,
		mmodem.ModemAccessTechnologyEvdoa,
		mmodem.ModemAccessTechnologyEvdo0,
		mmodem.ModemAccessTechnology1xrtt,
		mmodem.ModemAccessTechnologyPots,
	}
	for _, tech := range priority {
		if slices.Contains(access, tech) {
			return tech.String()
		}
	}
	return access[0].String()
}
