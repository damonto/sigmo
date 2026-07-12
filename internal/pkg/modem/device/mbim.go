package device

import (
	"context"
	"errors"
	"fmt"
	"strings"

	uiccmbim "github.com/damonto/uicc-go/mbim"
	"github.com/damonto/uicc-go/usim"
	usimcard "github.com/damonto/uicc-go/usim/card"
)

type mbimDevice struct {
	device      string
	slot        uint8
	openRadio   func(context.Context) (mbimAirplaneModeReader, error)
	openNetwork func(context.Context) (mbimNetworkReader, error)
}

func newMBIMDevice(device string, slot uint8) mbimDevice {
	return mbimDevice{
		device: device,
		slot:   slot,
		openRadio: func(ctx context.Context) (mbimAirplaneModeReader, error) {
			return openMBIMReader(ctx, device, 1)
		},
		openNetwork: func(ctx context.Context) (mbimNetworkReader, error) {
			return openMBIMReader(ctx, device, slot)
		},
	}
}

type mbimAirplaneModeReader interface {
	RadioState(ctx context.Context) (uiccmbim.RadioStateInfo, error)
	SetRadioState(ctx context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error)
	Close() error
}

type mbimNetworkReader interface {
	SubscriberReadyStatus(ctx context.Context) (uiccmbim.SubscriberReadyStatusResponse, error)
	DeviceCaps(ctx context.Context) (uiccmbim.DeviceCapsInfo, error)
	RegistrationState(ctx context.Context) (uiccmbim.RegistrationStateInfo, error)
	PacketService(ctx context.Context) (uiccmbim.PacketServiceInfo, error)
	ProvisionedContexts(ctx context.Context) ([]uiccmbim.ProvisionedContext, error)
	Close() error
}

func (u mbimDevice) MSISDN(ctx context.Context) (string, error) {
	reader, err := u.openNetwork(ctx)
	if err != nil {
		return "", fmt.Errorf("open MBIM network reader: %w", err)
	}
	defer closeReader("close MBIM network reader", reader)

	status, err := reader.SubscriberReadyStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("read MBIM subscriber ready status: %w", err)
	}
	for _, number := range status.TelephoneNumbers {
		if number = strings.TrimSpace(number); number != "" {
			return number, nil
		}
	}
	return "", nil
}

func (u mbimDevice) UpdateMSISDN(context.Context, string) error {
	return ErrUnsupported
}

func (u mbimDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	return openMBIMUSIMReader(ctx, u.device, u.slot)
}

func (u mbimDevice) USIMWithCAT(ctx context.Context, _ CATProfile) (usimcard.Reader, error) {
	return u.USIM(ctx)
}

func (u mbimDevice) ATR(ctx context.Context) ([]byte, error) {
	reader, err := openMBIMReader(ctx, u.device, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open MBIM reader: %w", err)
	}
	defer closeReader("close MBIM reader", reader)

	atr, err := reader.QueryUiccATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("query MBIM UICC ATR: %w", err)
	}
	return atr, nil
}

func (u mbimDevice) VoLTEStatus(ctx context.Context) (VoLTEStatus, error) {
	reader, err := u.openNetwork(ctx)
	if err != nil {
		return VoLTEStatus{}, fmt.Errorf("open MBIM network reader: %w", err)
	}
	defer closeReader("close MBIM network reader", reader)

	found, err := mbimIMSContextAvailable(ctx, reader)
	if err != nil {
		return VoLTEStatus{}, err
	}
	if !found {
		return VoLTEStatus{}, nil
	}
	caps, err := reader.DeviceCaps(ctx)
	if err != nil {
		return VoLTEStatus{}, fmt.Errorf("read MBIM device capabilities: %w", err)
	}
	return VoLTEStatus{Supported: caps.MaxSessions > uiccmbim.DefaultIMSPDNSessionID}, nil
}

