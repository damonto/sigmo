package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

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
		{
			name: "nil",
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
	withWaitForModemRefreshInterval(t, time.Microsecond)

	current := &Modem{
		objectPath:          "/org/freedesktop/ModemManager1/Modem/1",
		EquipmentIdentifier: "354015820228039",
	}
	replacement := &Modem{
		objectPath:          "/org/freedesktop/ModemManager1/Modem/2",
		EquipmentIdentifier: current.EquipmentIdentifier,
	}
	samePathReplacement := &Modem{
		objectPath:          current.objectPath,
		EquipmentIdentifier: current.EquipmentIdentifier,
	}
	transientActionErr := errors.New("Object does not exist at path \"/org/freedesktop/ModemManager1/Modem/1\"")

	tests := []struct {
		name       string
		current    *Modem
		modems     map[dbus.ObjectPath]*Modem
		action     func(*Manager) error
		ctxTimeout time.Duration
		wantErr    error
		wantPath   dbus.ObjectPath
	}{
		{
			name:    "return replacement already present",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				replacement.objectPath: replacement,
			},
			wantPath: replacement.objectPath,
		},
		{
			name:    "same path replacement without reload evidence times out",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				samePathReplacement.objectPath: samePathReplacement,
			},
			ctxTimeout: time.Millisecond,
			wantErr:    context.DeadlineExceeded,
		},
		{
			name:    "return unmarked action error",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				replacement.objectPath: replacement,
			},
			action: func(*Manager) error {
				return transientActionErr
			},
			wantErr: transientActionErr,
		},
		{
			name:    "wait after marked reload action error",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				replacement.objectPath: replacement,
			},
			action: func(*Manager) error {
				return ReloadStarted(transientActionErr)
			},
			wantPath: replacement.objectPath,
		},
		{
			name:    "wait after marked reload action error returns same path replacement",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				samePathReplacement.objectPath: samePathReplacement,
			},
			action: func(*Manager) error {
				return ReloadStarted(transientActionErr)
			},
			wantPath: samePathReplacement.objectPath,
		},
		{
			name:    "event removed then added during action",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				current.objectPath: current,
			},
			action: func(manager *Manager) error {
				publishModemEvent(t, manager, ModemEvent{
					Type:  ModemEventRemoved,
					Modem: current,
					Path:  current.objectPath,
				})
				publishModemEvent(t, manager, ModemEvent{
					Type:  ModemEventAdded,
					Modem: replacement,
					Path:  replacement.objectPath,
				})
				return nil
			},
			wantPath: replacement.objectPath,
		},
		{
			name:    "ignore duplicate added event without reload evidence",
			current: current,
			modems: map[dbus.ObjectPath]*Modem{
				current.objectPath: current,
			},
			action: func(manager *Manager) error {
				publishModemEvent(t, manager, ModemEvent{
					Type:  ModemEventAdded,
					Modem: current,
					Path:  current.objectPath,
				})
				return nil
			},
			ctxTimeout: time.Millisecond,
			wantErr:    context.DeadlineExceeded,
		},
		{
			name: "empty equipment identifier does not match replacement",
			current: &Modem{
				objectPath: "/org/freedesktop/ModemManager1/Modem/1",
			},
			modems: map[dbus.ObjectPath]*Modem{
				"/org/freedesktop/ModemManager1/Modem/2": {
					objectPath: "/org/freedesktop/ModemManager1/Modem/2",
				},
			},
			ctxTimeout: time.Millisecond,
			wantErr:    context.DeadlineExceeded,
		},
		{
			name:    "poll until modem reappears after not found window",
			current: current,
			modems:  map[dbus.ObjectPath]*Modem{},
			action: func(manager *Manager) error {
				go func() {
					time.Sleep(100 * time.Microsecond)
					manager.mu.Lock()
					defer manager.mu.Unlock()
					manager.modems[replacement.objectPath] = replacement
				}()
				return nil
			},
			wantPath: replacement.objectPath,
		},
		{
			name:       "timeout while modem remains unavailable",
			current:    current,
			modems:     map[dbus.ObjectPath]*Modem{},
			ctxTimeout: time.Millisecond,
			wantErr:    context.DeadlineExceeded,
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

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			modem, err := manager.WaitForModemAfter(ctx, tt.current, func() error {
				if tt.action == nil {
					return nil
				}
				return tt.action(manager)
			})
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

func withWaitForModemRefreshInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	original := waitForModemRefreshInterval
	waitForModemRefreshInterval = interval
	t.Cleanup(func() {
		waitForModemRefreshInterval = original
	})
}

