package device

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	usimcard "github.com/damonto/uicc-go/usim/card"
)

const MaxSIMSlot = 5

var ErrUnsupported = errors.New("modem device is not supported")

type PortType uint8

const (
	PortTypeQMI PortType = iota + 1
	PortTypeMBIM
)

type Config struct {
	PortType PortType
	Device   string
	Slot     int
	IMEI     string
}

type Target struct {
	Slot  uint32
	ICCID string
}

type Device struct {
	adapter adapter
}

type closer interface {
	Close() error
}

type adapter interface {
	AirplaneMode(ctx context.Context) (bool, error)
	SetAirplaneMode(ctx context.Context, enabled bool) error
	ToggleAirplaneMode(ctx context.Context) (bool, error)
	ATR(ctx context.Context) ([]byte, error)
	USIM(ctx context.Context) (usimcard.Reader, error)
	USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error)
}

type statusAdapter interface {
	SlotStatus(ctx context.Context) (SlotStatus, error)
	CardStatus(ctx context.Context) (CardStatus, error)
}

type provisioningAdapter interface {
	ChangeProvisioningSession(ctx context.Context, req ProvisioningSessionRequest) error
	ActivateProvisioningIfSIMMissing(ctx context.Context) error
}

type powerAdapter interface {
	PowerOffSIM(ctx context.Context) error
	PowerOnSIM(ctx context.Context) error
	PowerCycleSIM(ctx context.Context) error
}

type simStateAdapter interface {
	SIMState(ctx context.Context, target Target) (SIMState, error)
}

type SlotStatus struct {
	ActiveSlot uint8
	Slots      []Slot
}

type Slot struct {
	ICCID string
	ATR   []byte
}

type CardStatus struct {
	Cards []Card
}

type Card struct {
	Present          bool
	USIMApplications []USIMApplication
}

type USIMApplication struct {
	Ready                bool
	AID                  []byte
	ApplicationState     string
	PersonalizationState string
}

type ProvisioningSessionRequest struct {
	Activate bool
	Slot     uint8
	AID      []byte
}

type CATProfile struct {
	Data             []byte
	EventMask        uint32
	FullFunctionMask uint32
}

type SIMState struct {
	Supported     bool
	Matches       bool
	Recoverable   bool
	Ready         bool
	ICCIDMismatch bool
	ICCID         string
	Slot          uint8
}

func Open(cfg Config) (*Device, error) {
	if strings.TrimSpace(cfg.Device) == "" {
		return nil, ErrUnsupported
	}
	if _, err := normalizeSIMSlot(cfg.Slot); err != nil {
		return nil, err
	}
	switch cfg.PortType {
	case PortTypeQMI:
		return &Device{adapter: newQMIDevice(cfg.Device, cfg.Slot, cfg.IMEI)}, nil
	case PortTypeMBIM:
		return &Device{adapter: newMBIMDevice(cfg.Device, cfg.Slot)}, nil
	default:
		return nil, ErrUnsupported
	}
}

func (d *Device) AirplaneMode(ctx context.Context) (bool, error) {
	return d.adapter.AirplaneMode(ctx)
}

func (d *Device) SetAirplaneMode(ctx context.Context, enabled bool) error {
	return d.adapter.SetAirplaneMode(ctx, enabled)
}

func (d *Device) ToggleAirplaneMode(ctx context.Context) (bool, error) {
	return d.adapter.ToggleAirplaneMode(ctx)
}

func (d *Device) ATR(ctx context.Context) ([]byte, error) {
	return d.adapter.ATR(ctx)
}

func (d *Device) SlotStatus(ctx context.Context) (SlotStatus, error) {
	adapter, ok := d.adapter.(statusAdapter)
	if !ok {
		return SlotStatus{}, nil
	}
	return adapter.SlotStatus(ctx)
}

func (d *Device) CardStatus(ctx context.Context) (CardStatus, error) {
	adapter, ok := d.adapter.(statusAdapter)
	if !ok {
		return CardStatus{}, nil
	}
	return adapter.CardStatus(ctx)
}

func (d *Device) ChangeProvisioningSession(ctx context.Context, req ProvisioningSessionRequest) error {
	adapter, ok := d.adapter.(provisioningAdapter)
	if !ok {
		return nil
	}
	return adapter.ChangeProvisioningSession(ctx, req)
}

func (d *Device) PowerOffSIM(ctx context.Context) error {
	adapter, ok := d.adapter.(powerAdapter)
	if !ok {
		return nil
	}
	return adapter.PowerOffSIM(ctx)
}

func (d *Device) PowerOnSIM(ctx context.Context) error {
	adapter, ok := d.adapter.(powerAdapter)
	if !ok {
		return nil
	}
	return adapter.PowerOnSIM(ctx)
}

func (d *Device) PowerCycleSIM(ctx context.Context) error {
	adapter, ok := d.adapter.(powerAdapter)
	if !ok {
		return nil
	}
	return adapter.PowerCycleSIM(ctx)
}

func (d *Device) ActivateProvisioningIfSIMMissing(ctx context.Context) error {
	adapter, ok := d.adapter.(provisioningAdapter)
	if !ok {
		return nil
	}
	return adapter.ActivateProvisioningIfSIMMissing(ctx)
}

func (d *Device) USIM(ctx context.Context) (usimcard.Reader, error) {
	return d.adapter.USIM(ctx)
}

func (d *Device) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	return d.adapter.USIMWithCAT(ctx, profile)
}

func (d *Device) SIMState(ctx context.Context, target Target) (SIMState, error) {
	adapter, ok := d.adapter.(simStateAdapter)
	if !ok {
		return SIMState{}, nil
	}
	return adapter.SIMState(ctx, target)
}

func targetSIMSlot(primarySlot int, target Target) (uint8, error) {
	if target.Slot != 0 {
		if target.Slot > MaxSIMSlot {
			return 0, fmt.Errorf("SIM slot %d is out of range", target.Slot)
		}
		return uint8(target.Slot), nil
	}
	return normalizeSIMSlot(primarySlot)
}

func normalizeSIMSlot(slot int) (uint8, error) {
	if slot == 0 {
		return 0, errors.New("SIM slot is required")
	}
	if slot < 0 || slot > MaxSIMSlot {
		return 0, fmt.Errorf("SIM slot %d is out of range", slot)
	}
	return uint8(slot), nil
}

func closeReader(message string, reader closer) {
	if err := reader.Close(); err != nil {
		slog.Debug(message, "error", err)
	}
}
