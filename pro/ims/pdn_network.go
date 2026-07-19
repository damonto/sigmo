//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
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

const (
	imsPolicyRulePriorityBase uint32 = 10_000
	imsPolicyRouteTableBase   uint32 = 20_000
	imsPolicyRouteTableCount  uint32 = 1024
	imsPolicyProtocol                = 200
)

var imsPolicyRoutes = struct {
	sync.Mutex
	reserved map[uint32]struct{}
}{reserved: make(map[uint32]struct{})}

type pdnLinks interface {
	DisableIPv6Autoconfiguration(string) error
	SetUp(string) error
	AddAddress(string, netip.Prefix) error
	AddPointToPointAddress(string, netip.Addr, netip.Addr) error
	DeleteAddress(string, netip.Prefix) error
	DeletePointToPointAddress(string, netip.Addr, netip.Addr) error
	AddDefaultRoute(netlink.DefaultRoute) error
	DeleteDefaultRoute(netlink.DefaultRoute) error
	RouteEntries() ([]netlink.RouteEntry, error)
	FlushDefaultRoutesInTable(uint32) error
	PolicyRules() ([]netlink.PolicyRule, error)
	AddPolicyRule(netlink.PolicyRule) error
	DeletePolicyRule(netlink.PolicyRule) error
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
func (systemPDNLinks) AddDefaultRoute(route netlink.DefaultRoute) error {
	return netlink.AddDefaultRoute(route)
}
func (systemPDNLinks) DeleteDefaultRoute(route netlink.DefaultRoute) error {
	return netlink.DeleteDefaultRoute(route)
}
func (systemPDNLinks) RouteEntries() ([]netlink.RouteEntry, error) {
	return netlink.RouteEntries()
}
func (systemPDNLinks) FlushDefaultRoutesInTable(table uint32) error {
	return netlink.FlushDefaultRoutesInTable(table, imsPolicyProtocol)
}
func (systemPDNLinks) PolicyRules() ([]netlink.PolicyRule, error) {
	return netlink.PolicyRules()
}
func (systemPDNLinks) AddPolicyRule(rule netlink.PolicyRule) error {
	return netlink.AddPolicyRule(rule)
}
func (systemPDNLinks) DeletePolicyRule(rule netlink.PolicyRule) error {
	return netlink.DeletePolicyRule(rule)
}
func (systemPDNLinks) AddVLAN(parent, name string, id uint16) error {
	return netlink.AddVLAN(parent, name, id)
}
func (systemPDNLinks) DeleteLink(name string) error { return netlink.DeleteLink(name) }

type pdnNetworkState struct {
	prefixes    []netip.Prefix
	peers       map[netip.Prefix]netip.Addr
	defaults    []netlink.DefaultRoute
	rules       []netlink.PolicyRule
	policyTable uint32
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

func (n *pdnNetwork) Replace(ctx context.Context, info imsPDNInfo) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.closeLocked(); err != nil {
		return "", fmt.Errorf("clean IMS PDN network: %w", err)
	}
	interfaceName, err := n.sessionInterface(info.SessionID)
	if err != nil {
		return "", err
	}
	n.interfaceName = interfaceName
	state, err := n.configure(ctx, interfaceName, info)
	n.state = state
	if err != nil {
		return "", errors.Join(err, n.closeLocked())
	}
	return n.interfaceName, nil
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
		} else {
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
			if state.peers == nil {
				state.peers = make(map[netip.Prefix]netip.Addr)
			}
			state.peers[prefix] = gateway
		}
		state.prefixes = append(state.prefixes, prefix)
	}
	if err := n.addPolicyRouting(interfaceName, &state); err != nil {
		return state, err
	}
	return state, nil
}

func (n *pdnNetwork) addPolicyRouting(interfaceName string, state *pdnNetworkState) error {
	table, priority, err := reserveIMSPolicyTable(n.links)
	if err != nil {
		return fmt.Errorf("reserve IMS policy routing table: %w", err)
	}
	state.policyTable = table
	if err := n.links.FlushDefaultRoutesInTable(table); err != nil {
		return fmt.Errorf("clean IMS policy routing table %d: %w", table, err)
	}
	for _, prefix := range state.prefixes {
		family := netlink.FamilyIPv6
		if prefix.Addr().Is4() {
			family = netlink.FamilyIPv4
		}
		route := netlink.DefaultRoute{
			Interface: interfaceName,
			Family:    family,
			Table:     table,
			Source:    prefix.Addr(),
			Protocol:  imsPolicyProtocol,
		}
		if err := n.links.AddDefaultRoute(route); err != nil {
			return fmt.Errorf("add IMS default route for %s in table %d: %w", prefix.Addr(), table, err)
		}
		state.defaults = append(state.defaults, route)
	}
	for _, route := range state.defaults {
		rule := netlink.PolicyRule{
			Family:          route.Family,
			Priority:        priority,
			Table:           table,
			OutputInterface: interfaceName,
			Protocol:        imsPolicyProtocol,
		}
		if err := n.links.AddPolicyRule(rule); err != nil {
			return fmt.Errorf("add IMS output-interface rule for %s: %w", interfaceName, err)
		}
		state.rules = append(state.rules, rule)
	}
	return nil
}

func (n *pdnNetwork) closeLocked() error {
	if n.interfaceName == "" {
		return nil
	}
	remaining, err := n.cleanup(n.interfaceName, n.state)
	n.state = remaining
	if n.mbim && len(n.state.prefixes) == 0 && len(n.state.rules) == 0 && len(n.state.defaults) == 0 && n.state.policyTable == 0 {
		if linkErr := n.links.DeleteLink(n.interfaceName); linkErr != nil {
			return errors.Join(err, linkErr)
		}
		n.state = pdnNetworkState{}
	}
	if len(n.state.prefixes) == 0 && len(n.state.defaults) == 0 && len(n.state.rules) == 0 && n.state.policyTable == 0 {
		n.interfaceName = ""
	}
	return err
}

