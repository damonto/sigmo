package device

import (
	"context"
	"testing"

	uiccmbim "github.com/damonto/uicc-go/mbim"
)

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

func TestDeviceToggleAirplaneModeMBIM(t *testing.T) {
	tests := []struct {
		name      string
		state     uiccmbim.RadioSwitchState
		want      bool
		wantState uiccmbim.RadioSwitchState
	}{
		{name: "turn on", state: uiccmbim.RadioSwitchStateOn, want: true, wantState: uiccmbim.RadioSwitchStateOff},
		{name: "turn off", state: uiccmbim.RadioSwitchStateOff, wantState: uiccmbim.RadioSwitchStateOn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeMBIMAirplaneModeReader{
				state: uiccmbim.RadioStateInfo{SwRadioState: tt.state},
			}
			device := mbimDeviceWithAirplaneModeReader(t, "/dev/cdc-wdm0", reader, nil)

			got, err := device.ToggleAirplaneMode(context.Background())
			if err != nil {
				t.Fatalf("ToggleAirplaneMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToggleAirplaneMode() = %v, want %v", got, tt.want)
			}
			if reader.setState != tt.wantState {
				t.Fatalf("MBIM set state = %d, want %d", reader.setState, tt.wantState)
			}
			if !reader.closed {
				t.Fatal("MBIM reader closed = false, want true")
			}
		})
	}
}

type fakeMBIMAirplaneModeReader struct {
	state    uiccmbim.RadioStateInfo
	setState uiccmbim.RadioSwitchState
	setCalls int
	closed   bool
}

func (r *fakeMBIMAirplaneModeReader) RadioState(context.Context) (uiccmbim.RadioStateInfo, error) {
	return r.state, nil
}

func (r *fakeMBIMAirplaneModeReader) SetRadioState(_ context.Context, state uiccmbim.RadioSwitchState) (uiccmbim.RadioStateInfo, error) {
	r.setState = state
	r.setCalls++
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