func publishModemEvent(t *testing.T, manager *Manager, event ModemEvent) {
	t.Helper()
	manager.mu.RLock()
	subscribers := append([]subscription(nil), manager.subs...)
	manager.mu.RUnlock()
	for _, subscriber := range subscribers {
		if err := subscriber.fn(event); err != nil {
			t.Fatalf("publish modem event: %v", err)
		}
	}
}

func TestSIMSlotPaths(t *testing.T) {
	tests := []struct {
		name           string
		data           map[string]dbus.Variant
		primarySIMPath dbus.ObjectPath
		want           []dbus.ObjectPath
	}{
		{
			name:           "fallback to primary SIM when slots missing",
			data:           map[string]dbus.Variant{},
			primarySIMPath: "/org/freedesktop/ModemManager1/SIM/1",
			want:           []dbus.ObjectPath{"/org/freedesktop/ModemManager1/SIM/1"},
		},
		{
			name: "use real slots when available",
			data: map[string]dbus.Variant{
				"SimSlots": dbus.MakeVariant([]dbus.ObjectPath{
					"/org/freedesktop/ModemManager1/SIM/2",
					"/org/freedesktop/ModemManager1/SIM/3",
				}),
			},
			primarySIMPath: "/org/freedesktop/ModemManager1/SIM/1",
			want: []dbus.ObjectPath{
				"/org/freedesktop/ModemManager1/SIM/2",
				"/org/freedesktop/ModemManager1/SIM/3",
			},
		},
		{
			name: "filter empty slot path",
			data: map[string]dbus.Variant{
				"SimSlots": dbus.MakeVariant([]dbus.ObjectPath{
					"/",
					"/org/freedesktop/ModemManager1/SIM/2",
				}),
			},
			primarySIMPath: "/org/freedesktop/ModemManager1/SIM/1",
			want:           []dbus.ObjectPath{"/org/freedesktop/ModemManager1/SIM/2"},
		},
		{
			name:           "keep empty when primary SIM path missing",
			data:           map[string]dbus.Variant{},
			primarySIMPath: "",
			want:           nil,
		},
		{
			name:           "keep empty when primary SIM path is root",
			data:           map[string]dbus.Variant{},
			primarySIMPath: "/",
			want:           nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := simSlotPaths(tt.data, tt.primarySIMPath); !slices.Equal(got, tt.want) {
				t.Fatalf("simSlotPaths() = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakeBusObject struct {
	path           dbus.ObjectPath
	errors         map[string][]error
	outputs        map[string][]any
	properties     map[string]dbus.Variant
	propertyErrors map[string][]error
	calls          []string
	propertyCalls  []string
	args           [][]any
}

func (f *fakeBusObject) Call(method string, _ dbus.Flags, args ...any) *dbus.Call {
	f.calls = append(f.calls, method)
	f.args = append(f.args, append([]any(nil), args...))
	var err error
	if queue := f.errors[method]; len(queue) > 0 {
		err = queue[0]
		f.errors[method] = queue[1:]
	}
	return &dbus.Call{Err: err, Body: f.outputs[method]}
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

func (f *fakeBusObject) GetProperty(name string) (dbus.Variant, error) {
	f.propertyCalls = append(f.propertyCalls, name)
	if queue := f.propertyErrors[name]; len(queue) > 0 {
		err := queue[0]
		f.propertyErrors[name] = queue[1:]
		if err != nil {
			return dbus.Variant{}, err
		}
	}
	variant, ok := f.properties[name]
	if !ok {
		return dbus.Variant{}, fmt.Errorf("missing property %s", name)
	}
	return variant, nil
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
