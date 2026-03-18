package modem

import (
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Service struct {
	cfg     *config.Config
	manager *mmodem.Manager
}

func NewService(cfg *config.Config, manager *mmodem.Manager) *Service {
	return &Service{
		cfg:     cfg,
		manager: manager,
	}
}

func (s *Service) List() ([]*ModemResponse, error) {
	modems, err := s.manager.Modems()
	if err != nil {
		slog.Error("failed to list modems", "error", err)
		return nil, err
	}
	response := make([]*ModemResponse, 0, len(modems))
	for _, m := range modems {
		modemResp, err := s.buildModemResponse(m)
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

func (s *Service) Get(modem *mmodem.Modem) (*ModemResponse, error) {
	resp, err := s.buildModemResponse(modem)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Service) buildModemResponse(m *mmodem.Modem) (*ModemResponse, error) {
	sim, err := m.SIMs().Primary()
	if err != nil {
		slog.Error("failed to fetch SIM", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	percent, _, err := m.SignalQuality()
	if err != nil {
		slog.Error("failed to fetch signal quality", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	access, err := m.AccessTechnologies()
	if err != nil {
		slog.Error("failed to fetch access technologies", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	threeGpp := m.ThreeGPP()
	registrationState, err := threeGpp.RegistrationState()
	if err != nil {
		slog.Error("failed to fetch registration state", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	registeredOperatorName, err := threeGpp.OperatorName()
	if err != nil {
		slog.Error("failed to fetch operator name", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	operatorCode, err := threeGpp.OperatorCode()
	if err != nil {
		slog.Error("failed to fetch operator code", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	carrierInfo := carrier.Lookup(sim.OperatorIdentifier)
	supportsEsim, err := supportsEsim(m, s.cfg)
	if err != nil {
		slog.Error("failed to detect eSIM support", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	simSlots, err := s.buildSimSlotsResponse(m)
	if err != nil {
		slog.Error("failed to fetch SIM slots", "modem", m.EquipmentIdentifier, "error", err)
		return nil, err
	}

	alias := s.cfg.FindModem(m.EquipmentIdentifier).Alias
	name := m.Model
	if alias != "" {
		name = alias
	}
	simOperatorName := carrierInfo.Name
	if sim.OperatorName != "" {
		simOperatorName = sim.OperatorName
	}
	return &ModemResponse{
		Manufacturer:     m.Manufacturer,
		ID:               m.EquipmentIdentifier,
		FirmwareRevision: m.FirmwareRevision,
		HardwareRevision: m.HardwareRevision,
		Name:             name,
		Number:           m.Number,
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

func (s *Service) buildSimSlotsResponse(m *mmodem.Modem) ([]SlotResponse, error) {
	if len(m.SimSlots) == 0 {
		return []SlotResponse{}, nil
	}
	simSlots := make([]SlotResponse, 0, len(m.SimSlots))
	for _, slotPath := range m.SimSlots {
		sim, err := m.SIMs().Get(slotPath)
		if err != nil {
			slog.Error("failed to fetch SIM for slot", "modem", m.EquipmentIdentifier, "slot", slotPath, "error", err)
			return nil, err
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

func supportsEsim(m *mmodem.Modem, cfg *config.Config) (bool, error) {
	client, err := lpa.New(m, cfg)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return false, nil
		}
		slog.Error("failed to create LPA client", "modem", m.EquipmentIdentifier, "error", err)
		return false, err
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
