package ims

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/ims-go/wfcsetup"
	"github.com/damonto/sigmo/internal/app/websheet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestEmergencyAddressCallback(t *testing.T) {
	for _, tt := range []struct {
		name      string
		events    []string
		firstErr  bool
		wantCalls int
	}{
		{name: "recheck once after successful completion", events: []string{"pageLoaded", "CloseWebView", "entitlementChanged"}, wantCalls: 1},
		{name: "cancellation preserves previous entitlement", events: []string{"cancelButtonPressed", "entitlementChanged"}},
		{name: "callback can retry after a carrier error", events: []string{"entitlementChanged", "entitlementChanged", "entitlementChanged"}, firstErr: true, wantCalls: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			carrierErr := errors.New("test carrier error")
			complete := emergencyAddressCallback(func(context.Context) error {
				calls++
				if calls == 1 && tt.firstErr {
					return carrierErr
				}
				return nil
			})
			for index, event := range tt.events {
				var wantErr error
				if index == 0 && tt.firstErr {
					wantErr = carrierErr
				}
				if err := complete(t.Context(), websheet.Callback{Event: event}); !errors.Is(err, wantErr) {
					t.Fatalf("callback %q returned %v, want %v", event, err, wantErr)
				}
			}
			if calls != tt.wantCalls {
				t.Fatalf("rechecks = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestEmergencyAddressCallbackSkipsCanceledDelivery(t *testing.T) {
	calls := 0
	complete := emergencyAddressCallback(func(context.Context) error {
		calls++
		return nil
	})
	callback := websheet.Callback{Event: "entitlementChanged"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := complete(ctx, callback); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled delivery error = %v, want %v", err, context.Canceled)
	}
	if calls != 0 {
		t.Fatalf("rechecks after canceled delivery = %d, want 0", calls)
	}
	if err := complete(t.Context(), callback); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("rechecks after redelivery = %d, want 1", calls)
	}
}

func TestEmergencyAddressCallbackRejectsReplacedSIM(t *testing.T) {
	c := &coordinator{}
	modem := &mmodem.Modem{SIM: &mmodem.SIM{Identifier: "replacement"}}
	err := c.recheckEmergencyAddress(t.Context(), modem, "original")
	if !errors.Is(err, websheet.ErrExpired) {
		t.Fatalf("recheckEmergencyAddress() = %v, want %v", err, websheet.ErrExpired)
	}
}

func TestEmergencyAddressUpdateAvailable(t *testing.T) {
	tests := []struct {
		name string
		sim  *mmodem.SIM
		want bool
	}{
		{
			name: "keeps o2 uk hidden",
			sim: &mmodem.SIM{
				Identifier:         "iccid-1",
				IMSI:               "234100123456789",
				OperatorIdentifier: "23410",
			},
		},
		{
			name: "keeps ct excel uk hidden",
			sim: &mmodem.SIM{
				Identifier:         "iccid-1",
				IMSI:               "234100123456789",
				OperatorIdentifier: "23410",
				GID1:               "547275554B3030656E",
			},
		},
		{
			name: "shows telus builtin update",
			sim: &mmodem.SIM{
				Identifier:         "iccid-1",
				IMSI:               "302220123456789",
				OperatorIdentifier: "302220",
				GID1:               "5455",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{})}
			modem := &mmodem.Modem{SIM: tt.sim}
			got := c.EmergencyAddressUpdateAvailable(t.Context(), modem)
			if got != tt.want {
				t.Fatalf("EmergencyAddressUpdateAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartEmergencyAddressUpdateRejectsUnsupportedCarrier(t *testing.T) {
	c := &coordinator{websheets: websheet.New(websheet.Config{})}
	modem := &mmodem.Modem{SIM: &mmodem.SIM{
		Identifier:         "iccid-1",
		IMSI:               "234100123456789",
		OperatorIdentifier: "23410",
		GID1:               "547275554B3030656E",
	}}
	_, err := c.StartEmergencyAddressUpdate(t.Context(), modem, "iccid-1", nil)
	if !errors.Is(err, ErrWebsheetUnavailable) {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v, want %v", err, ErrWebsheetUnavailable)
	}
}

func TestEmergencyAddressUpdateAvailableWithoutWebsheetBroker(t *testing.T) {
	c := &coordinator{}
	modem := &mmodem.Modem{SIM: &mmodem.SIM{
		Identifier:         "iccid-1",
		IMSI:               "302220123456789",
		OperatorIdentifier: "302220",
		GID1:               "5455",
	}}
	if got := c.EmergencyAddressUpdateAvailable(t.Context(), modem); got {
		t.Fatalf("EmergencyAddressUpdateAvailable() = %v, want false", got)
	}
}

func TestEmergencyAddressUpdateSupported(t *testing.T) {
	tests := []struct {
		name    string
		support wfcsetup.E911UpdateSupport
		want    bool
	}{
		{
			name: "builtin carrier websheet",
			support: wfcsetup.E911UpdateSupport{
				Supported: true,
				BuiltIn:   true,
			},
			want: true,
		},
		{
			name: "unsupported carrier",
			support: wfcsetup.E911UpdateSupport{
				Supported: false,
				BuiltIn:   false,
			},
		},
		{
			name: "external credential required",
			support: wfcsetup.E911UpdateSupport{
				Supported:                  true,
				RequiresExternalCredential: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emergencyAddressUpdateSupported(tt.support); got != tt.want {
				t.Fatalf("emergencyAddressUpdateSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}
