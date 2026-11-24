package ims

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/stun/v4"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
)

const wifiProxyCheckSTUN = "stun.cloudflare.com:3478"

var errWiFiProxyUDPTimeout = errors.New("SOCKS5 UDP relay returned no STUN response")

type wifiProxyCheckResult struct {
	TCP wifiProxyCheckStep `json:"tcp"`
	UDP wifiProxyCheckStep `json:"udp"`
	DNS wifiProxyCheckStep `json:"dns"`
}

type wifiProxyCheckStep struct {
	DurationMs    int64        `json:"durationMs"`
	ErrorCode     string       `json:"errorCode,omitempty"`
	ServerAddress string       `json:"serverAddress,omitempty"`
	RelayAddress  string       `json:"relayAddress,omitempty"`
	IP            netip.Addr   `json:"ip,omitzero"`
	Port          int          `json:"port,omitempty"`
	Addresses     []netip.Addr `json:"addresses,omitempty"`
}

func checkWiFiProxy(ctx context.Context, underlay socks5Underlay) wifiProxyCheckResult {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var result wifiProxyCheckResult
	var probes sync.WaitGroup
	probes.Go(func() { result.TCP = checkWiFiProxyTCP(ctx, underlay, pinternet.LookupPublicIP) })
	probes.Go(func() { result.UDP = checkWiFiProxyUDP(ctx, underlay, wifiProxyCheckSTUN) })
	probes.Go(func() {
		start := time.Now()
		addresses, err := underlay.LookupNetIP(ctx, "ip", "example.com.")
		result.DNS = wifiProxyCheckStep{
			DurationMs: time.Since(start).Milliseconds(), Addresses: addresses,
			ErrorCode: wifiProxyCheckError(err, "dns_failed"),
		}
	})
	probes.Wait()
	return result
}

func checkWiFiProxyTCP(ctx context.Context, underlay socks5Underlay, lookup func(context.Context, *http.Client) (pinternet.PublicIPInfo, error)) wifiProxyCheckStep {
	start := time.Now()
	var serverAddress atomic.Pointer[string]
	client := wifiCallingHTTPClientWithDial(func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, peer, err := underlay.dialTCP(ctx, network, address)
		if peer != "" {
			serverAddress.Store(&peer)
		}
		return conn, err
	})
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	defer client.CloseIdleConnections()
	info, err := lookup(ctx, client)
	address, parseErr := netip.ParseAddr(info.IP)
	if err == nil {
		err = parseErr
	}
	result := wifiProxyCheckStep{
		DurationMs: time.Since(start).Milliseconds(), IP: address.Unmap(),
		ErrorCode: wifiProxyCheckError(err, "tcp_ip_lookup_failed"),
	}
	if peer := serverAddress.Load(); peer != nil {
		result.ServerAddress = *peer
	}
	return result
}

func checkWiFiProxyUDP(ctx context.Context, underlay socks5Underlay, stunAddress string) (step wifiProxyCheckStep) {
	start := time.Now()
	defer func() { step.DurationMs = time.Since(start).Milliseconds() }()
	conn, peer, err := underlay.openUDP(ctx, ":0")
	step.ServerAddress = peer
	if err != nil {
		step.ErrorCode = wifiProxyCheckError(err, "udp_relay_failed")
		return step
	}
	defer conn.Close()
	step.RelayAddress = conn.udp.RemoteAddr().String()
	step.IP, step.Port, err = checkSTUNBinding(ctx, conn, underlay, stunAddress)
	step.ErrorCode = wifiProxyCheckError(err, "udp_probe_failed")
	return step
}

func checkSTUNBinding(ctx context.Context, conn net.PacketConn, underlay socks5Underlay, address string) (netip.Addr, int, error) {
	host, service, err := net.SplitHostPort(address)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	port, err := strconv.Atoi(service)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	addresses, err := underlay.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, 0, fmt.Errorf("resolve STUN server through SOCKS5: %w", err)
	}
	if len(addresses) == 0 {
		return netip.Addr{}, 0, errors.New("resolve STUN server through SOCKS5: no addresses returned")
	}
	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return netip.Addr{}, 0, err
		}
	}
	// Try all families before retrying so an unreachable IPv6 address cannot
	// consume the whole timeout when the proxy has a working IPv4 path.
	for _, delay := range []time.Duration{300 * time.Millisecond, 600 * time.Millisecond, 1200 * time.Millisecond} {
		if err := ctx.Err(); err != nil {
			return netip.Addr{}, 0, err
		}
		writes := 0
		var writeErr error
		for _, addr := range addresses {
			if _, err := conn.WriteTo(request.Raw, net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, uint16(port)))); err != nil {
				writeErr = err
			} else {
				writes++
			}
		}
		if writes == 0 {
			return netip.Addr{}, 0, writeErr
		}
		deadline := time.Now().Add(delay)
		if overall, ok := ctx.Deadline(); ok && overall.Before(deadline) {
			deadline = overall
		}
		if err := conn.SetReadDeadline(deadline); err != nil {
			return netip.Addr{}, 0, err
		}
		// Cancellation can race with setting a new deadline for this attempt.
		if err := ctx.Err(); err != nil {
			return netip.Addr{}, 0, err
		}
		mapped, err := readSTUNBinding(conn, request, addresses, port)
		if err == nil {
			ip, _ := netip.AddrFromSlice(mapped.IP)
			return ip.Unmap(), mapped.Port, nil
		}
		if timeout, ok := errors.AsType[net.Error](err); !ok || !timeout.Timeout() {
			return netip.Addr{}, 0, err
		}
	}
	if err := ctx.Err(); err != nil {
		return netip.Addr{}, 0, err
	}
	return netip.Addr{}, 0, errWiFiProxyUDPTimeout
}

func readSTUNBinding(conn net.PacketConn, request *stun.Message, addresses []netip.Addr, port int) (stun.XORMappedAddress, error) {
	var buf [2048]byte
	for {
		n, source, err := conn.ReadFrom(buf[:])
		if err != nil {
			return stun.XORMappedAddress{}, err
		}
		addr, ok := source.(*net.UDPAddr)
		if !ok || addr.Port != port {
			continue
		}
		valid := false
		for _, expected := range addresses {
			valid = valid || expected.Unmap() == addr.AddrPort().Addr().Unmap()
		}
		if !valid {
			continue
		}
		var reply stun.Message
		if err := stun.Decode(buf[:n], &reply); err != nil || reply.TransactionID != request.TransactionID || reply.Type != stun.BindingSuccess {
			continue
		}
		var mapped stun.XORMappedAddress
		if err := mapped.GetFrom(&reply); err == nil && mapped.Port != 0 {
			return mapped, nil
		}
	}
}

func wifiProxyCheckError(err error, fallback string) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errSOCKS5Auth):
		return "authentication_failed"
	case errors.Is(err, errSOCKS5Unreachable):
		return "proxy_unreachable"
	case errors.Is(err, errSOCKS5UDPRejected):
		return "udp_associate_rejected"
	case errors.Is(err, errSOCKS5ControlLost):
		return "udp_relay_closed"
	case errors.Is(err, errWiFiProxyUDPTimeout):
		return "udp_no_response"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed_out"
	default:
		return fallback
	}
}
