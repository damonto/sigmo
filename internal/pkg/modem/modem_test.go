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

			err := modem.Restart(context.Background(), false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Restart() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModemRestartReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	modem := &Modem{
		dbusObject:          &fakeBusObject{path: "/org/freedesktop/ModemManager1/Modem/1"},
		objectPath:          "/org/freedesktop/ModemManager1/Modem/1",
		EquipmentIdentifier: "354015820228039",
	}

	if err := modem.Restart(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("Restart() error = %v, want context.Canceled", err)
	}
}

func TestEnableDisabledModem(t *testing.T) {
	tests := []struct {
		name    string
		state   ModemState
		err     error
		wantRun bool
		wantErr bool
	}{
		{
			name:    "enable disabled modem",
			state:   ModemStateDisabled,
			wantRun: true,
		},
		{
			name:  "skip enabled modem",
			state: ModemStateEnabled,
		},
		{
			name:    "return enable error",
			state:   ModemStateDisabled,
			err:     errors.New("permission denied"),
			wantRun: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &fakeBusObject{
				errors: map[string][]error{
					ModemInterface + ".Enable": {tt.err},
				},
			}
			modem := &Modem{
				dbusObject: object,
				State:      tt.state,
			}

			err := enableDisabledModem(context.Background(), modem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("enableDisabledModem() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := len(object.calls) > 0; got != tt.wantRun {
				t.Fatalf("enable called = %v, want %v", got, tt.wantRun)
			}
		})
	}
}

