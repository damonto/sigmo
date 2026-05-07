package modem

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestMessagingSetDefaultStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		storage SMSStorage
		err     error
		wantErr bool
	}{
		{
			name:    "mobile equipment",
			storage: SMSStorageME,
		},
		{
			name:    "return dbus error",
			storage: SMSStorageME,
			err:     errors.New("permission denied"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				errors: map[string][]error{
					ModemMessagingInterface + ".SetDefaultStorage": {tt.err},
				},
			}
			modem := &Modem{dbusObject: object}

			err := modem.Messaging().SetDefaultStorage(tt.storage)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SetDefaultStorage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(object.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(object.calls))
			}
			if got := object.calls[0]; got != ModemMessagingInterface+".SetDefaultStorage" {
				t.Fatalf("call = %q, want %q", got, ModemMessagingInterface+".SetDefaultStorage")
			}
			if len(object.args) != 1 || len(object.args[0]) != 1 {
				t.Fatalf("args = %#v, want one argument", object.args)
			}
			if got := object.args[0][0]; got != uint32(tt.storage) {
				t.Fatalf("storage argument = %#v, want %d", got, tt.storage)
			}
		})
	}
}

func TestMessagingSupportedStorages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     []uint32
		err     error
		want    []SMSStorage
		wantErr bool
	}{
		{
			name: "read storages",
			raw:  []uint32{uint32(SMSStorageSM), uint32(SMSStorageME), uint32(SMSStorageMT)},
			want: []SMSStorage{SMSStorageSM, SMSStorageME, SMSStorageMT},
		},
		{
			name:    "return dbus error",
			err:     errors.New("permission denied"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				properties: map[string]dbus.Variant{
					ModemMessagingInterface + ".SupportedStorages": dbus.MakeVariant(tt.raw),
				},
				propertyErrors: map[string][]error{
					ModemMessagingInterface + ".SupportedStorages": {tt.err},
				},
			}
			modem := &Modem{dbusObject: object}

			got, err := modem.Messaging().SupportedStorages()
			if (err != nil) != tt.wantErr {
				t.Fatalf("SupportedStorages() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SupportedStorages() = %#v, want %#v", got, tt.want)
			}
			if len(object.propertyCalls) != 1 {
				t.Fatalf("property calls = %d, want 1", len(object.propertyCalls))
			}
			if got := object.propertyCalls[0]; got != ModemMessagingInterface+".SupportedStorages" {
				t.Fatalf("property call = %q, want %q", got, ModemMessagingInterface+".SupportedStorages")
			}
		})
	}
}

func TestMessagingDefaultStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     uint32
		err     error
		want    SMSStorage
		wantErr bool
	}{
		{
			name: "read storage",
			raw:  uint32(SMSStorageME),
			want: SMSStorageME,
		},
		{
			name:    "return dbus error",
			err:     errors.New("permission denied"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				properties: map[string]dbus.Variant{
					ModemMessagingInterface + ".DefaultStorage": dbus.MakeVariant(tt.raw),
				},
				propertyErrors: map[string][]error{
					ModemMessagingInterface + ".DefaultStorage": {tt.err},
				},
			}
			modem := &Modem{dbusObject: object}

			got, err := modem.Messaging().DefaultStorage()
			if (err != nil) != tt.wantErr {
				t.Fatalf("DefaultStorage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("DefaultStorage() = %v, want %v", got, tt.want)
			}
			if len(object.propertyCalls) != 1 {
				t.Fatalf("property calls = %d, want 1", len(object.propertyCalls))
			}
			if got := object.propertyCalls[0]; got != ModemMessagingInterface+".DefaultStorage" {
				t.Fatalf("property call = %q, want %q", got, ModemMessagingInterface+".DefaultStorage")
			}
		})
	}
}

func TestSetDefaultSMSStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		supported     []uint32
		current       SMSStorage
		wantSet       bool
		wantPropCalls []string
	}{
		{
			name:      "set when supported and different",
			supported: []uint32{uint32(SMSStorageSM), uint32(SMSStorageME)},
			current:   SMSStorageSM,
			wantSet:   true,
			wantPropCalls: []string{
				ModemMessagingInterface + ".SupportedStorages",
				ModemMessagingInterface + ".DefaultStorage",
			},
		},
		{
			name:      "skip unsupported storage",
			supported: []uint32{uint32(SMSStorageSM)},
			current:   SMSStorageSM,
			wantPropCalls: []string{
				ModemMessagingInterface + ".SupportedStorages",
			},
		},
		{
			name:      "skip already default storage",
			supported: []uint32{uint32(SMSStorageSM), uint32(SMSStorageME)},
			current:   SMSStorageME,
			wantPropCalls: []string{
				ModemMessagingInterface + ".SupportedStorages",
				ModemMessagingInterface + ".DefaultStorage",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				properties: map[string]dbus.Variant{
					ModemMessagingInterface + ".SupportedStorages": dbus.MakeVariant(tt.supported),
					ModemMessagingInterface + ".DefaultStorage":    dbus.MakeVariant(uint32(tt.current)),
				},
			}
			modem := &Modem{dbusObject: object, EquipmentIdentifier: "354015820228039"}

			setDefaultSMSStorage(context.Background(), modem, SMSStorageME)

			if !slices.Equal(object.propertyCalls, tt.wantPropCalls) {
				t.Fatalf("property calls = %#v, want %#v", object.propertyCalls, tt.wantPropCalls)
			}
			if tt.wantSet {
				if len(object.calls) != 1 {
					t.Fatalf("calls = %d, want 1", len(object.calls))
				}
				if got := object.calls[0]; got != ModemMessagingInterface+".SetDefaultStorage" {
					t.Fatalf("call = %q, want %q", got, ModemMessagingInterface+".SetDefaultStorage")
				}
				if got := object.args[0][0]; got != uint32(SMSStorageME) {
					t.Fatalf("storage argument = %#v, want %d", got, SMSStorageME)
				}
				return
			}
			if len(object.calls) != 0 {
				t.Fatalf("calls = %#v, want none", object.calls)
			}
		})
	}
}
