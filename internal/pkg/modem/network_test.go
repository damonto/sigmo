package modem

import (
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestModemModeLabels(t *testing.T) {
	tests := []struct {
		name string
		mode ModemMode
		want string
	}{
		{name: "any", mode: ModemModeAny, want: "Any"},
		{name: "single", mode: ModemMode4G, want: "4G"},
		{name: "combined", mode: ModemMode3G | ModemMode4G | ModemMode5G, want: "3G + 4G + 5G"},
		{name: "unknown", mode: 1 << 20, want: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.Label(); got != tt.want {
				t.Fatalf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModemBands(t *testing.T) {
	tests := []struct {
		name string
		band ModemBand
		want string
	}{
		{name: "any", band: ModemBandAny, want: "Any"},
		{name: "gsm", band: 1, want: "GSM EGSM 900"},
		{name: "utran enum value", band: 5, want: "UMTS band 1"},
		{name: "lte", band: 71, want: "LTE B41"},
		{name: "eutran 85", band: 115, want: "LTE B85"},
		{name: "cdma skipped enum value", band: 134, want: "CDMA BC5"},
		{name: "nr", band: 378, want: "NR n78"},
		{name: "fallback", band: 999, want: "Band 999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.band.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModemNetworkWrappers(t *testing.T) {
	object := &fakeBusObject{
		path: "/org/freedesktop/ModemManager1/Modem/1",
		properties: map[string]dbus.Variant{
			ModemInterface + ".SupportedModes": dbus.MakeVariant([]dbusModePair{
				{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)},
				{Allowed: uint32(ModemMode5G | ModemMode4G), Preferred: uint32(ModemMode5G)},
			}),
			ModemInterface + ".CurrentModes": dbus.MakeVariant(dbusModePair{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)}),
			ModemInterface + ".SupportedBands": dbus.MakeVariant([]uint32{
				uint32(ModemBandAny),
				71,
				378,
			}),
			ModemInterface + ".CurrentBands": dbus.MakeVariant([]uint32{71}),
		},
	}
	modem := &Modem{dbusObject: object, objectPath: object.path}

	modes, err := modem.SupportedModes()
	if err != nil {
		t.Fatalf("SupportedModes() error = %v", err)
	}
	wantModes := []ModemModePair{
		{Allowed: ModemMode4G, Preferred: ModemModeNone},
		{Allowed: ModemMode5G | ModemMode4G, Preferred: ModemMode5G},
	}
	if !slices.Equal(modes, wantModes) {
		t.Fatalf("SupportedModes() = %#v, want %#v", modes, wantModes)
	}

	current, err := modem.CurrentModes()
	if err != nil {
		t.Fatalf("CurrentModes() error = %v", err)
	}
	if current != wantModes[0] {
		t.Fatalf("CurrentModes() = %#v, want %#v", current, wantModes[0])
	}

	if err := modem.SetCurrentModes(wantModes[1]); err != nil {
		t.Fatalf("SetCurrentModes() error = %v", err)
	}
	if got := object.calls[len(object.calls)-1]; got != ModemInterface+".SetCurrentModes" {
		t.Fatalf("Call() = %q, want SetCurrentModes", got)
	}

	bands, err := modem.SupportedBands()
	if err != nil {
		t.Fatalf("SupportedBands() error = %v", err)
	}
	wantBands := []ModemBand{ModemBandAny, 71, 378}
	if !slices.Equal(bands, wantBands) {
		t.Fatalf("SupportedBands() = %#v, want %#v", bands, wantBands)
	}

	if err := modem.SetCurrentBands([]ModemBand{71, 378}); err != nil {
		t.Fatalf("SetCurrentBands() error = %v", err)
	}
	if got := object.args[len(object.args)-1][0]; !slices.Equal(got.([]uint32), []uint32{71, 378}) {
		t.Fatalf("SetCurrentBands() args = %#v", got)
	}

}

func TestModemNetworkWrapperParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		property string
		call     func(*Modem) error
	}{
		{
			name:     "supported modes",
			property: ModemInterface + ".SupportedModes",
			call: func(modem *Modem) error {
				_, err := modem.SupportedModes()
				return err
			},
		},
		{
			name:     "current modes",
			property: ModemInterface + ".CurrentModes",
			call: func(modem *Modem) error {
				_, err := modem.CurrentModes()
				return err
			},
		},
		{
			name:     "supported bands",
			property: ModemInterface + ".SupportedBands",
			call: func(modem *Modem) error {
				_, err := modem.SupportedBands()
				return err
			},
		},
		{
			name:     "current bands",
			property: ModemInterface + ".CurrentBands",
			call: func(modem *Modem) error {
				_, err := modem.CurrentBands()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &fakeBusObject{
				path: "/org/freedesktop/ModemManager1/Modem/1",
				properties: map[string]dbus.Variant{
					tt.property: dbus.MakeVariant("invalid"),
				},
			}
			modem := &Modem{dbusObject: object, objectPath: object.path}

			if err := tt.call(modem); err == nil {
				t.Fatal("parse error = nil, want error")
			}
		})
	}
}
