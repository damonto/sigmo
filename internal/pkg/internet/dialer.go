package internet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const fallbackDNSServer = "1.1.1.1:53"

// BoundUnderlay opens DNS, stream, and packet sockets through one Linux
// network interface without changing the process or system default route.
type BoundUnderlay struct {
	interfaceName string
	dnsServers    []string
}

func NewBoundUnderlay(interfaceName string, dnsServers []string) (*BoundUnderlay, error) {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return nil, errors.New("create bound underlay: interface name is empty")
	}
	return &BoundUnderlay{
		interfaceName: interfaceName,
		dnsServers:    effectiveDNSServers(dnsServers),
	}, nil
}

func (u *BoundUnderlay) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return u.resolver().LookupNetIP(ctx, network, host)
}

func (u *BoundUnderlay) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Resolver: u.resolver(),
		Control:  bindToDeviceControl(u.interfaceName),
	}
	return dialer.DialContext(ctx, network, address)
}

func (u *BoundUnderlay) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	config := net.ListenConfig{Control: bindToDeviceControl(u.interfaceName)}
	return config.ListenPacket(ctx, network, address)
}

func (u *BoundUnderlay) resolver() *net.Resolver {
	dialer := &net.Dialer{Control: bindToDeviceControl(u.interfaceName)}
	return newDNSResolver(u.dnsServers, dialer.DialContext)
}

func newDNSResolver(servers []string, dialContext func(context.Context, string, string) (net.Conn, error)) *net.Resolver {
	var next atomic.Uint64
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			server := servers[(next.Add(1)-1)%uint64(len(servers))]
			return dialContext(ctx, dnsNetworkForServer(network, server), server)
		},
	}
}

func normalizeDNSServers(servers []string) []string {
	result := make([]string, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if addr, err := netip.ParseAddr(server); err == nil {
			server = net.JoinHostPort(addr.String(), "53")
		} else if _, _, err := net.SplitHostPort(server); err != nil {
			server = net.JoinHostPort(server, "53")
		}
		if _, ok := seen[server]; ok {
			continue
		}
		seen[server] = struct{}{}
		result = append(result, server)
	}
	return result
}

func effectiveDNSServers(servers []string) []string {
	servers = normalizeDNSServers(servers)
	if len(servers) == 0 {
		return []string{fallbackDNSServer}
	}
	return servers
}

func dnsNetworkForServer(network, server string) string {
	family := "4"
	host, _, err := net.SplitHostPort(server)
	if err == nil {
		if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
			family = "6"
		}
	}
	if strings.HasPrefix(network, "tcp") {
		return "tcp" + family
	}
	return "udp" + family
}

func boundTransportWithTimeout(interfaceName string, dnsServers []string, timeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:       nil,
		DialContext: boundDialerWithTimeout(interfaceName, dnsServers, timeout).DialContext,
	}
}

func boundDialerWithTimeout(interfaceName string, dnsServers []string, timeout time.Duration) *net.Dialer {
	dialer := rawBoundDialerWithTimeout(interfaceName, timeout)
	dialer.Resolver = boundResolverWithTimeout(interfaceName, dnsServers, timeout)
	return dialer
}

func boundResolverWithTimeout(interfaceName string, dnsServers []string, timeout time.Duration) *net.Resolver {
	dialer := rawBoundDialerWithTimeout(interfaceName, timeout)
	return newDNSResolver(effectiveDNSServers(dnsServers), dialer.DialContext)
}

func rawBoundDialerWithTimeout(interfaceName string, timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: bindToDeviceControl(interfaceName),
	}
}

func bindToDeviceControl(interfaceName string) func(string, string, syscall.RawConn) error {
	return func(_ string, _ string, connection syscall.RawConn) error {
		var controlErr error
		if err := connection.Control(func(fd uintptr) {
			controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName)
		}); err != nil {
			return err
		}
		return controlErr
	}
}
