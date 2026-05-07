package modem

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestIsUnknownObjectError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "dbus value error",
			err:  dbus.Error{Name: dbusErrUnknownObject},
			want: true,
		},
		{
			name: "dbus pointer error",
			err:  &dbus.Error{Name: dbusErrUnknownObject},
			want: true,
		},
		{
			name: "other dbus error",
			err:  dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"},
			want: false,
		},
		{
			name: "unknown object error from message",
			err: dbus.Error{
				Name: "org.freedesktop.DBus.Error.Failed",
				Body: []any{"Object does not exist at path \"/org/freedesktop/ModemManager1/Modem/4\""},
			},
			want: true,
		},
		{
			name: "wrapped non dbus error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnknownObjectError(tt.err); got != tt.want {
				t.Fatalf("isUnknownObjectError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTransientRestartError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unknown object",
			err:  dbus.Error{Name: dbusErrUnknownObject},
			want: true,
		},
		{
			name: "canceled",
			err:  dbus.Error{Name: dbusErrCanceled},
			want: true,
		},
		{
			name: "cancelled message",
			err:  errors.New("Operation was cancelled"),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("permission denied"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientRestartError(tt.err); got != tt.want {
				t.Fatalf("isTransientRestartError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModemRestart(t *testing.T) {
	tests := []struct {
		name    string
		errors  map[string][]error
		wantErr bool
	}{
		{
			name: "ignore unknown object after disable",
			errors: map[string][]error{
				ModemInterface + ".Simple.GetStatus": {nil},
				ModemInterface + ".Enable": {
					nil,
					dbus.Error{Name: dbusErrUnknownObject},
				},
			},
			wantErr: false,
		},
		{
			name: "return unexpected enable error",
			errors: map[string][]error{
				ModemInterface + ".Simple.GetStatus": {nil},
				ModemInterface + ".Enable": {
					nil,
					errors.New("permission denied"),
				},
			},
			wantErr: true,
		},
		{
			name: "ignore unknown object message after enable",
			errors: map[string][]error{
				ModemInterface + ".Simple.GetStatus": {nil},
				ModemInterface + ".Enable": {
					nil,
					dbus.Error{
						Name: "org.freedesktop.DBus.Error.Failed",
						Body: []any{"Object does not exist at path \"/org/freedesktop/ModemManager1/Modem/1\""},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &fakeBusObject{
				path:   "/org/freedesktop/ModemManager1/Modem/1",
				errors: tt.errors,
			}
			modem := &Modem{
				dbusObject:          object,
				objectPath:          object.path,
				EquipmentIdentifier: "354015820228039",
			}

			err := modem.Restart(false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Restart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWaitForModem(t *testing.T) {
	tests := []struct {
		name     string
		current  *Modem
		modems   map[dbus.ObjectPath]*Modem
		wantErr  error
		wantPath dbus.ObjectPath
	}{
		{
			name: "return replacement already present",
			current: &Modem{
				objectPath:          "/org/freedesktop/ModemManager1/Modem/1",
				EquipmentIdentifier: "354015820228039",
			},
			modems: map[dbus.ObjectPath]*Modem{
				"/org/freedesktop/ModemManager1/Modem/2": {
					objectPath:          "/org/freedesktop/ModemManager1/Modem/2",
					EquipmentIdentifier: "354015820228039",
				},
			},
			wantPath: "/org/freedesktop/ModemManager1/Modem/2",
		},
		{
			name:    "reject nil modem",
			wantErr: errModemRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &Manager{
				modems: tt.modems,
			}
			manager.subscribe.Do(func() {})

			modem, err := manager.WaitForModem(context.Background(), tt.current)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("WaitForModem() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("WaitForModem() error = %v", err)
			}
			if modem == nil || modem.objectPath != tt.wantPath {
				t.Fatalf("WaitForModem() path = %v, want %v", modem.objectPath, tt.wantPath)
			}
		})
	}
}

type fakeBusObject struct {
	path   dbus.ObjectPath
	errors map[string][]error
	calls  []string
	args   [][]any
}

func (f *fakeBusObject) Call(method string, _ dbus.Flags, args ...any) *dbus.Call {
	f.calls = append(f.calls, method)
	f.args = append(f.args, append([]any(nil), args...))
	var err error
	if queue := f.errors[method]; len(queue) > 0 {
		err = queue[0]
		f.errors[method] = queue[1:]
	}
	return &dbus.Call{Err: err}
}

func (f *fakeBusObject) CallWithContext(context.Context, string, dbus.Flags, ...any) *dbus.Call {
	panic("unexpected CallWithContext")
}

func (f *fakeBusObject) Go(string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call {
	panic("unexpected Go")
}

func (f *fakeBusObject) GoWithContext(context.Context, string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call {
	panic("unexpected GoWithContext")
}

func (f *fakeBusObject) AddMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call {
	panic("unexpected AddMatchSignal")
}

func (f *fakeBusObject) RemoveMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call {
	panic("unexpected RemoveMatchSignal")
}

func (f *fakeBusObject) GetProperty(string) (dbus.Variant, error) {
	panic("unexpected GetProperty")
}

func (f *fakeBusObject) StoreProperty(string, any) error {
	panic("unexpected StoreProperty")
}

func (f *fakeBusObject) SetProperty(string, any) error {
	panic("unexpected SetProperty")
}

func (f *fakeBusObject) Destination() string {
	return ModemManagerInterface
}

func (f *fakeBusObject) Path() dbus.ObjectPath {
	return f.path
}
