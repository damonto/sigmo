package wwan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	uiccmbim "github.com/damonto/wwan-go/mbim"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

type mbimDevice struct {
	device      string
	slot        uint8
	openRadio   func(context.Context) (mbimRadio, error)
	openNetwork func(context.Context) (mbimNetwork, error)
}

func newMBIMDevice(device string, slot uint8) mbimDevice {
	return mbimDevice{
		device: device,
		slot:   slot,
		openRadio: func(ctx context.Context) (mbimRadio, error) {
			return openMBIMClient(ctx, device, 1)
		},
		openNetwork: func(ctx context.Context) (mbimNetwork, error) {
			return openMBIMClient(ctx, device, slot)
		},
	}
}

func (u mbimDevice) Close() error {
	return nil
}

type mbimRadio interface {
	RadioState(ctx context.Context) (uiccmbim.RadioStateInfo, error)
	SetRadioState(ctx context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error)
	Close() error
}

type mbimNetwork interface {
	SubscriberReadyStatus(ctx context.Context) (uiccmbim.SubscriberReadyStatusResponse, error)
	RegistrationState(ctx context.Context) (uiccmbim.RegistrationStateInfo, error)
	PacketService(ctx context.Context) (uiccmbim.PacketServiceInfo, error)
	ProvisionedContexts(ctx context.Context) ([]uiccmbim.ProvisionedContext, error)
	Close() error
}

func (u mbimDevice) MSISDN(ctx context.Context) (string, error) {
	client, err := u.openNetwork(ctx)
	if err != nil {
		return "", fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient("close MBIM network client", client)

	status, err := client.SubscriberReadyStatus(ctx)
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
	return openMBIMUSIM(ctx, u.device, u.slot)
}

func (u mbimDevice) USIMWithCAT(ctx context.Context, _ CATProfile) (usimcard.Reader, error) {
	return u.USIM(ctx)
}

func (u mbimDevice) ATR(ctx context.Context) ([]byte, error) {
	client, err := openMBIMClient(ctx, u.device, u.slot)
	if err != nil {
		return nil, fmt.Errorf("open MBIM client: %w", err)
	}
	defer closeClient("close MBIM client", client)

	atr, err := client.QueryUiccATR(ctx)
	if err != nil {
		return nil, fmt.Errorf("query MBIM UICC ATR: %w", err)
	}
	return atr, nil
}

func (mbimDevice) VoLTEStatus(context.Context) (VoLTEStatus, error) {
	// MBIM does not expose the modem's native IMS registration ownership.
	return VoLTEStatus{}, nil
}

func (u mbimDevice) PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error) {
	client, err := u.openNetwork(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient("close MBIM network client", client)

	registration, err := client.RegistrationState(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read MBIM registration state: %w", err)
	}
	packet, err := client.PacketService(ctx)
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
	client, err := u.openNetwork(ctx)
	if err != nil {
		return 0, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient("close MBIM network client", client)

	found, err := mbimIMSContextAvailable(ctx, client)
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

func mbimIMSContextAvailable(ctx context.Context, client mbimNetwork) (bool, error) {
	contexts, err := client.ProvisionedContexts(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM provisioned contexts: %w", err)
	}
	for _, profile := range contexts {
		if profile.ContextType != uiccmbim.ContextTypeIMS || !strings.EqualFold(strings.TrimSpace(profile.AccessString), uiccmbim.DefaultIMSPDNAPN) {
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
	client, err := u.openRadio(ctx)
	if err != nil {
		return false, fmt.Errorf("open MBIM radio client: %w", err)
	}
	defer closeClient("close MBIM radio client", client)

	state, err := client.RadioState(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM radio state: %w", err)
	}
	return state.SwRadioState == uiccmbim.RadioSwitchStateOff, nil
}

func (u mbimDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	client, err := u.openRadio(ctx)
	if err != nil {
		return fmt.Errorf("open MBIM radio client: %w", err)
	}
	defer closeClient("close MBIM radio client", client)

	return setMBIMAirplaneMode(ctx, client, enabled)
}

func setMBIMAirplaneMode(ctx context.Context, client mbimRadio, enabled bool) error {
	state := uiccmbim.RadioSwitchStateOn
	if enabled {
		state = uiccmbim.RadioSwitchStateOff
	}
	if _, err := client.SetRadioState(ctx, state); err != nil {
		return fmt.Errorf("set MBIM radio state: %w", err)
	}
	return nil
}

func openMBIMClient(ctx context.Context, device string, slot uint8) (*uiccmbim.Client, error) {
	return uiccmbim.Open(ctx, uiccmbim.WithProxy(device), uiccmbim.WithSlot(int(slot)))
}

func openMBIMUSIM(ctx context.Context, device string, slot uint8) (usimcard.Reader, error) {
	client, err := openMBIMClient(ctx, device, slot)
	if err != nil {
		return nil, err
	}
	adapter, err := usim.NewMBIM(client)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return adapter, nil
}
