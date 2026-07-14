//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

var imsInterfacePollInterval = time.Second
var imsInterfaceByName = net.InterfaceByName

type pdnLinks interface {
	DisableIPv6Autoconfiguration(string) error
	SetUp(string) error
	AddAddress(string, netip.Prefix) error
	AddPointToPointAddress(string, netip.Addr, netip.Addr) error
	DeleteAddress(string, netip.Prefix) error
	DeletePointToPointAddress(string, netip.Addr, netip.Addr) error
	AddHostRoute(string, netip.Addr) error
	DeleteHostRoute(string, netip.Addr) error
	AddVLAN(string, string, uint16) error
	DeleteLink(string) error
}

type systemPDNLinks struct{}

func (systemPDNLinks) DisableIPv6Autoconfiguration(name string) error {
	return netlink.DisableIPv6Autoconfiguration(name)
}
func (systemPDNLinks) SetUp(name string) error { return netlink.SetUp(name) }
func (systemPDNLinks) AddAddress(name string, prefix netip.Prefix) error {
	return netlink.AddAddress(name, prefix)
}
func (systemPDNLinks) AddPointToPointAddress(name string, local, peer netip.Addr) error {
	return netlink.AddPointToPointAddress(name, local, peer)
}
func (systemPDNLinks) DeleteAddress(name string, prefix netip.Prefix) error {
	return netlink.DeleteAddress(name, prefix)
}
func (systemPDNLinks) DeletePointToPointAddress(name string, local, peer netip.Addr) error {
	return netlink.DeletePointToPointAddress(name, local, peer)
}
func (systemPDNLinks) AddHostRoute(name string, address netip.Addr) error {
	return netlink.AddHostRoute(name, address)
}
func (systemPDNLinks) DeleteHostRoute(name string, address netip.Addr) error {
	return netlink.DeleteHostRoute(name, address)
}
func (systemPDNLinks) AddVLAN(parent, name string, id uint16) error {
	return netlink.AddVLAN(parent, name, id)
}
func (systemPDNLinks) DeleteLink(name string) error { return netlink.DeleteLink(name) }

type pdnNetworkState struct {
	prefixes []netip.Prefix
	peers    map[netip.Prefix]netip.Addr
	routes   []netip.Addr
}

type imsPDNInfo = imsgo.IMSPDNNetworkInfo

type pdnNetwork struct {
	parent        string
	mbim          bool
	links         pdnLinks
	mu            sync.Mutex
	interfaceName string
	state         pdnNetworkState
}

func newPDNNetwork(parent string, mbim bool) *pdnNetwork {
	return &pdnNetwork{parent: parent, mbim: mbim, links: systemPDNLinks{}}
}

func (n *pdnNetwork) Replace(ctx context.Context, info imsPDNInfo) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.closeLocked(); err != nil {
		return fmt.Errorf("clean IMS PDN network: %w", err)
	}
	interfaceName, err := n.sessionInterface(info.SessionID)
	if err != nil {
		return err
	}
	n.interfaceName = interfaceName
	state, err := n.configure(ctx, interfaceName, info)
	n.state = state
	if err != nil {
		return errors.Join(err, n.closeLocked())
	}
	return nil
}

func (n *pdnNetwork) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closeLocked()
}

func (n *pdnNetwork) sessionInterface(sessionID uint32) (string, error) {
	if !n.mbim {
		return n.parent, nil
	}
	if sessionID == 0 || sessionID > 4094 {
		return "", fmt.Errorf("create MBIM IMS session interface: session ID %d is outside VLAN range", sessionID)
	}
	name, err := mbimSessionInterfaceName(n.parent, sessionID)
	if err != nil {
		return "", err
	}
	if err := n.links.AddVLAN(n.parent, name, uint16(sessionID)); err != nil {
		if !errors.Is(err, syscall.EEXIST) {
			return "", fmt.Errorf("create MBIM IMS session interface: %w", err)
		}
		if err := n.links.DeleteLink(name); err != nil {
			return "", fmt.Errorf("replace stale MBIM IMS session interface: %w", err)
		}
		if err := n.links.AddVLAN(n.parent, name, uint16(sessionID)); err != nil {
			return "", fmt.Errorf("recreate MBIM IMS session interface: %w", err)
		}
	}
	return name, nil
}

