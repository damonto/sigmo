package modem

import (
	"bytes"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestVariantUint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    map[string]dbus.Variant
		want32 uint32
		want64 uint64
	}{
		{
			name: "uint32 value",
			raw: map[string]dbus.Variant{
				"value": dbus.MakeVariant(uint32(42)),
			},
			want32: 42,
			want64: 42,
		},
		{
			name: "uint64 value",
			raw: map[string]dbus.Variant{
				"value": dbus.MakeVariant(uint64(2048)),
			},
			want32: 2048,
			want64: 2048,
		},
		{
			name: "negative value",
			raw: map[string]dbus.Variant{
				"value": dbus.MakeVariant(int32(-1)),
			},
			want32: 0,
			want64: 0,
		},
		{
			name:   "missing value",
			raw:    map[string]dbus.Variant{},
			want32: 0,
			want64: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := variantUint[uint32](tt.raw, "value"); got != tt.want32 {
				t.Fatalf("variantUint[uint32]() = %d, want %d", got, tt.want32)
			}
			if got := variantUint[uint64](tt.raw, "value"); got != tt.want64 {
				t.Fatalf("variantUint[uint64]() = %d, want %d", got, tt.want64)
			}
		})
	}
}

func TestVariantBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]dbus.Variant
		want []byte
	}{
		{
			name: "byte array",
			raw: map[string]dbus.Variant{
				"value": dbus.MakeVariant([]byte{0xA0, 0x01}),
			},
			want: []byte{0xA0, 0x01},
		},
		{
			name: "missing value",
			raw:  map[string]dbus.Variant{},
		},
		{
			name: "wrong type",
			raw: map[string]dbus.Variant{
				"value": dbus.MakeVariant("A001"),
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := variantBytes(tt.raw, "value"); !bytes.Equal(got, tt.want) {
				t.Fatalf("variantBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}
