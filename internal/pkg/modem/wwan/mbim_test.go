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
			reader := &fakeMBIMNetworkReader{subscriberReady: uiccmbim.SubscriberReadyStatusResponse{TelephoneNumbers: slices.Clone(tt.numbers)}, subscriberReadyErr: tt.err}
			got, err := mbimDeviceWithNetworkReader(reader).MSISDN(context.Background())
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
			if !reader.closed {
				t.Fatal("reader was not closed")
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
			reader := &fakeMBIMAirplaneModeReader{
				state: uiccmbim.RadioStateInfo{SwRadioState: tt.state},
			}
			device := mbimDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			got, err := device.AirplaneMode(context.Background())
			if err != nil {
				t.Fatalf("AirplaneMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("AirplaneMode() = %v, want %v", got, tt.want)
			}
			if !reader.closed {
				t.Fatal("MBIM reader closed = false, want true")
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
			reader := &fakeMBIMAirplaneModeReader{}
			device := mbimDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			if err := device.SetAirplaneMode(context.Background(), tt.want); err != nil {
				t.Fatalf("SetAirplaneMode() error = %v", err)
			}
			if reader.setState != tt.state {
				t.Fatalf("MBIM set state = %d, want %d", reader.setState, tt.state)
			}
			if !reader.closed {
				t.Fatal("MBIM reader closed = false, want true")
			}
		})
	}
}

func TestDeviceVoLTEStatusMBIM(t *testing.T) {
	tests := []struct {
		name        string
		contexts    []uiccmbim.ProvisionedContext
		maxSessions uint32
		err         error
		capsErr     error
		want        VoLTEStatus
		wantErr     error
	}{
		{
			name: "IMS context supported",
			contexts: []uiccmbim.ProvisionedContext{
				{ContextID: 1, ContextType: uiccmbim.ContextTypeInternet, AccessString: "internet"},
				{ContextID: 2, ContextType: uiccmbim.ContextTypeIMS, AccessString: " IMS "},
			},
			maxSessions: 2,
			want:        VoLTEStatus{Supported: true},
		},
		{
			name:        "IMS context without spare session",
			contexts:    []uiccmbim.ProvisionedContext{{ContextID: 2, ContextType: uiccmbim.ContextTypeIMS, AccessString: "ims"}},
			maxSessions: 1,
		},
		{name: "IMS context missing"},
		{name: "query error", err: errors.New("MBIM unavailable"), wantErr: errors.New("MBIM unavailable")},
		{name: "capability error", contexts: []uiccmbim.ProvisionedContext{{ContextType: uiccmbim.ContextTypeIMS, AccessString: "ims"}}, capsErr: errors.New("caps unavailable"), wantErr: errors.New("caps unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeMBIMNetworkReader{contexts: slices.Clone(tt.contexts), contextsErr: tt.err, maxSessions: tt.maxSessions, capsErr: tt.capsErr}
			device := mbimDeviceWithNetworkReader(reader)

			got, err := device.VoLTEStatus(context.Background())
			if tt.wantErr != nil {
				target := tt.err
				if target == nil {
					target = tt.capsErr
				}
				if err == nil || !errors.Is(err, target) {
					t.Fatalf("VoLTEStatus() error = %v, want wrapped %v", err, target)
				}
				return
			}
			if err != nil {
				t.Fatalf("VoLTEStatus() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("VoLTEStatus() = %+v, want %+v", got, tt.want)
			}
			if !reader.closed {
				t.Fatal("MBIM reader closed = false, want true")
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
			reader := &fakeMBIMNetworkReader{
				registration: uiccmbim.RegistrationStateInfo{RegisterState: tt.registration},
				packet:       tt.packet,
			}
			device := mbimDeviceWithNetworkReader(reader)
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
			device := mbimDeviceWithNetworkReader(&fakeMBIMNetworkReader{contexts: slices.Clone(tt.contexts)})
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

type fakeMBIMAirplaneModeReader struct {
	state    uiccmbim.RadioStateInfo
	setState uiccmbim.RadioSwitchState
	closed   bool
}

type fakeMBIMNetworkReader struct {
	subscriberReady    uiccmbim.SubscriberReadyStatusResponse
	subscriberReadyErr error
	registration       uiccmbim.RegistrationStateInfo
	registrationErr    error
	packet             uiccmbim.PacketServiceInfo
	packetErr          error
	contexts           []uiccmbim.ProvisionedContext
	contextsErr        error
	maxSessions        uint32
	capsErr            error
	closed             bool
}

func (r *fakeMBIMNetworkReader) SubscriberReadyStatus(context.Context) (uiccmbim.SubscriberReadyStatusResponse, error) {
	return r.subscriberReady, r.subscriberReadyErr
}

func (r *fakeMBIMNetworkReader) DeviceCaps(context.Context) (uiccmbim.DeviceCapsInfo, error) {
	return uiccmbim.DeviceCapsInfo{MaxSessions: r.maxSessions}, r.capsErr
}

func (r *fakeMBIMNetworkReader) RegistrationState(context.Context) (uiccmbim.RegistrationStateInfo, error) {
	return r.registration, r.registrationErr
}

func (r *fakeMBIMNetworkReader) PacketService(context.Context) (uiccmbim.PacketServiceInfo, error) {
	return r.packet, r.packetErr
}

func (r *fakeMBIMNetworkReader) ProvisionedContexts(context.Context) ([]uiccmbim.ProvisionedContext, error) {
	return slices.Clone(r.contexts), r.contextsErr
}

func (r *fakeMBIMNetworkReader) Close() error {
	r.closed = true
	return nil
}

func mbimDeviceWithNetworkReader(reader mbimNetworkReader) mbimDevice {
	return mbimDevice{
		openNetwork: func(context.Context) (mbimNetworkReader, error) {
			return reader, nil
		},
	}
}

func (r *fakeMBIMAirplaneModeReader) RadioState(context.Context) (uiccmbim.RadioStateInfo, error) {
	return r.state, nil
}

func (r *fakeMBIMAirplaneModeReader) SetRadioState(_ context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error) {
	r.setState = state
	return uiccmbim.RadioStateInfo{SwRadioState: state}, nil
}

func (r *fakeMBIMAirplaneModeReader) Close() error {
	r.closed = true
	return nil
}

func mbimDeviceWithAirplaneModeReader(t *testing.T, wantDevice string, reader mbimAirplaneModeReader, openErr error) mbimDevice {
	t.Helper()

	return mbimDevice{
		device: wantDevice,
		slot:   1,
		openRadio: func(context.Context) (mbimAirplaneModeReader, error) {
			return reader, openErr
		},
	}
}
