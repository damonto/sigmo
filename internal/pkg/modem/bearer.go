package modem

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	ModemSimpleInterface = ModemInterface + ".Simple"
	BearerInterface      = ModemManagerInterface + ".Bearer"
)

const (
	bearerIPFamilyIPv4V6 uint32 = 1 << 2
)

type BearerIPMethod uint32

const (
	BearerIPMethodUnknown BearerIPMethod = iota
	BearerIPMethodPPP
	BearerIPMethodStatic
	BearerIPMethodDHCP
)

func (m BearerIPMethod) String() string {
	switch m {
	case BearerIPMethodPPP:
		return "ppp"
	case BearerIPMethodStatic:
		return "static"
	case BearerIPMethodDHCP:
		return "dhcp"
	default:
		return "unknown"
	}
}

type Bearer struct {
	objectPath dbus.ObjectPath
	dbusObject dbus.BusObject
}

type BearerIPConfig struct {
	Method  BearerIPMethod
	Address string
	Prefix  uint32
	Gateway string
	DNS     []string
	MTU     uint32
}

func (c BearerIPConfig) StaticAddress() bool {
	return c.Method == BearerIPMethodStatic && strings.TrimSpace(c.Address) != ""
}

type BearerStats struct {
	RXBytes  uint64
	TXBytes  uint64
	Duration uint32
}

func (m *Modem) ConnectBearer(apn string) (*Bearer, error) {
	properties := map[string]dbus.Variant{
		"ip-type": dbus.MakeVariant(bearerIPFamilyIPv4V6),
	}
	if apn = strings.TrimSpace(apn); apn != "" {
		properties["apn"] = dbus.MakeVariant(apn)
	}

	var path dbus.ObjectPath
	if err := m.dbusObject.Call(ModemSimpleInterface+".Connect", 0, properties).Store(&path); err != nil {
		return nil, err
	}
	return m.Bearer(path)
}

func (m *Modem) DisconnectBearer(path dbus.ObjectPath) error {
	return m.dbusObject.Call(ModemSimpleInterface+".Disconnect", 0, path).Err
}

func (m *Modem) Bearers() ([]*Bearer, error) {
	variant, err := m.dbusObject.GetProperty(ModemInterface + ".Bearers")
	if err != nil {
		return nil, err
	}
	paths := objectPathsFromVariant(variant)
	bearers := make([]*Bearer, 0, len(paths))
	for _, path := range paths {
		bearer, err := m.Bearer(path)
		if err != nil {
			return nil, err
		}
		bearers = append(bearers, bearer)
	}
	return bearers, nil
}

func (m *Modem) Bearer(path dbus.ObjectPath) (*Bearer, error) {
	if path == "" || path == "/" {
		return nil, errors.New("bearer path is required")
	}
	if m.mmgr != nil && m.mmgr.dbusConn != nil {
		return &Bearer{
			objectPath: path,
			dbusObject: m.mmgr.dbusConn.Object(ModemManagerInterface, path),
		}, nil
	}
	object, err := systemBusObject(path)
	if err != nil {
		return nil, err
	}
	return &Bearer{objectPath: path, dbusObject: object}, nil
}

func (b *Bearer) Path() dbus.ObjectPath {
	return b.objectPath
}

func (b *Bearer) Interface() (string, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + ".Interface")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (b *Bearer) Connected() (bool, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + ".Connected")
	if err != nil {
		return false, err
	}
	return boolFromVariant(variant), nil
}

func (b *Bearer) IP4Config() (BearerIPConfig, error) {
	return b.ipConfig("Ip4Config")
}

func (b *Bearer) IP6Config() (BearerIPConfig, error) {
	return b.ipConfig("Ip6Config")
}

func (b *Bearer) Stats() (BearerStats, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + ".Stats")
	if err != nil {
		return BearerStats{}, err
	}
	return bearerStatsFromVariants(variantMapFromVariant(variant)), nil
}

func (b *Bearer) APN() (string, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + ".Properties")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(variantString(variantMapFromVariant(variant), "apn")), nil
}

func (b *Bearer) Disconnect() error {
	return b.dbusObject.Call(BearerInterface+".Disconnect", 0).Err
}

func (b *Bearer) ipConfig(name string) (BearerIPConfig, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + "." + name)
	if err != nil {
		return BearerIPConfig{}, err
	}
	return bearerIPConfigFromVariants(variantMapFromVariant(variant)), nil
}

func bearerIPConfigFromVariants(raw map[string]dbus.Variant) BearerIPConfig {
	cfg := BearerIPConfig{
		Method:  BearerIPMethod(variantUint[uint32](raw, "method")),
		Prefix:  variantUint[uint32](raw, "prefix"),
		Address: strings.TrimSpace(variantString(raw, "address")),
		Gateway: strings.TrimSpace(variantString(raw, "gateway")),
		MTU:     variantUint[uint32](raw, "mtu"),
	}
	for i := 1; i <= 3; i++ {
		dns := strings.TrimSpace(variantString(raw, fmt.Sprintf("dns%d", i)))
		if dns == "" || slices.Contains(cfg.DNS, dns) {
			continue
		}
		cfg.DNS = append(cfg.DNS, dns)
	}
	return cfg
}

func bearerStatsFromVariants(raw map[string]dbus.Variant) BearerStats {
	return BearerStats{
		RXBytes:  variantUint[uint64](raw, "rx-bytes"),
		TXBytes:  variantUint[uint64](raw, "tx-bytes"),
		Duration: variantUint[uint32](raw, "duration"),
	}
}
