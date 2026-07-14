package wwan

import (
	"context"
	"errors"
	"slices"
	"testing"

	uiccmbim "github.com/damonto/wwan-go/mbim"
)

func TestDeviceMSISDNMBIM(t *testing.T) {
	readErr := errors.New("subscriber status unavailable")
	tests := []struct {
		name    string
		numbers []string
		err     error
		want    string
	}{
		{name: "single number", numbers: []string{"+15551234567"}, want: "+15551234567"},
		{name: "first non-empty number", numbers: []string{" ", " +8613800138000 "}, want: "+8613800138000"},
		{name: "empty list"},
		{name: "query error", err: readErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMNetwork{subscriberReady: uiccmbim.SubscriberReadyStatusResponse{TelephoneNumbers: slices.Clone(tt.numbers)}, subscriberReadyErr: tt.err}
			got, err := mbimDeviceWithNetwork(client).MSISDN(context.Background())
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("MSISDN() error = %v, want %v", err, tt.err)
				}
			} else if err != nil {
				t.Fatalf("MSISDN() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("MSISDN() = %q, want %q", got, tt.want)
			}
			if !client.closed {
				t.Fatal("client was not closed")
			}
		})
	}
}

func TestDeviceAirplaneModeMBIM(t *testing.T) {
	tests := []struct {
		name  string
		state uiccmbim.RadioSwitchState
		want  bool
	}{
		{name: "on", state: uiccmbim.RadioSwitchStateOn},
		{name: "off", state: uiccmbim.RadioSwitchStateOff, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMRadio{
				state: uiccmbim.RadioStateInfo{SwRadioState: tt.state},
			}
			device := mbimDeviceWithRadio(t, "/dev/cdc-wdm0", client, nil)

			got, err := device.AirplaneMode(context.Background())
			if err != nil {
				t.Fatalf("AirplaneMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("AirplaneMode() = %v, want %v", got, tt.want)
			}
			if !client.closed {
				t.Fatal("MBIM client closed = false, want true")
			}
		})
	}
}

func TestDeviceSetAirplaneModeMBIM(t *testing.T) {
	tests := []struct {
		name  string
		want  bool
		state uiccmbim.RadioSwitchState
	}{
		{name: "enable", want: true, state: uiccmbim.RadioSwitchStateOff},
		{name: "disable", state: uiccmbim.RadioSwitchStateOn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMRadio{}
			device := mbimDeviceWithRadio(t, "/dev/cdc-wdm0", client, nil)

			if err := device.SetAirplaneMode(context.Background(), tt.want); err != nil {
				t.Fatalf("SetAirplaneMode() error = %v", err)
			}
			if client.setState != tt.state {
				t.Fatalf("MBIM set state = %d, want %d", client.setState, tt.state)
			}
			if !client.closed {
				t.Fatal("MBIM client closed = false, want true")
			}
		})
	}
}

func TestDeviceVoLTEStatusMBIM(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "native IMS ownership is unavailable", ctx: context.Background()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (mbimDevice{}).VoLTEStatus(tt.ctx)
			if err != nil {
				t.Fatalf("VoLTEStatus() error = %v", err)
			}
			if got != (VoLTEStatus{}) {
				t.Fatalf("VoLTEStatus() = %+v, want zero status", got)
			}
		})
	}
}