func TestModemDeleteBearer(t *testing.T) {
	object := &fakeBusObject{path: "/org/freedesktop/ModemManager1/Modem/1"}
	modem := &Modem{dbusObject: object}

	if err := modem.DeleteBearer(context.Background(), "/org/freedesktop/ModemManager1/Bearer/7"); err != nil {
		t.Fatalf("DeleteBearer() error = %v", err)
	}
	if got, want := object.calls, []string{ModemInterface + ".DeleteBearer"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if len(object.args) != 1 || len(object.args[0]) != 1 || object.args[0][0] != dbus.ObjectPath("/org/freedesktop/ModemManager1/Bearer/7") {
		t.Fatalf("args = %#v, want bearer path", object.args)
	}
}

func TestBearerDBusProperties(t *testing.T) {
	properties, err := bearerDBusProperties(BearerProperties{
		APN:         " wap.vodafone.co.uk ",
		IPType:      "ipv4",
		Username:    " wap ",
		Password:    "*wap",
		AllowedAuth: "pap",
	})
	if err != nil {
		t.Fatalf("bearerDBusProperties() error = %v", err)
	}
	if got := properties["apn"].Value(); got != "wap.vodafone.co.uk" {
		t.Fatalf("apn = %#v, want trimmed APN", got)
	}
	if got := properties["user"].Value(); got != "wap" {
		t.Fatalf("user = %#v, want trimmed username", got)
	}
	if got := properties["password"].Value(); got != "*wap" {
		t.Fatalf("password = %#v, want password", got)
	}
	if got := properties["ip-type"].Value(); got != bearerIPFamilyIPv4 {
		t.Fatalf("ip-type = %#v, want IPv4", got)
	}
	if got := properties["allowed-auth"].Value(); got != bearerAllowedAuthPAP {
		t.Fatalf("allowed-auth = %#v, want PAP", got)
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
		action     func(*Registry) error
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
			action: func(*Registry) error {
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
			action: func(*Registry) error {
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
			action: func(*Registry) error {
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
			action: func(registry *Registry) error {
				publishModemEvent(t, registry, ModemEvent{
					Type:  ModemEventRemoved,
					Modem: current,
					Path:  current.objectPath,
				})
				publishModemEvent(t, registry, ModemEvent{
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
			action: func(registry *Registry) error {
				publishModemEvent(t, registry, ModemEvent{
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
			action: func(registry *Registry) error {
				go func() {
					registry.mu.Lock()
					defer registry.mu.Unlock()
					registry.modems[replacement.objectPath] = replacement
				}()
				return nil
			},
			ctxTimeout: time.Second,
			wantPath:   replacement.objectPath,
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
			registry := &Registry{
				modems: tt.modems,
			}
			registry.subscribed = true

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			modem, err := registry.WaitForModemAfter(ctx, tt.current, func() error {
				if tt.action == nil {
					return nil
				}
				return tt.action(registry)
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

func publishModemEvent(t *testing.T, registry *Registry, event ModemEvent) {
	t.Helper()
	registry.mu.RLock()
	subscribers := append([]subscription(nil), registry.subs...)
	registry.mu.RUnlock()
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

func TestRegistryDeleteAndUpdate(t *testing.T) {
	tests := []struct {
		name  string
		start map[dbus.ObjectPath]*Modem
		modem *Modem
		want  map[dbus.ObjectPath]string
	}{
		{
			name: "replace duplicate equipment identifier",
			start: map[dbus.ObjectPath]*Modem{
				"/old": {objectPath: "/old", EquipmentIdentifier: "imei-1"},
			},
			modem: &Modem{objectPath: "/new", EquipmentIdentifier: "imei-1"},
			want: map[dbus.ObjectPath]string{
				"/new": "imei-1",
			},
		},
		{
			name: "empty equipment identifier does not delete unrelated empty identifiers",
			start: map[dbus.ObjectPath]*Modem{
				"/old": {objectPath: "/old"},
			},
			modem: &Modem{objectPath: "/new"},
			want: map[dbus.ObjectPath]string{
				"/old": "",
				"/new": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &Registry{modems: tt.start}
			registry.deleteAndUpdate(tt.modem)
			if len(registry.modems) != len(tt.want) {
				t.Fatalf("modems length = %d, want %d", len(registry.modems), len(tt.want))
			}
			for path, wantID := range tt.want {
				modem, ok := registry.modems[path]
				if !ok {
					t.Fatalf("modem %s missing", path)
				}
				if modem.EquipmentIdentifier != wantID {
					t.Fatalf("modem %s EquipmentIdentifier = %q, want %q", path, modem.EquipmentIdentifier, wantID)
				}
			}
		})
	}
}

func TestSignalParsing(t *testing.T) {
	tests := []struct {
		name         string
		signal       *dbus.Signal
		wantPath     dbus.ObjectPath
		wantReceived bool
		wantOK       bool
	}{
		{
			name: "message received",
			signal: &dbus.Signal{
				Body: []any{dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1"), true},
			},
			wantPath:     "/org/freedesktop/ModemManager1/SMS/1",
			wantReceived: true,
			wantOK:       true,
		},
		{
			name: "message stored but not received",
			signal: &dbus.Signal{
				Body: []any{dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1"), false},
			},
			wantPath: "/org/freedesktop/ModemManager1/SMS/1",
			wantOK:   true,
		},
		{
			name: "short body",
			signal: &dbus.Signal{
				Body: []any{dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1")},
			},
		},
		{
			name: "wrong path type",
			signal: &dbus.Signal{
				Body: []any{"not-a-path", true},
			},
		},
		{
			name: "wrong received type",
			signal: &dbus.Signal{
				Body: []any{dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1"), "true"},
			},
		},
		{
			name: "nil signal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotReceived, gotOK := receivedMessageSignal(tt.signal)
			if gotOK != tt.wantOK {
				t.Fatalf("receivedMessageSignal() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotPath != tt.wantPath {
				t.Fatalf("receivedMessageSignal() path = %v, want %v", gotPath, tt.wantPath)
			}
			if gotReceived != tt.wantReceived {
				t.Fatalf("receivedMessageSignal() received = %v, want %v", gotReceived, tt.wantReceived)
			}
		})
	}
}

func TestDevicePath(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "  "},
		{name: "device name", raw: "ttyUSB0", want: "/dev/ttyUSB0"},
		{name: "absolute path", raw: "/dev/cdc-wdm0", want: "/dev/cdc-wdm0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := devicePath(tt.raw); got != tt.want {
				t.Fatalf("devicePath() = %q, want %q", got, tt.want)
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
	if method == dbusPropertiesGet {
		if len(args) != 2 {
			return &dbus.Call{Err: fmt.Errorf("property get args = %#v", args)}
		}
		iface, ok := args[0].(string)
		if !ok {
			return &dbus.Call{Err: fmt.Errorf("property get interface = %#v", args[0])}
		}
		name, ok := args[1].(string)
		if !ok {
			return &dbus.Call{Err: fmt.Errorf("property get name = %#v", args[1])}
		}
		return f.property(iface + "." + name)
	}
	f.calls = append(f.calls, method)
	f.args = append(f.args, append([]any(nil), args...))
	var err error
	if queue := f.errors[method]; len(queue) > 0 {
		err = queue[0]
		f.errors[method] = queue[1:]
	}
	return &dbus.Call{Err: err, Body: f.outputs[method]}
}

func (f *fakeBusObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
	if err := ctx.Err(); err != nil {
		return &dbus.Call{Err: err}
	}
	return f.Call(method, flags, args...)
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
	call := f.property(name)
	if call.Err != nil {
		return dbus.Variant{}, call.Err
	}
	return call.Body[0].(dbus.Variant), nil
}

func (f *fakeBusObject) property(name string) *dbus.Call {
	f.propertyCalls = append(f.propertyCalls, name)
	if queue := f.propertyErrors[name]; len(queue) > 0 {
		err := queue[0]
		f.propertyErrors[name] = queue[1:]
		if err != nil {
			return &dbus.Call{Err: err}
		}
	}
	variant, ok := f.properties[name]
	if !ok {
		return &dbus.Call{Err: fmt.Errorf("missing property %s", name)}
	}
	return &dbus.Call{Body: []any{variant}}
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
