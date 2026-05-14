package modem

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type catalog struct {
	store   *config.Store
	manager *mmodem.Manager
}

func newCatalog(store *config.Store, manager *mmodem.Manager) *catalog {
	return &catalog{
		store:   store,
		manager: manager,
	}
}

func (c *catalog) List() ([]*ModemResponse, error) {
	modems, err := c.manager.Modems()
	if err != nil {
		return nil, fmt.Errorf("list modems: %w", err)
	}
	response := make([]*ModemResponse, 0, len(modems))
	for _, device := range modems {
		modemResp, err := c.buildResponse(device)
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

func (c *catalog) Get(modem *mmodem.Modem) (*ModemResponse, error) {
	resp, err := c.buildResponse(modem)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *catalog) buildResponse(device *mmodem.Modem) (*ModemResponse, error) {
	sim, err := device.SIMs().Primary()
	if err != nil {
		return nil, fmt.Errorf("fetch primary SIM: %w", err)
	}

	percent, _, err := device.SignalQuality()
	if err != nil {
		return nil, fmt.Errorf("fetch signal quality: %w", err)
	}

	access, err := device.AccessTechnologies()
	if err != nil {
		return nil, fmt.Errorf("fetch access technologies: %w", err)
	}

	threeGpp := device.ThreeGPP()
	registrationState, err := threeGpp.RegistrationState()
	if err != nil {
		return nil, fmt.Errorf("fetch registration state: %w", err)
	}

	registeredOperatorName, err := threeGpp.OperatorName()
	if err != nil {
		return nil, fmt.Errorf("fetch operator name: %w", err)
	}

	operatorCode, err := threeGpp.OperatorCode()
	if err != nil {
		return nil, fmt.Errorf("fetch operator code: %w", err)
	}

	carrierInfo := carrier.Lookup(sim.OperatorIdentifier)
	supportsEsim, err := supportsEsim(device, c.store)
	if err != nil {
		return nil, fmt.Errorf("detect eSIM support: %w", err)
	}

	simSlots, err := c.buildSlotsResponse(device)
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
	return &ModemResponse{
		Manufacturer:     device.Manufacturer,
		ID:               device.EquipmentIdentifier,
		FirmwareRevision: device.FirmwareRevision,
		HardwareRevision: device.HardwareRevision,
		Name:             name,
		Number:           device.Number,
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
	}, nil
}

func (c *catalog) buildSlotsResponse(device *mmodem.Modem) ([]SlotResponse, error) {
	if len(device.SimSlots) == 0 {
		return []SlotResponse{}, nil
	}
	simSlots := make([]SlotResponse, 0, len(device.SimSlots))
	for _, slotPath := range device.SimSlots {
		sim, err := device.SIMs().Get(slotPath)
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

func supportsEsim(m *mmodem.Modem, store *config.Store) (bool, error) {
	cfg := store.Snapshot()
	client, err := lpa.New(m, &cfg)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return false, nil
		}
		return false, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	return true, nil
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
