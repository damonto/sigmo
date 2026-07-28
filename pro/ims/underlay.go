//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	imsgo "github.com/damonto/ims-go"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type modemUnderlay struct {
	internet internetRestorer
	modem    *mmodem.Modem
}

var _ imsgo.Underlay = (*modemUnderlay)(nil)

func (c *coordinator) wifiCallingUnderlay(ctx context.Context, modem *mmodem.Modem, settings WiFiCallingSettings) (imsgo.Underlay, error) {
	settings, err := ResolveWiFiCallingSettings(modem, settings)
	if err != nil {
		return nil, err
	}
	switch settings.Underlay.Mode {
	case UnderlayModeSystem:
		return nil, nil
	case UnderlayModeSelf:
		return newModemUnderlay(ctx, c.internet, modem)
	case UnderlayModeModem:
		if c.registry == nil {
			return nil, fmt.Errorf("%w: modem registry is unavailable", ErrWiFiCallingUnderlayUnavailable)
		}
		target, err := c.registry.Find(ctx, settings.Underlay.ModemID)
		if err != nil {
			return nil, errors.Join(ErrWiFiCallingUnderlayUnavailable, fmt.Errorf("find uplink modem %s: %w", settings.Underlay.ModemID, err))
		}
		return newModemUnderlay(ctx, c.internet, target)
	default:
		return nil, fmt.Errorf("%w: unsupported mode %q", ErrInvalidWiFiCallingUnderlay, settings.Underlay.Mode)
	}
}

func newModemUnderlay(ctx context.Context, internet internetRestorer, modem *mmodem.Modem) (*modemUnderlay, error) {
	underlay := &modemUnderlay{internet: internet, modem: modem}
	_, _, err := underlay.current(ctx)
	if err != nil {
		return nil, err
	}
	return underlay, nil
}

func (u *modemUnderlay) LocalIP() netip.Addr {
	// The selected bearer can reconnect with a different address family. Let
	// LookupNetIP filter candidates against the current connection instead of
	// pinning ims-go to the family observed when this underlay was created.
	return netip.Addr{}
}

func (u *modemUnderlay) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	connection, underlay, err := u.current(ctx)
	if err != nil {
		return nil, err
	}
	addresses, err := underlay.LookupNetIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	filtered := filterAddressesForConnection(connection, addresses)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("resolve %s: no address matches uplink IP families", host)
	}
	return filtered, nil
}

func filterAddressesForConnection(connection *pinternet.Connection, addresses []netip.Addr) []netip.Addr {
	if connection == nil {
		return addresses
	}
	hasIPv4 := len(connection.IPv4Addresses) > 0
	hasIPv6 := len(connection.IPv6Addresses) > 0
	if !hasIPv4 && !hasIPv6 {
		return addresses
	}
	filtered := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if (address.Is4() && hasIPv4) || (address.Is6() && hasIPv6) {
			filtered = append(filtered, address)
		}
	}
	return filtered
}

func (u *modemUnderlay) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	_, underlay, err := u.current(ctx)
	if err != nil {
		return nil, err
	}
	return underlay.DialContext(ctx, network, address)
}

func (u *modemUnderlay) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	_, underlay, err := u.current(ctx)
	if err != nil {
		return nil, err
	}
	return underlay.ListenPacket(ctx, network, address)
}

func (u *modemUnderlay) current(ctx context.Context) (*pinternet.Connection, *pinternet.BoundUnderlay, error) {
	if u.internet == nil || u.modem == nil {
		return nil, nil, fmt.Errorf("%w: Internet connector or modem is unavailable", ErrWiFiCallingUnderlayUnavailable)
	}
	connection, err := u.internet.Current(ctx, u.modem)
	if err != nil {
		return nil, nil, errors.Join(ErrWiFiCallingUnderlayUnavailable, fmt.Errorf("read modem %s Internet: %w", u.modem.EquipmentIdentifier, err))
	}
	if connection == nil || connection.Status != pinternet.StatusConnected || strings.TrimSpace(connection.InterfaceName) == "" {
		return nil, nil, fmt.Errorf("%w: modem %s Internet is not connected", ErrWiFiCallingUnderlayUnavailable, u.modem.EquipmentIdentifier)
	}
	underlay, err := pinternet.NewBoundUnderlay(connection.InterfaceName, connection.DNS)
	if err != nil {
		return nil, nil, fmt.Errorf("create modem %s underlay: %w", u.modem.EquipmentIdentifier, err)
	}
	return connection, underlay, nil
}

func wifiCallingHTTPClient(underlay imsgo.Underlay) *http.Client {
	if underlay == nil {
		return nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	transport.DialContext = underlay.DialContext
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}
