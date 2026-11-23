package internet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	resolveService = "org.freedesktop.resolve1"
	resolvePath    = "/org/freedesktop/resolve1"
	resolveManager = resolveService + ".Manager"
	nmService      = "org.freedesktop.NetworkManager"
	nmPath         = "/org/freedesktop/NetworkManager"
)

type dnsDomain struct {
	Name      string
	RouteOnly bool
}

type linkDNS struct {
	servers       []netip.Addr
	domains       []dnsDomain
	customServers bool
}

type systemDNSResolver interface {
	check(context.Context, string) error
	configuration(context.Context) (map[int32]linkDNS, error)
	setServers(context.Context, int32, []netip.Addr) error
	setDomains(context.Context, int32, []dnsDomain) error
}

type resolvedDNS struct{}

func (resolvedDNS) check(ctx context.Context, name string) error {
	conf, err := os.ReadFile("/etc/resolv.conf")
	if err != nil || !usesResolvedStub(string(conf)) {
		return fmt.Errorf("%w: configure the systemd-resolved stub in /etc/resolv.conf", errSystemDNSUnavailable)
	}
	err = withSystemBus(ctx, func(ctx context.Context, bus *dbus.Conn) error {
		manager := bus.Object(resolveService, resolvePath)
		var props map[string]dbus.Variant
		if err := manager.CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", dbus.FlagNoAutoStart, resolveManager).Store(&props); err != nil {
			return err
		}
		// Older resolved versions do not expose these properties. The local stub
		// configuration and the successful manager call still apply there.
		if mode, ok := props["ResolvConfMode"]; ok && mode.Value() != "stub" && mode.Value() != "static" && mode.Value() != "foreign" {
			return errors.New("the host does not use the resolved stub")
		}
		if listener, ok := props["DNSStubListener"]; ok && listener.Value() == "no" {
			return errors.New("the resolved stub listener is disabled")
		}
		return checkNetworkManagerLink(ctx, bus, name)
	})
	if err != nil && !errors.Is(err, errSystemDNSConflict) {
		return fmt.Errorf("%w: %w", errSystemDNSUnavailable, err)
	}
	return err
}

func usesResolvedStub(conf string) bool {
	found := false
	for line := range strings.Lines(conf) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if fields[1] != "127.0.0.53" && fields[1] != "127.0.0.54" {
			return false
		}
		found = true
	}
	return found
}

func checkNetworkManagerLink(ctx context.Context, bus *dbus.Conn, name string) error {
	manager := bus.Object(nmService, nmPath)
	var device dbus.ObjectPath
	err := manager.CallWithContext(ctx, nmService+".GetDeviceByIpIface", dbus.FlagNoAutoStart, name).Store(&device)
	if isDBusError(err, "org.freedesktop.DBus.Error.NameHasNoOwner", "org.freedesktop.DBus.Error.ServiceUnknown", nmService+".UnknownDevice") {
		return nil
	}
	if err != nil {
		return err
	}
	var active dbus.ObjectPath
	if err := bus.Object(nmService, device).StoreProperty(nmService+".Device.ActiveConnection", &active); err != nil {
		return err
	}
	if active == "/" {
		return nil
	}
	var flags uint32
	if err := bus.Object(nmService, active).StoreProperty(nmService+".Connection.Active.StateFlags", &flags); err != nil {
		return err
	}
	const externalActivation = 0x80
	if flags&externalActivation == 0 {
		return fmt.Errorf("%w: NetworkManager manages %s", errSystemDNSConflict, name)
	}
	return nil
}

type resolvedServer struct {
	Family  int32
	Address []byte
}

type resolvedLinkServer struct {
	Index   int32
	Family  int32
	Address []byte
}

type resolvedLinkServerEx struct {
	Index      int32
	Family     int32
	Address    []byte
	Port       uint16
	ServerName string
}

type resolvedLinkDomain struct {
	Index     int32
	Domain    string
	RouteOnly bool
}

func (resolvedDNS) configuration(ctx context.Context) (map[int32]linkDNS, error) {
	var props map[string]dbus.Variant
	err := withSystemBus(ctx, func(ctx context.Context, bus *dbus.Conn) error {
		return bus.Object(resolveService, resolvePath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", dbus.FlagNoAutoStart, resolveManager).Store(&props)
	})
	if err != nil {
		return nil, fmt.Errorf("read system DNS: %w", err)
	}
	return decodeResolvedDNS(props)
}

func decodeResolvedDNS(props map[string]dbus.Variant) (map[int32]linkDNS, error) {
	links := make(map[int32]linkDNS)
	addServer := func(index int32, address []byte, custom bool) {
		if addr, ok := netip.AddrFromSlice(address); ok {
			link := links[index]
			link.servers = append(link.servers, addr)
			link.customServers = link.customServers || custom
			links[index] = link
		}
	}
	if property, ok := props["DNSEx"]; ok {
		var servers []resolvedLinkServerEx
		if err := property.Store(&servers); err != nil {
			return nil, fmt.Errorf("decode resolved DNSEx: %w", err)
		}
		for _, server := range servers {
			addServer(server.Index, server.Address, server.Port != 0 || server.ServerName != "")
		}
	} else {
		var servers []resolvedLinkServer
		if err := props["DNS"].Store(&servers); err != nil {
			return nil, fmt.Errorf("decode resolved DNS: %w", err)
		}
		for _, server := range servers {
			addServer(server.Index, server.Address, false)
		}
	}
	var domains []resolvedLinkDomain
	if err := props["Domains"].Store(&domains); err != nil {
		return nil, fmt.Errorf("decode resolved Domains: %w", err)
	}
	for _, domain := range domains {
		link := links[domain.Index]
		link.domains = append(link.domains, dnsDomain{Name: domain.Domain, RouteOnly: domain.RouteOnly})
		links[domain.Index] = link
	}
	return links, nil
}

func (resolvedDNS) setServers(ctx context.Context, index int32, servers []netip.Addr) error {
	values := make([]resolvedServer, 0, len(servers))
	for _, server := range servers {
		family := int32(unix.AF_INET6)
		if server.Is4() {
			family = unix.AF_INET
		}
		values = append(values, resolvedServer{Family: family, Address: server.AsSlice()})
	}
	return callResolved(ctx, "SetLinkDNS", index, values)
}

func (resolvedDNS) setDomains(ctx context.Context, index int32, domains []dnsDomain) error {
	return callResolved(ctx, "SetLinkDomains", index, domains)
}

func callResolved(ctx context.Context, method string, args ...any) error {
	return withSystemBus(ctx, func(ctx context.Context, bus *dbus.Conn) error {
		return bus.Object(resolveService, resolvePath).CallWithContext(ctx, resolveManager+"."+method, dbus.FlagNoAutoStart, args...).Err
	})
}

func withSystemBus(ctx context.Context, fn func(context.Context, *dbus.Conn) error) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	bus, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer bus.Close()
	return fn(ctx, bus)
}

func isDBusError(err error, names ...string) bool {
	var busErr dbus.Error
	if !errors.As(err, &busErr) {
		return false
	}
	return slices.Contains(names, busErr.Name)
}
