package modem

import (
	"context"
	"testing"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

type fakeDeviceControl struct {
	calls       []string
	state       wwan.SIMState
	stateErr    error
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

func (d *fakeDeviceControl) ActivateProvisioningIfSIMMissing(context.Context) error {
	d.calls = append(d.calls, "activate-provisioning")
	return d.activateErr
}

func (d *fakeDeviceControl) SIMState(context.Context, wwan.Target) (wwan.SIMState, error) {
	d.calls = append(d.calls, "sim-state")
	return d.state, d.stateErr
}

func fakeDeviceOpener(t *testing.T, device deviceControl, openErr error) deviceControlOpener {
	t.Helper()

	return func(wwan.Config) (deviceControl, error) {
		if openErr != nil {
			return nil, openErr
		}
		return device, nil
	}
}
