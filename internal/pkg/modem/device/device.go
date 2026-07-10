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

var (
	ErrUnsupported    = errors.New("modem capability is not supported")
	errDeviceRequired = errors.New("modem device path is required")
)

type PortType uint8

const (
	PortTypeQMI PortType = iota + 1
	PortTypeMBIM
)

type Config struct {
	PortType PortType
	Device   string
	Slot     uint8
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
	ATR(ctx context.Context) ([]byte, error)
	PowerCycleSIM(ctx context.Context) error
	ActivateProvisioningIfSIMMissing(ctx context.Context) error
	USIM(ctx context.Context) (usimcard.Reader, error)
	USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error)
	SIMState(ctx context.Context, target Target) (SIMState, error)
	VoLTEStatus(ctx context.Context) (VoLTEStatus, error)
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

type VoLTEStatus struct {
	Supported bool
	Known     bool
	CanEnable bool
}

func Open(cfg Config) (*Device, error) {
	if strings.TrimSpace(cfg.Device) == "" {
		return nil, errDeviceRequired
	}
	if err := validateSIMSlot(cfg.Slot); err != nil {
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

func (d *Device) ATR(ctx context.Context) ([]byte, error) {
	return d.adapter.ATR(ctx)
}

func (d *Device) PowerCycleSIM(ctx context.Context) error {
	return d.adapter.PowerCycleSIM(ctx)
}

func (d *Device) ActivateProvisioningIfSIMMissing(ctx context.Context) error {
	return d.adapter.ActivateProvisioningIfSIMMissing(ctx)
}

func (d *Device) USIM(ctx context.Context) (usimcard.Reader, error) {
	return d.adapter.USIM(ctx)
}

func (d *Device) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	return d.adapter.USIMWithCAT(ctx, profile)
}

func (d *Device) SIMState(ctx context.Context, target Target) (SIMState, error) {
	return d.adapter.SIMState(ctx, target)
}

func (d *Device) VoLTEStatus(ctx context.Context) (VoLTEStatus, error) {
	return d.adapter.VoLTEStatus(ctx)
}

func targetSIMSlot(primarySlot uint8, target Target) (uint8, error) {
	if target.Slot != 0 {
		if target.Slot > MaxSIMSlot {
			return 0, fmt.Errorf("sim slot %d is out of range", target.Slot)
		}
		return uint8(target.Slot), nil
	}
	return primarySlot, validateSIMSlot(primarySlot)
}

func validateSIMSlot(slot uint8) error {
	if slot == 0 {
		return errors.New("sim slot is required")
	}
	if slot > MaxSIMSlot {
		return fmt.Errorf("sim slot %d is out of range", slot)
	}
	return nil
}

func closeReader(message string, reader closer) {
	if err := reader.Close(); err != nil {
		slog.Debug(message, "error", err)
	}
}
