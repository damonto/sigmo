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

func TestDeviceVoLTEStatusMBIM(t *testing.T) {
	device, err := Open(Config{PortType: PortTypeMBIM, Device: "/dev/cdc-wdm0", Slot: 1})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	got, err := device.VoLTEStatus(context.Background())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("VoLTEStatus() error = %v, want %v", err, ErrUnsupported)
	}
	if got != (VoLTEStatus{}) {
		t.Fatalf("VoLTEStatus() = %+v, want zero status", got)
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

func TestDeviceIMSProfileMBIM(t *testing.T) {
	tests := []struct {
		name     string
		contexts []uiccmbim.ProvisionedContext
		want     IMSProfile
		wantErr  bool
	}{
		{name: "finds IMS profile", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "ims"}}},
		{name: "requires IMS APN", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "internet"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := mbimDeviceWithNetwork(&fakeMBIMNetwork{contexts: slices.Clone(tt.contexts)})
			got, err := device.IMSProfile(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("IMSProfile() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IMSProfile() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("IMSProfile() = %+v, want %+v", got, tt.want)
			}
		})
	}
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
