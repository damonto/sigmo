//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/pro/websheet"
	"github.com/damonto/vowifi-go/wfcsetup"
)

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
				Imsi:               "234100123456789",
				OperatorIdentifier: "23410",
			},
		},
		{
			name: "keeps ct excel uk hidden",
			sim: &mmodem.SIM{
				Identifier:         "iccid-1",
				Imsi:               "234100123456789",
				OperatorIdentifier: "23410",
				GID1:               "547275554B3030656E",
			},
		},
		{
			name: "shows telus builtin update",
			sim: &mmodem.SIM{
				Identifier:         "iccid-1",
				Imsi:               "302220123456789",
				OperatorIdentifier: "302220",
				GID1:               "5455",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{})}
			modem := &mmodem.Modem{Sim: tt.sim}
			got := c.EmergencyAddressUpdateAvailable(context.Background(), modem)
			if got != tt.want {
				t.Fatalf("EmergencyAddressUpdateAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartEmergencyAddressUpdateRejectsUnsupportedCarrier(t *testing.T) {
	c := &coordinator{websheets: websheet.New(websheet.Config{})}
	modem := &mmodem.Modem{Sim: &mmodem.SIM{
		Identifier:         "iccid-1",
		Imsi:               "234100123456789",
		OperatorIdentifier: "23410",
		GID1:               "547275554B3030656E",
	}}
	_, err := c.StartEmergencyAddressUpdate(context.Background(), modem)
	if !errors.Is(err, ErrWebsheetUnavailable) {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v, want %v", err, ErrWebsheetUnavailable)
	}
}

func TestEmergencyAddressUpdateAvailableWithoutWebsheetBroker(t *testing.T) {
	c := &coordinator{}
	modem := &mmodem.Modem{Sim: &mmodem.SIM{
		Identifier:         "iccid-1",
		Imsi:               "302220123456789",
		OperatorIdentifier: "302220",
		GID1:               "5455",
	}}
	if got := c.EmergencyAddressUpdateAvailable(context.Background(), modem); got {
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
