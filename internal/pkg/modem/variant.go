package modem

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

type variantUnsigned interface {
	~uint32 | ~uint64
}

func variantString(raw map[string]dbus.Variant, key string) string {
	return stringFromVariant(raw[key])
}

func variantUint[T variantUnsigned](raw map[string]dbus.Variant, key string) T {
	return uintFromVariant[T](raw[key])
}

func variantInt32(raw map[string]dbus.Variant, key string) int32 {
	return int32FromVariant(raw[key])
}

func variantObjectPath(raw map[string]dbus.Variant, key string) dbus.ObjectPath {
	return objectPathFromVariant(raw[key])
}

func variantStrings(raw map[string]dbus.Variant, key string) []string {
	return stringsFromVariant(raw[key])
}

func variantObjectPaths(raw map[string]dbus.Variant, key string) []dbus.ObjectPath {
	return objectPathsFromVariant(raw[key])
}

func variantAnySlices(raw map[string]dbus.Variant, key string) [][]any {
	return anySlicesFromVariant(raw[key])
}

func stringFromVariant(variant dbus.Variant) string {
	value, ok := variant.Value().(string)
	if !ok {
		return ""
	}
	return value
}

func boolFromVariant(variant dbus.Variant) bool {
	value, ok := variant.Value().(bool)
	if !ok {
		return false
	}
	return value
}

func uintFromVariant[T variantUnsigned](variant dbus.Variant) T {
	switch value := variant.Value().(type) {
	case uint32:
		return T(value)
	case uint64:
		return T(value)
	case int:
		if value < 0 {
			return 0
		}
		return T(value)
	case int32:
		if value < 0 {
			return 0
		}
		return T(value)
	case int64:
		if value < 0 {
			return 0
		}
		return T(value)
	default:
		return 0
	}
}

func int32FromVariant(variant dbus.Variant) int32 {
	switch value := variant.Value().(type) {
	case int32:
		return value
	case int:
		return int32(value)
	case uint32:
		return int32(value)
	case uint64:
		return int32(value)
	default:
		return 0
	}
}

func objectPathFromVariant(variant dbus.Variant) dbus.ObjectPath {
	value, ok := variant.Value().(dbus.ObjectPath)
	if !ok {
		return ""
	}
	return value
}

func stringsFromVariant(variant dbus.Variant) []string {
	value, ok := variant.Value().([]string)
	if !ok {
		return nil
	}
	return value
}

func objectPathsFromVariant(variant dbus.Variant) []dbus.ObjectPath {
	value, ok := variant.Value().([]dbus.ObjectPath)
	if !ok {
		return nil
	}
	return value
}

func anySliceFromVariant(variant dbus.Variant) []any {
	value, ok := variant.Value().([]any)
	if !ok {
		return nil
	}
	return value
}

func anySlicesFromVariant(variant dbus.Variant) [][]any {
	value, ok := variant.Value().([][]any)
	if !ok {
		return nil
	}
	return value
}

func variantMapFromVariant(variant dbus.Variant) map[string]dbus.Variant {
	value, ok := variant.Value().(map[string]dbus.Variant)
	if !ok {
		return nil
	}
	return value
}

func modePairsFromVariant(variant dbus.Variant) ([]ModemModePair, error) {
	var raw []dbusModePair
	if err := variant.Store(&raw); err == nil {
		pairs := make([]ModemModePair, 0, len(raw))
		for _, pair := range raw {
			pairs = append(pairs, ModemModePair{
				Allowed:   ModemMode(pair.Allowed),
				Preferred: ModemMode(pair.Preferred),
			})
		}
		return pairs, nil
	}

	values, ok := variant.Value().([][]any)
	if !ok {
		return nil, fmt.Errorf("parse mode pairs: unexpected variant type %T", variant.Value())
	}
	pairs := make([]ModemModePair, 0, len(values))
	for _, value := range values {
		pairs = append(pairs, modePairFromValues(value))
	}
	return pairs, nil
}

func modePairFromVariant(variant dbus.Variant) (ModemModePair, error) {
	var raw dbusModePair
	if err := variant.Store(&raw); err == nil {
		return ModemModePair{
			Allowed:   ModemMode(raw.Allowed),
			Preferred: ModemMode(raw.Preferred),
		}, nil
	}
	values := anySliceFromVariant(variant)
	if len(values) == 0 {
		return ModemModePair{}, fmt.Errorf("parse mode pair: unexpected variant type %T", variant.Value())
	}
	return modePairFromValues(values), nil
}

func modePairFromValues(values []any) ModemModePair {
	if len(values) < 2 {
		return ModemModePair{}
	}
	return ModemModePair{
		Allowed:   ModemMode(uintFromAny(values[0])),
		Preferred: ModemMode(uintFromAny(values[1])),
	}
}

func bandsFromVariant(variant dbus.Variant) ([]ModemBand, error) {
	var raw []uint32
	if err := variant.Store(&raw); err == nil {
		bands := make([]ModemBand, 0, len(raw))
		for _, band := range raw {
			bands = append(bands, ModemBand(band))
		}
		return bands, nil
	}

	values, ok := variant.Value().([]any)
	if !ok {
		return nil, fmt.Errorf("parse bands: unexpected variant type %T", variant.Value())
	}
	bands := make([]ModemBand, 0, len(values))
	for _, value := range values {
		bands = append(bands, ModemBand(uintFromAny(value)))
	}
	return bands, nil
}

func uintFromAny(value any) uint32 {
	switch v := value.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case int32:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint32(v)
	default:
		return 0
	}
}
