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
	openNetwork func(context.Context) (mbimNetwork, error)
}

func newMBIMDevice(device string, slot uint8) mbimDevice {
	return mbimDevice{
		device: device,
		slot:   slot,
		openNetwork: func(ctx context.Context) (mbimNetwork, error) {
			return openMBIMClient(ctx, device, slot)
		},
	}
}

func (u mbimDevice) Close() error {
	return nil
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
	defer closeClient(client, "MBIM network")

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

func (u mbimDevice) SIMState(ctx context.Context, target Target) (SIMState, error) {
	slot, err := targetSIMSlot(u.slot, target)
	if err != nil {
		return SIMState{Supported: true}, err
	}
	state := SIMState{Supported: true, Slot: slot}

	client, err := u.openNetwork(ctx)
	if err != nil {
		return state, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient(client, "MBIM network")

	status, err := client.SubscriberReadyStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("read MBIM subscriber ready status: %w", err)
	}
	state.Ready = status.ReadyState == uiccmbim.SubscriberReadyStateInitialized
	state.ICCID = strings.TrimSpace(status.SIMICCID)
	state.Recoverable = state.ICCID != ""
	target.ICCID = strings.TrimSpace(target.ICCID)
	state.Matches = target.ICCID == "" || state.ICCID == target.ICCID
	state.ICCIDMismatch = target.ICCID != "" && state.ICCID != "" && state.ICCID != target.ICCID
	return state, nil
}

func (u mbimDevice) USIM(ctx context.Context) (usimcard.Reader, error) {
	return openMBIMUSIM(ctx, u.device, u.slot)
}

func (u mbimDevice) USIMWithCAT(ctx context.Context, _ CATProfile) (usimcard.Reader, error) {
	return u.USIM(ctx)
}

func (u mbimDevice) PacketServiceStatus(ctx context.Context) (PacketServiceStatus, error) {
	client, err := u.openNetwork(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient(client, "MBIM network")

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

func (u mbimDevice) IMSProfile(ctx context.Context) (IMSProfile, error) {
	client, err := u.openNetwork(ctx)
	if err != nil {
		return IMSProfile{}, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer closeClient(client, "MBIM network")

	found, err := mbimIMSContextAvailable(ctx, client)
	if err != nil {
		return IMSProfile{}, err
	}
	if !found {
		return IMSProfile{}, errors.New("MBIM IMS provisioned context is unavailable")
	}
	// MBIM selects its IMS context by context type and APN, not by the QMI WDS profile index.
	return IMSProfile{}, nil
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

func openMBIMClient(ctx context.Context, device string, slot uint8) (*uiccmbim.Client, error) {
	return uiccmbim.Open(ctx, uiccmbim.WithAutoDetect(device), uiccmbim.WithSlot(int(slot)))
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