func (n *pdnNetwork) configure(ctx context.Context, interfaceName string, info imsPDNInfo) (pdnNetworkState, error) {
	if err := waitForIMSInterface(ctx, interfaceName, func(name string) error {
		if n.mbim {
			if err := n.links.DisableIPv6Autoconfiguration(name); err != nil {
				return err
			}
		}
		return n.links.SetUp(name)
	}); err != nil {
		return pdnNetworkState{}, fmt.Errorf("set IMS interface up: %w", err)
	}
	state := pdnNetworkState{}
	addresses := []struct {
		local   net.IP
		gateway net.IP
	}{
		{local: info.LocalIPv4, gateway: info.IPv4Gateway},
		{local: info.LocalIPv6, gateway: info.IPv6Gateway},
	}
	for _, address := range addresses {
		prefix, ok := imsPDNPrefix(address.local)
		if !ok {
			continue
		}
		if n.mbim {
			if err := n.links.AddAddress(interfaceName, prefix); err != nil {
				return state, fmt.Errorf("add IMS address %s: %w", prefix, err)
			}
			state.prefixes = append(state.prefixes, prefix)
			continue
		}
		// Raw-IP QMI links are point-to-point and do not answer ARP or IPv6
		// neighbor discovery. Marking the WDS gateway as the peer lets the
		// kernel transmit directly instead of waiting for an unreachable neighbor.
		gateway, ok := imsPDNAddress(address.gateway)
		if !ok || gateway.BitLen() != prefix.Addr().BitLen() {
			return state, fmt.Errorf("add IMS address %s: point-to-point gateway is unavailable", prefix)
		}
		if err := n.links.AddPointToPointAddress(interfaceName, prefix.Addr(), gateway); err != nil {
			return state, fmt.Errorf("add IMS address %s: %w", prefix, err)
		}
		state.prefixes = append(state.prefixes, prefix)
		if state.peers == nil {
			state.peers = make(map[netip.Prefix]netip.Addr)
		}
		state.peers[prefix] = gateway
	}
	for _, ip := range info.PCSCFIPs {
		address, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		address = address.Unmap()
		if err := n.links.AddHostRoute(interfaceName, address); err != nil {
			return state, fmt.Errorf("add P-CSCF route %s: %w", address, err)
		}
		state.routes = append(state.routes, address)
	}
	return state, nil
}

func (n *pdnNetwork) closeLocked() error {
	if n.interfaceName == "" {
		return nil
	}
	remaining, err := n.cleanup(n.interfaceName, n.state)
	n.state = remaining
	if n.mbim {
		if linkErr := n.links.DeleteLink(n.interfaceName); linkErr != nil {
			return errors.Join(err, linkErr)
		}
		n.state = pdnNetworkState{}
	}
	if len(n.state.prefixes) == 0 && len(n.state.routes) == 0 {
		n.interfaceName = ""
	}
	return err
}

func (n *pdnNetwork) cleanup(interfaceName string, state pdnNetworkState) (pdnNetworkState, error) {
	remaining := pdnNetworkState{
		prefixes: make([]netip.Prefix, 0, len(state.prefixes)),
		peers:    make(map[netip.Prefix]netip.Addr, len(state.peers)),
		routes:   make([]netip.Addr, 0, len(state.routes)),
	}
	var result error
	for _, route := range state.routes {
		if err := n.links.DeleteHostRoute(interfaceName, route); err != nil {
			result = errors.Join(result, err)
			remaining.routes = append(remaining.routes, route)
		}
	}
	for _, prefix := range state.prefixes {
		peer, pointToPoint := state.peers[prefix]
		var err error
		if pointToPoint {
			err = n.links.DeletePointToPointAddress(interfaceName, prefix.Addr(), peer)
		} else {
			err = n.links.DeleteAddress(interfaceName, prefix)
		}
		if err != nil {
			result = errors.Join(result, err)
			remaining.prefixes = append(remaining.prefixes, prefix)
			if pointToPoint {
				remaining.peers[prefix] = peer
			}
		}
	}
	return remaining, result
}

func mbimSessionInterfaceName(parent string, sessionID uint32) (string, error) {
	ifi, err := imsInterfaceByName(parent)
	if err != nil {
		return "", fmt.Errorf("find MBIM parent interface: %w", err)
	}
	return fmt.Sprintf("mbim%ds%d", ifi.Index, sessionID), nil
}

func waitForIMSInterface(ctx context.Context, interfaceName string, setUp func(string) error) error {
	ticker := time.NewTicker(imsInterfacePollInterval)
	defer ticker.Stop()
	for {
		err := setUp(interfaceName)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.ENODEV) && !errors.Is(err, syscall.ENXIO) && !errors.Is(err, syscall.ENOENT) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for interface %s: %w", interfaceName, ctx.Err())
		case <-ticker.C:
		}
	}
}

func imsPDNPrefix(ip net.IP) (netip.Prefix, bool) {
	address, ok := imsPDNAddress(ip)
	if !ok {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, address.BitLen()), true
}

func imsPDNAddress(ip net.IP) (netip.Addr, bool) {
	address, ok := netip.AddrFromSlice(ip)
	return address.Unmap(), ok
}

func voLTEInterfaceName(modem *mmodem.Modem) (string, error) {
	if modem == nil {
		return "", errors.New("modem is required")
	}
	for _, port := range modem.Ports {
		if port.PortType == mmodem.ModemPortTypeNet && strings.TrimSpace(port.Device) != "" {
			return filepath.Base(strings.TrimSpace(port.Device)), nil
		}
	}
	return "", errors.New("VoLTE network interface is unavailable")
}