func (n *pdnNetwork) cleanup(interfaceName string, state pdnNetworkState) (pdnNetworkState, error) {
	remaining := clonePDNNetworkState(state)
	var result error
	remaining.rules = remaining.rules[:0]
	for _, rule := range state.rules {
		if err := n.links.DeletePolicyRule(rule); err != nil {
			result = errors.Join(result, err)
			remaining.rules = append(remaining.rules, rule)
		}
	}
	if len(remaining.rules) != 0 {
		return remaining, result
	}

	remaining.defaults = remaining.defaults[:0]
	for _, route := range state.defaults {
		if err := n.links.DeleteDefaultRoute(route); err != nil {
			result = errors.Join(result, err)
			remaining.defaults = append(remaining.defaults, route)
		}
	}
	if len(remaining.defaults) != 0 {
		return remaining, result
	}
	if state.policyTable != 0 {
		if err := n.links.FlushDefaultRoutesInTable(state.policyTable); err != nil {
			result = errors.Join(result, err)
			return remaining, result
		}
		releaseIMSPolicyTable(state.policyTable)
		remaining.policyTable = 0
	}

	remaining.prefixes = remaining.prefixes[:0]
	remaining.peers = make(map[netip.Prefix]netip.Addr, len(state.peers))
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

func clonePDNNetworkState(state pdnNetworkState) pdnNetworkState {
	state.prefixes = slices.Clone(state.prefixes)
	state.peers = maps.Clone(state.peers)
	state.defaults = slices.Clone(state.defaults)
	state.rules = slices.Clone(state.rules)
	return state
}

func reserveIMSPolicyTable(links pdnLinks) (uint32, uint32, error) {
	imsPolicyRoutes.Lock()
	defer imsPolicyRoutes.Unlock()
	rules, err := links.PolicyRules()
	if err != nil {
		return 0, 0, err
	}
	routes, err := links.RouteEntries()
	if err != nil {
		return 0, 0, err
	}
	usedTables := make(map[uint32]struct{}, len(rules)+len(routes))
	usedPriorities := make(map[uint32]struct{}, len(rules))
	for _, route := range routes {
		usedTables[route.Table] = struct{}{}
	}
	for _, rule := range rules {
		usedTables[rule.Table] = struct{}{}
		usedPriorities[rule.Priority] = struct{}{}
	}
	for slot := range imsPolicyRouteTableCount {
		table := imsPolicyRouteTableBase + slot
		priority := imsPolicyRulePriorityBase + slot
		if _, reserved := imsPolicyRoutes.reserved[table]; reserved {
			continue
		}
		if _, used := usedTables[table]; used {
			continue
		}
		if _, used := usedPriorities[priority]; used {
			continue
		}
		imsPolicyRoutes.reserved[table] = struct{}{}
		return table, priority, nil
	}
	return 0, 0, errors.New("IMS policy routing tables are exhausted")
}

func releaseIMSPolicyTable(table uint32) {
	imsPolicyRoutes.Lock()
	delete(imsPolicyRoutes.reserved, table)
	imsPolicyRoutes.Unlock()
}

func cleanupStaleIMSPolicyRouting(links pdnLinks) error {
	imsPolicyRoutes.Lock()
	defer imsPolicyRoutes.Unlock()
	rules, err := links.PolicyRules()
	if err != nil {
		return err
	}
	routes, err := links.RouteEntries()
	if err != nil {
		return err
	}
	tables := make(map[uint32]bool)
	for table := range imsPolicyRoutes.reserved {
		if isIMSPolicyTable(table) {
			tables[table] = true
		}
	}
	for _, route := range routes {
		table, ok := imsPolicyTableForRoute(route)
		if ok {
			tables[table] = true
		}
	}
	var result error
	for _, rule := range rules {
		table, ok := imsPolicyTableForRule(rule)
		if !ok {
			continue
		}
		if _, seen := tables[table]; !seen {
			tables[table] = true
		}
		if err := links.DeletePolicyRule(rule); err != nil {
			result = errors.Join(result, err)
			tables[table] = false
		}
	}
	for table, rulesDeleted := range tables {
		if !rulesDeleted {
			continue
		}
		if err := links.FlushDefaultRoutesInTable(table); err != nil {
			result = errors.Join(result, err)
			continue
		}
		delete(imsPolicyRoutes.reserved, table)
	}
	return result
}

func imsPolicyTableForRule(rule netlink.PolicyRule) (uint32, bool) {
	if rule.Family != netlink.FamilyIPv4 && rule.Family != netlink.FamilyIPv6 {
		return 0, false
	}
	if rule.OutputInterface == "" || rule.Protocol != imsPolicyProtocol || !isIMSPolicyTable(rule.Table) || rule.Priority < imsPolicyRulePriorityBase {
		return 0, false
	}
	tableSlot := rule.Table - imsPolicyRouteTableBase
	prioritySlot := rule.Priority - imsPolicyRulePriorityBase
	if prioritySlot != tableSlot {
		return 0, false
	}
	return rule.Table, true
}

func imsPolicyTableForRoute(route netlink.RouteEntry) (uint32, bool) {
	if !route.Default || route.Protocol != imsPolicyProtocol || !isIMSPolicyTable(route.Table) {
		return 0, false
	}
	return route.Table, true
}

func isIMSPolicyTable(table uint32) bool {
	return table >= imsPolicyRouteTableBase && table-imsPolicyRouteTableBase < imsPolicyRouteTableCount
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
