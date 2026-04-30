package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	ipInfoURL     = "https://ipinfo.io/json"
	dnsServer     = "1.1.1.1:53"
	ipInfoTimeout = 4 * time.Second
)

type IPInfo struct {
	IP           string
	Country      string
	Organization string
}

func (c *Connector) Public(ctx context.Context, modem *mmodem.Modem) (IPInfo, error) {
	connection, err := c.Current(modem)
	if err != nil {
		return IPInfo{}, err
	}
	if connection.Status != StatusConnected {
		return IPInfo{}, nil
	}
	interfaceName := strings.TrimSpace(connection.InterfaceName)
	if interfaceName == "" {
		return IPInfo{}, nil
	}
	return fetchIPInfo(ctx, interfaceName)
}

type ipInfoResponse struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

func fetchIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return IPInfo{}, errors.New("interface name is empty")
	}

	return requestIPInfo(ctx, interfaceName)
}

func requestIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	client := &http.Client{
		Timeout:   ipInfoTimeout,
		Transport: boundTransport(interfaceName),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoURL, nil)
	if err != nil {
		return IPInfo{}, fmt.Errorf("create ipinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return IPInfo{}, fmt.Errorf("request ipinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return IPInfo{}, fmt.Errorf("ipinfo status: %d", resp.StatusCode)
	}

	var payload ipInfoResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return IPInfo{}, fmt.Errorf("decode ipinfo response: %w", err)
	}
	country := strings.ToUpper(strings.TrimSpace(payload.Country))
	return IPInfo{
		IP:           strings.TrimSpace(payload.IP),
		Country:      country,
		Organization: strings.TrimSpace(payload.Org),
	}, nil
}

func boundResolver(interfaceName string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return rawBoundDialer(interfaceName).DialContext(ctx, dnsNetwork(network), dnsServer)
		},
	}
}

func dnsNetwork(network string) string {
	if strings.HasPrefix(network, "tcp") {
		return "tcp4"
	}
	return "udp4"
}

func boundTransport(interfaceName string) *http.Transport {
	return &http.Transport{
		Proxy:       nil,
		DialContext: boundDialer(interfaceName).DialContext,
	}
}

func boundDialer(interfaceName string) *net.Dialer {
	dialer := rawBoundDialer(interfaceName)
	dialer.Resolver = boundResolver(interfaceName)
	return dialer
}

func rawBoundDialer(interfaceName string) *net.Dialer {
	dialer := &net.Dialer{
		Timeout: ipInfoTimeout,
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
	return dialer
}
