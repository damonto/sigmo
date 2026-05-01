package internet

import (
	"context"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const dnsServer = "1.1.1.1:53"

func boundTransportWithTimeout(interfaceName string, timeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:       nil,
		DialContext: boundDialerWithTimeout(interfaceName, timeout).DialContext,
	}
}

func boundDialerWithTimeout(interfaceName string, timeout time.Duration) *net.Dialer {
	dialer := rawBoundDialerWithTimeout(interfaceName, timeout)
	dialer.Resolver = boundResolverWithTimeout(interfaceName, timeout)
	return dialer
}

func boundResolverWithTimeout(interfaceName string, timeout time.Duration) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return rawBoundDialerWithTimeout(interfaceName, timeout).DialContext(ctx, dnsNetwork(network), dnsServer)
		},
	}
}

func dnsNetwork(network string) string {
	if strings.HasPrefix(network, "tcp") {
		return "tcp4"
	}
	return "udp4"
}

func rawBoundDialerWithTimeout(interfaceName string, timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, connection syscall.RawConn) error {
			var controlErr error
			if err := connection.Control(func(fd uintptr) {
				controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName)
			}); err != nil {
				return err
			}
			return controlErr
		},
	}
}
