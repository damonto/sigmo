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
	bearerIPFamilyIPv4   uint32 = 1 << 0
	bearerIPFamilyIPv6   uint32 = 1 << 1
	bearerIPFamilyIPv4V6 uint32 = 1 << 2
)

const (
	bearerAllowedAuthNone     uint32 = 1 << 0
	bearerAllowedAuthPAP      uint32 = 1 << 1
	bearerAllowedAuthCHAP     uint32 = 1 << 2
	bearerAllowedAuthMSCHAP   uint32 = 1 << 3
	bearerAllowedAuthMSCHAPV2 uint32 = 1 << 4
	bearerAllowedAuthEAP      uint32 = 1 << 5
)

var (
	ErrUnsupportedBearerAuth   = errors.New("bearer authentication method is unsupported")
	ErrUnsupportedBearerIPType = errors.New("bearer IP type is unsupported")
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

type BearerProperties struct {
	APN         string
	IPType      string
	Username    string
	Password    string
	AllowedAuth string
}

func (m *Modem) ConnectBearer(prefs BearerProperties) (*Bearer, error) {
	properties, err := bearerDBusProperties(prefs)
	if err != nil {
		return nil, err
	}

	var path dbus.ObjectPath
	if err := m.dbusObject.Call(ModemSimpleInterface+".Connect", 0, properties).Store(&path); err != nil {
		return nil, err
	}
	return m.Bearer(path)
}

func bearerDBusProperties(prefs BearerProperties) (map[string]dbus.Variant, error) {
	ipType, err := BearerIPFamily(prefs.IPType)
	if err != nil {
		return nil, err
	}
	properties := map[string]dbus.Variant{
		"ip-type": dbus.MakeVariant(ipType),
	}
	if apn := strings.TrimSpace(prefs.APN); apn != "" {
		properties["apn"] = dbus.MakeVariant(apn)
	}
	if username := strings.TrimSpace(prefs.Username); username != "" {
		properties["user"] = dbus.MakeVariant(username)
	}
	if prefs.Password != "" {
		properties["password"] = dbus.MakeVariant(prefs.Password)
	}
	if auth := strings.TrimSpace(prefs.AllowedAuth); auth != "" {
		allowedAuth, err := BearerAllowedAuth(auth)
		if err != nil {
			return nil, err
		}
		properties["allowed-auth"] = dbus.MakeVariant(allowedAuth)
	}
	return properties, nil
}

func (m *Modem) DisconnectBearer(path dbus.ObjectPath) error {
	return m.dbusObject.Call(ModemSimpleInterface+".Disconnect", 0, path).Err
}

func (m *Modem) DeleteBearer(path dbus.ObjectPath) error {
	return m.dbusObject.Call(ModemInterface+".DeleteBearer", 0, path).Err
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
	properties, err := b.Properties()
	if err != nil {
		return "", err
	}
	return properties.APN, nil
}

func (b *Bearer) Properties() (BearerProperties, error) {
	variant, err := b.dbusObject.GetProperty(BearerInterface + ".Properties")
	if err != nil {
		return BearerProperties{}, err
	}
	return bearerPropertiesFromVariants(variantMapFromVariant(variant)), nil
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

func bearerPropertiesFromVariants(raw map[string]dbus.Variant) BearerProperties {
	return BearerProperties{
		APN:         strings.TrimSpace(variantString(raw, "apn")),
		IPType:      bearerIPFamilyString(variantUint[uint32](raw, "ip-type")),
		Username:    strings.TrimSpace(variantString(raw, "user")),
		Password:    variantString(raw, "password"),
		AllowedAuth: bearerAllowedAuthString(variantUint[uint32](raw, "allowed-auth")),
	}
}

func bearerStatsFromVariants(raw map[string]dbus.Variant) BearerStats {
	return BearerStats{
		RXBytes:  variantUint[uint64](raw, "rx-bytes"),
		TXBytes:  variantUint[uint64](raw, "tx-bytes"),
		Duration: variantUint[uint32](raw, "duration"),
	}
}

func BearerIPFamily(ipType string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(ipType)) {
	case "", "ipv4v6":
		return bearerIPFamilyIPv4V6, nil
	case "ipv4":
		return bearerIPFamilyIPv4, nil
	case "ipv6":
		return bearerIPFamilyIPv6, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerIPType, ipType)
	}
}

func bearerIPFamilyString(ipType uint32) string {
	switch ipType {
	case bearerIPFamilyIPv4:
		return "ipv4"
	case bearerIPFamilyIPv6:
		return "ipv6"
	case bearerIPFamilyIPv4V6:
		return "ipv4v6"
	default:
		return ""
	}
}

func BearerAllowedAuth(auth string) (uint32, error) {
	auth = strings.ToLower(strings.TrimSpace(auth))
	if auth == "" {
		return 0, nil
	}

	var result uint32
	for _, part := range strings.FieldsFunc(auth, func(r rune) bool {
		return r == '|' || r == ',' || r == ' '
	}) {
		switch strings.TrimSpace(part) {
		case "":
			continue
		case "none":
			result |= bearerAllowedAuthNone
		case "pap":
			result |= bearerAllowedAuthPAP
		case "chap":
			result |= bearerAllowedAuthCHAP
		case "mschap":
			result |= bearerAllowedAuthMSCHAP
		case "mschapv2":
			result |= bearerAllowedAuthMSCHAPV2
		case "eap":
			result |= bearerAllowedAuthEAP
		default:
			return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerAuth, part)
		}
	}
	if result&bearerAllowedAuthNone != 0 && result != bearerAllowedAuthNone {
		return 0, fmt.Errorf("%w: none cannot be combined with other methods", ErrUnsupportedBearerAuth)
	}
	return result, nil
}

func bearerAllowedAuthString(auth uint32) string {
	if auth == 0 {
		return ""
	}
	parts := make([]string, 0, 6)
	for _, item := range []struct {
		mask uint32
		name string
	}{
		{bearerAllowedAuthNone, "none"},
		{bearerAllowedAuthPAP, "pap"},
		{bearerAllowedAuthCHAP, "chap"},
		{bearerAllowedAuthMSCHAP, "mschap"},
		{bearerAllowedAuthMSCHAPV2, "mschapv2"},
		{bearerAllowedAuthEAP, "eap"},
	} {
		if auth&item.mask != 0 {
			parts = append(parts, item.name)
		}
	}
	return strings.Join(parts, "|")
}
