package modem

import (
	"context"
	"strings"
	"testing"

	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

type fakeDeviceControl struct {
	calls       []string
	airplane    bool
	state       mdevice.SIMState
	stateErr    error
	powerErr    error
	activateErr error
	msisdn      string
	msisdnErr   error
	updateErr   error
}

func (d *fakeDeviceControl) MSISDN(context.Context) (string, error) {
	d.calls = append(d.calls, "msisdn")
	return d.msisdn, d.msisdnErr
}

func (d *fakeDeviceControl) UpdateMSISDN(_ context.Context, number string) error {
	d.calls = append(d.calls, "update-msisdn:"+number)
	return d.updateErr
}

func (d *fakeDeviceControl) AirplaneMode(context.Context) (bool, error) {
	d.calls = append(d.calls, "airplane-mode")
	return d.airplane, nil
}

func (d *fakeDeviceControl) SetAirplaneMode(_ context.Context, enabled bool) error {
	d.calls = append(d.calls, fmtBoolEvent("set-airplane-mode", enabled))
	d.airplane = enabled
	return nil
}

func (d *fakeDeviceControl) PowerCycleSIM(context.Context) error {
	d.calls = append(d.calls, "power-cycle")
	return d.powerErr
}

func (d *fakeDeviceControl) ActivateProvisioningIfSIMMissing(context.Context) error {
	d.calls = append(d.calls, "activate-provisioning")
	return d.activateErr
}

func (d *fakeDeviceControl) SIMState(context.Context, mdevice.Target) (mdevice.SIMState, error) {
	d.calls = append(d.calls, "sim-state")
	return d.state, d.stateErr
}

func fakeDeviceOpener(t *testing.T, device deviceControl, openErr error) deviceControlOpener {
	t.Helper()

	return func(mdevice.Config) (deviceControl, error) {
		if openErr != nil {
			return nil, openErr
		}
		return device, nil
	}
}

func countCalls(calls []string, prefix string) int {
	var count int
	for _, call := range calls {
		if strings.HasPrefix(call, prefix) {
			count++
		}
	}
	return count
}
