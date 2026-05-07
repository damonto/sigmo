package modem

import (
	"errors"
	"testing"
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
