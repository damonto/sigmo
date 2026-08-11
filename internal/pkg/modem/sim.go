package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type SIMs struct{ modem *Modem }

func (m *Modem) SIMs() *SIMs { return &SIMs{modem: m} }

type SIM struct {
	modem              *Modem
	Slot               uint32
	Active             bool
	Identifier         string
	ATR                []byte
	EID                string
	IMSI               string
	OperatorIdentifier string
	OperatorName       string
	GID1               string
	SPN                string
}

func (s *SIMs) Primary(ctx context.Context) (*SIM, error) {
	if s == nil || s.modem == nil || s.modem.core == nil {
		return nil, errModemRequired
	}
	info, err := s.modem.core.SIMInfo(ctx)
	if err != nil {
		return nil, err
	}
	s.modem.applySIMInfo(info)
	return cloneSIM(s.modem, s.modem.Snapshot().SIM), nil
}

func (s *SIMs) Get(ctx context.Context, slot uint32) (*SIM, error) {
	if s == nil || s.modem == nil || s.modem.core == nil {
		return nil, errModemRequired
	}
	if slot == 0 || slot > 255 {
		return nil, fmt.Errorf("SIM slot %d is invalid", slot)
	}
	info, infoErr := s.modem.core.SIMInfo(ctx)
	if infoErr == nil {
		s.modem.applySIMInfo(info)
		snapshot := s.modem.Snapshot()
		if snapshot.PrimarySIMSlot == slot && snapshot.SIM != nil {
			return cloneSIM(s.modem, snapshot.SIM), nil
		}
	}
	slots, err := s.modem.core.SIMSlots(ctx)
	if err != nil {
		return nil, err
	}
	s.modem.applySIMSlots(slots)
	for _, item := range slots {
		if uint32(item.Index) != slot {
			continue
		}
		return &SIM{modem: s.modem, Slot: slot, Active: item.Active, Identifier: strings.TrimSpace(item.ICCID), EID: strings.TrimSpace(item.EID)}, nil
	}
	return nil, fmt.Errorf("SIM slot %d: %w", slot, ErrNotFound)
}

func simFromInfo(m *Modem, info wwanmodem.SIMInfo) *SIM {
	return &SIM{
		modem: m, Slot: uint32(info.Slot), Active: true,
		Identifier: strings.TrimSpace(info.ICCID), ATR: slices.Clone(info.ATR),
		EID: strings.TrimSpace(info.EID), IMSI: strings.TrimSpace(info.IMSI),
		OperatorIdentifier: strings.TrimSpace(info.OperatorID), OperatorName: strings.TrimSpace(info.OperatorName),
		GID1: strings.ToUpper(strings.TrimSpace(info.GID1)), SPN: strings.TrimSpace(info.SPN),
	}
}

func (s *SIM) SendPIN(ctx context.Context, pin string) error {
	if s == nil || s.modem == nil || s.modem.core == nil {
		return errors.New("SIM is required")
	}
	return s.modem.core.SendPIN(ctx, pin)
}