func TestDevicePacketServiceStatusMBIM(t *testing.T) {
	tests := []struct {
		name         string
		registration uiccmbim.RegisterState
		packet       uiccmbim.PacketServiceInfo
		want         PacketServiceStatus
	}{
		{
			name:         "registered attached LTE",
			registration: uiccmbim.RegisterStateHome,
			packet: uiccmbim.PacketServiceInfo{
				PacketServiceState:        uiccmbim.PacketServiceStateAttached,
				HighestAvailableDataClass: mbimDataClassLTE,
			},
			want: PacketServiceStatus{Registered: true, PSAttached: true, LTE: true},
		},
		{name: "searching detached", registration: uiccmbim.RegisterStateSearching},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMNetwork{
				registration: uiccmbim.RegistrationStateInfo{RegisterState: tt.registration},
				packet:       tt.packet,
			}
			device := mbimDeviceWithNetwork(client)
			got, err := device.PacketServiceStatus(context.Background())
			if err != nil {
				t.Fatalf("PacketServiceStatus() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("PacketServiceStatus() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDeviceIMSProfileIndexMBIM(t *testing.T) {
	tests := []struct {
		name     string
		contexts []uiccmbim.ProvisionedContext
		want     uint8
		wantErr  bool
	}{
		{name: "finds IMS profile", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "ims"}}},
		{name: "requires IMS APN", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "internet"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := mbimDeviceWithNetwork(&fakeMBIMNetwork{contexts: slices.Clone(tt.contexts)})
			got, err := device.IMSProfileIndex(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("IMSProfileIndex() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IMSProfileIndex() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("IMSProfileIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDeviceIMSProfileMutationsMBIMUnsupported(t *testing.T) {
	tests := []struct {
		name  string
		apply func(context.Context, mbimDevice) error
	}{
		{name: "set default", apply: func(ctx context.Context, device mbimDevice) error { return device.SetIMSProfileDefault(ctx, 1) }},
		{name: "enable P-CSCF via PCO", apply: func(ctx context.Context, device mbimDevice) error { return device.SetIMSProfilePCSCFViaPCO(ctx, 1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.apply(context.Background(), mbimDevice{}); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("apply() error = %v, want %v", err, ErrUnsupported)
			}
		})
	}
}

type fakeMBIMRadio struct {
	state    uiccmbim.RadioStateInfo
	setState uiccmbim.RadioSwitchState
	closed   bool
}

type fakeMBIMNetwork struct {
	subscriberReady    uiccmbim.SubscriberReadyStatusResponse
	subscriberReadyErr error
	registration       uiccmbim.RegistrationStateInfo
	registrationErr    error
	packet             uiccmbim.PacketServiceInfo
	packetErr          error
	contexts           []uiccmbim.ProvisionedContext
	contextsErr        error
	closed             bool
}

func (r *fakeMBIMNetwork) SubscriberReadyStatus(context.Context) (uiccmbim.SubscriberReadyStatusResponse, error) {
	return r.subscriberReady, r.subscriberReadyErr
}

func (r *fakeMBIMNetwork) RegistrationState(context.Context) (uiccmbim.RegistrationStateInfo, error) {
	return r.registration, r.registrationErr
}

func (r *fakeMBIMNetwork) PacketService(context.Context) (uiccmbim.PacketServiceInfo, error) {
	return r.packet, r.packetErr
}

func (r *fakeMBIMNetwork) ProvisionedContexts(context.Context) ([]uiccmbim.ProvisionedContext, error) {
	return slices.Clone(r.contexts), r.contextsErr
}

func (r *fakeMBIMNetwork) Close() error {
	r.closed = true
	return nil
}

func mbimDeviceWithNetwork(client mbimNetwork) mbimDevice {
	return mbimDevice{
		openNetwork: func(context.Context) (mbimNetwork, error) {
			return client, nil
		},
	}
}

func (r *fakeMBIMRadio) RadioState(context.Context) (uiccmbim.RadioStateInfo, error) {
	return r.state, nil
}

func (r *fakeMBIMRadio) SetRadioState(_ context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error) {
	r.setState = state
	return uiccmbim.RadioStateInfo{SwRadioState: state}, nil
}

func (r *fakeMBIMRadio) Close() error {
	r.closed = true
	return nil
}

func mbimDeviceWithRadio(t *testing.T, wantDevice string, client mbimRadio, openErr error) mbimDevice {
	t.Helper()

	return mbimDevice{
		device: wantDevice,
		slot:   1,
		openRadio: func(context.Context) (mbimRadio, error) {
			return client, openErr
		},
	}
}
