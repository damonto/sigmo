package modem

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestCreateModemKeepsSIMReferenceWhenPropertiesFail(t *testing.T) {
	registry := &Registry{
		dbusObject: &fakeBusObject{path: ModemManagerObjectPath},
	}
	data := map[string]dbus.Variant{
		"EquipmentIdentifier": dbus.MakeVariant("860588043408833"),
		"Model":               dbus.MakeVariant("RM520N"),
		"State":               dbus.MakeVariant(int32(ModemStateLocked)),
		"UnlockRequired":      dbus.MakeVariant(uint32(ModemLockSimPin)),
		"Sim":                 dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/1")),
	}

	got, err := registry.createModem(context.Background(), "/org/freedesktop/ModemManager1/Modem/1", data)
	if err != nil {
		t.Fatalf("createModem() error = %v", err)
	}
	if got.Sim == nil {
		t.Fatal("SIM = nil, want path reference")
	}
	if got.Sim.Path != "/org/freedesktop/ModemManager1/SIM/1" {
		t.Fatalf("SIM path = %q, want primary SIM path", got.Sim.Path)
	}
}

func TestCreateModemReadsDeviceMSISDN(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		readErr error
		want    string
	}{
		{name: "device number", number: "+15551234567", want: "+15551234567"},
		{name: "read error is non-fatal", readErr: errors.New("read failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &fakeDeviceControl{msisdn: tt.number, msisdnErr: tt.readErr}
			registry := &Registry{openDevice: fakeDeviceOpener(t, device, nil)}
			data := map[string]dbus.Variant{
				"EquipmentIdentifier": dbus.MakeVariant("imei-1"),
				"PrimaryPort":         dbus.MakeVariant("cdc-wdm0"),
				"PrimarySimSlot":      dbus.MakeVariant(uint32(1)),
				"OwnNumbers":          dbus.MakeVariant([]string{"stale-number"}),
				"Ports":               dbus.MakeVariant([][]any{{"cdc-wdm0", uint32(ModemPortTypeQmi)}}),
			}
			got, err := registry.createModem(context.Background(), "/org/freedesktop/ModemManager1/Modem/1", data)
			if err != nil {
				t.Fatalf("createModem() error = %v", err)
			}
			if got.Number != tt.want {
				t.Fatalf("Number = %q, want %q", got.Number, tt.want)
			}
		})
	}
}