func (u mbimDevice) PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error) {
	reader, err := u.openNetwork(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open MBIM network reader: %w", err)
	}
	defer closeReader("close MBIM network reader", reader)

	registration, err := reader.RegistrationState(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read MBIM registration state: %w", err)
	}
	packet, err := reader.PacketService(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read MBIM packet service: %w", err)
	}
	return PacketServiceStatus{
		Registered: mbimRegistered(registration.RegisterState),
		PSAttached: packet.PacketServiceState == uiccmbim.PacketServiceStateAttached,
		LTE:        packet.HighestAvailableDataClass&mbimDataClassLTE != 0,
	}, nil
}

func (u mbimDevice) IMSProfileIndex(ctx context.Context) (uint8, error) {
	reader, err := u.openNetwork(ctx)
	if err != nil {
		return 0, fmt.Errorf("open MBIM network reader: %w", err)
	}
	defer closeReader("close MBIM network reader", reader)

	found, err := mbimIMSContextAvailable(ctx, reader)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errors.New("MBIM IMS provisioned context is unavailable")
	}
	// MBIM selects its IMS context by context type and APN, not by the QMI WDS profile index.
	return 0, nil
}

func (u mbimDevice) IMSSTestMode(context.Context) (bool, error) {
	return false, ErrUnsupported
}

func (u mbimDevice) SetIMSSTestMode(context.Context, bool) error {
	return ErrUnsupported
}

const mbimDataClassLTE uint32 = 1 << 5

func mbimRegistered(state uiccmbim.RegisterState) bool {
	switch state {
	case uiccmbim.RegisterStateHome, uiccmbim.RegisterStateRoaming, uiccmbim.RegisterStatePartner:
		return true
	default:
		return false
	}
}

func mbimIMSContextAvailable(ctx context.Context, reader mbimNetworkReader) (bool, error) {
	contexts, err := reader.ProvisionedContexts(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM provisioned contexts: %w", err)
	}
	for _, profile := range contexts {
		if profile.ContextType != uiccmbim.ContextTypeIMS || !strings.EqualFold(strings.TrimSpace(profile.AccessString), usim.DefaultIMSPDNAPN) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (u mbimDevice) PowerCycleSIM(context.Context) error {
	return ErrUnsupported
}

func (u mbimDevice) ActivateProvisioningIfSIMMissing(context.Context) error {
	return ErrUnsupported
}

func (u mbimDevice) SIMState(context.Context, Target) (SIMState, error) {
	return SIMState{}, nil
}

func (u mbimDevice) AirplaneMode(ctx context.Context) (bool, error) {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open MBIM airplane mode reader: %w", err)
	}
	defer closeReader("close MBIM airplane mode reader", reader)

	state, err := reader.RadioState(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM radio state: %w", err)
	}
	return state.SwRadioState == uiccmbim.RadioSwitchStateOff, nil
}

func (u mbimDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	reader, err := u.openRadio(ctx)
	if err != nil {
		return fmt.Errorf("open MBIM airplane mode reader: %w", err)
	}
	defer closeReader("close MBIM airplane mode reader", reader)

	return setMBIMAirplaneMode(ctx, reader, enabled)
}

func setMBIMAirplaneMode(ctx context.Context, reader mbimAirplaneModeReader, enabled bool) error {
	state := uiccmbim.RadioSwitchStateOn
	if enabled {
		state = uiccmbim.RadioSwitchStateOff
	}
	if _, err := reader.SetRadioState(ctx, state); err != nil {
		return fmt.Errorf("set MBIM radio state: %w", err)
	}
	return nil
}

func openMBIMReader(ctx context.Context, device string, slot uint8) (*uiccmbim.Reader, error) {
	return uiccmbim.Open(ctx, uiccmbim.WithProxy(device), uiccmbim.WithSlot(int(slot)))
}

func openMBIMUSIMReader(ctx context.Context, device string, slot uint8) (usimcard.Reader, error) {
	reader, err := openMBIMReader(ctx, device, slot)
	if err != nil {
		return nil, err
	}
	adapter, err := usim.NewMBIM(reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return adapter, nil
}
