package webpush

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const (
	pushRequestTimeout = 15 * time.Second
	pushDialTimeout    = 10 * time.Second
)

var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

func newPushHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: pushDialTimeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPublicPushAddress(ctx, dialer, net.DefaultResolver, network, address)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   pushRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func dialPublicPushAddress(
	ctx context.Context,
	dialer *net.Dialer,
	resolver *net.Resolver,
	network string,
	address string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse push endpoint address: %w", err)
	}
	lookupNetwork := "ip"
	if strings.HasSuffix(network, "4") {
		lookupNetwork = "ip4"
	} else if strings.HasSuffix(network, "6") {
		lookupNetwork = "ip6"
	}
	addresses, err := resolver.LookupNetIP(ctx, lookupNetwork, host)
	if err != nil {
		return nil, fmt.Errorf("resolve push endpoint %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve push endpoint %q: no addresses", host)
	}
	for _, addr := range addresses {
		if !isPublicPushAddress(addr) {
			return nil, fmt.Errorf("push endpoint %q resolves to a non-public address", host)
		}
	}

	var combined error
	for _, addr := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		combined = errors.Join(combined, err)
	}
	return nil, fmt.Errorf("connect push endpoint %q: %w", host, combined)
}

func isPublicPushAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() &&
		addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!carrierGradeNAT.Contains(addr)
}
