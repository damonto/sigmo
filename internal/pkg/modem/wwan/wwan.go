package wwan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	usimcard "github.com/damonto/wwan-go/sim/card"
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

type deviceOperations struct {
	backend backend
}

// Session keeps reusable QMI or MBIM protocol clients open until Close.
type Session struct {
	deviceOperations
}

type closer interface {
	Close() error
}

type backend interface {
	Close() error
}

type usimBackend interface {
	USIM(ctx context.Context) (usimcard.Reader, error)
	USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error)
}

type simRecoveryBackend interface {
	ActivateProvisioningIfSIMMissing(ctx context.Context) error
}

type simStateBackend interface {
	SIMState(ctx context.Context, target Target) (SIMState, error)
}

type simRefreshBackend interface {
	WatchSIMRefresh(ctx context.Context) (<-chan SIMRefreshEvent, error)
}

type volteStatusBackend interface {
	VoLTEStatus(ctx context.Context) (VoLTEStatus, error)
}

type packetServiceBackend interface {
	PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error)
	IMSProfile(ctx context.Context) (IMSProfile, error)
}

type imsTakeoverBackend interface {
	IMSSTestMode(ctx context.Context) (bool, error)
	SetIMSSTestMode(ctx context.Context, enabled bool) error
}

type subscriberBackend interface {
	MSISDN(ctx context.Context) (string, error)
}

type subscriberWriterBackend interface {
	UpdateMSISDN(ctx context.Context, number string) error
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

type SIMRefreshStage uint8

const (
	SIMRefreshWaitForOK SIMRefreshStage = iota
	SIMRefreshStart
	SIMRefreshEndWithSuccess
	SIMRefreshEndWithFailure
)

type SIMRefreshMode uint8

const (
	SIMRefreshReset SIMRefreshMode = iota
	SIMRefreshInit
	SIMRefreshInitFCN
	SIMRefreshFCN
	SIMRefreshInitFullFCN
	SIMRefreshApplicationReset
	SIMRefresh3GReset
)

type SIMRefreshEvent struct {
	Stage SIMRefreshStage
	Mode  SIMRefreshMode
}

type VoLTEStatus struct {
	Occupied bool
}

type PacketServiceStatus struct {
	Registered bool
	PSAttached bool
	LTE        bool
}

type IMSProfile struct {
	Index   uint8
	PDNType string
}

// OpenSession opens a QMI or MBIM device that reuses protocol clients across operations.
// The caller must close the returned session.
func OpenSession(cfg Config) (*Session, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	var backend backend
	switch cfg.PortType {
	case PortTypeQMI:
		backend = newQMISession(cfg)
	case PortTypeMBIM:
		backend = newMBIMSession(cfg)
	default:
		return nil, ErrUnsupported
	}
	return &Session{deviceOperations: deviceOperations{backend: backend}}, nil
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Device) == "" {
		return errDeviceRequired
	}
	if err := validateSIMSlot(cfg.Slot); err != nil {
		return err
	}
	return nil
}

// Close releases protocol clients retained by the session.
func (s *Session) Close() error {
	if s == nil || s.backend == nil {
		return nil
	}
	return s.backend.Close()
}

func (d *deviceOperations) ActivateProvisioningIfSIMMissing(ctx context.Context) error {
	backend, ok := d.backend.(simRecoveryBackend)
	if !ok {
		return ErrUnsupported
	}
	return backend.ActivateProvisioningIfSIMMissing(ctx)
}

func (d *deviceOperations) USIM(ctx context.Context) (usimcard.Reader, error) {
	backend, ok := d.backend.(usimBackend)
	if !ok {
		return nil, ErrUnsupported
	}
	return backend.USIM(ctx)
}

func (d *deviceOperations) USIMWithCAT(ctx context.Context, profile CATProfile) (usimcard.Reader, error) {
	backend, ok := d.backend.(usimBackend)
	if !ok {
		return nil, ErrUnsupported
	}
	return backend.USIMWithCAT(ctx, profile)
}

func (d *deviceOperations) SIMState(ctx context.Context, target Target) (SIMState, error) {
	backend, ok := d.backend.(simStateBackend)
	if !ok {
		return SIMState{}, ErrUnsupported
	}
	return backend.SIMState(ctx, target)
}

func (d *deviceOperations) WatchSIMRefresh(ctx context.Context) (<-chan SIMRefreshEvent, error) {
	backend, ok := d.backend.(simRefreshBackend)
	if !ok {
		return nil, ErrUnsupported
	}
	return backend.WatchSIMRefresh(ctx)
}

func (d *deviceOperations) VoLTEStatus(ctx context.Context) (VoLTEStatus, error) {
	backend, ok := d.backend.(volteStatusBackend)
	if !ok {
		return VoLTEStatus{}, ErrUnsupported
	}
	return backend.VoLTEStatus(ctx)
}

func (d *deviceOperations) PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error) {
	backend, ok := d.backend.(packetServiceBackend)
	if !ok {
		return PacketServiceStatus{}, ErrUnsupported
	}
	return backend.PacketServiceStatus(ctx)
}

func (d *deviceOperations) IMSProfile(ctx context.Context) (IMSProfile, error) {
	backend, ok := d.backend.(packetServiceBackend)
	if !ok {
		return IMSProfile{}, ErrUnsupported
	}
	return backend.IMSProfile(ctx)
}

func (d *deviceOperations) IMSSTestMode(ctx context.Context) (bool, error) {
	backend, ok := d.backend.(imsTakeoverBackend)
	if !ok {
		return false, ErrUnsupported
	}
	return backend.IMSSTestMode(ctx)
}

func (d *deviceOperations) SetIMSSTestMode(ctx context.Context, enabled bool) error {
	backend, ok := d.backend.(imsTakeoverBackend)
	if !ok {
		return ErrUnsupported
	}
	return backend.SetIMSSTestMode(ctx, enabled)
}

func (d *deviceOperations) MSISDN(ctx context.Context) (string, error) {
	backend, ok := d.backend.(subscriberBackend)
	if !ok {
		return "", ErrUnsupported
	}
	return backend.MSISDN(ctx)
}

func (d *deviceOperations) UpdateMSISDN(ctx context.Context, number string) error {
	backend, ok := d.backend.(subscriberWriterBackend)
	if !ok {
		return ErrUnsupported
	}
	return backend.UpdateMSISDN(ctx, number)
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

func closeClient(client closer, kind string) {
	if err := client.Close(); err != nil {
		slog.Debug("close modem client", "kind", kind, "error", err)
	}
}
