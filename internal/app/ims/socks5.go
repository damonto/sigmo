package ims

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	imsgo "github.com/damonto/ims-go"
	"github.com/things-go/go-socks5/statute"
)

const socks5HandshakeTimeout = 8 * time.Second

var (
	errSOCKS5Unreachable = errors.New("connect to SOCKS5 server")
	errSOCKS5Auth        = errors.New("SOCKS5 authentication rejected")
	errSOCKS5UDPRejected = errors.New("SOCKS5 server rejected UDP ASSOCIATE")
	errSOCKS5ControlLost = errors.New("SOCKS5 UDP control connection closed")
)

type socks5Underlay struct {
	proxy wifiProxy
}

var _ imsgo.Underlay = socks5Underlay{}

func (socks5Underlay) LocalIP() netip.Addr {
	// The relay can reach ePDGs of either family, independently of the
	// connection between Sigmo and the proxy.
	return netip.Addr{}
}

func (s socks5Underlay) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	resolver := net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// DNS over TCP uses the same SOCKS5 CONNECT path. A literal DNS
			// server address avoids a lookup while connecting to the resolver.
			return s.DialContext(ctx, "tcp", "1.1.1.1:53")
		},
	}
	return resolver.LookupNetIP(ctx, network, host)
}

func (s socks5Underlay) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, _, err := s.dialTCP(ctx, network, address)
	return conn, err
}

func (s socks5Underlay) dialTCP(ctx context.Context, network, address string) (net.Conn, string, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, "", fmt.Errorf("SOCKS5 TCP dial: unsupported network %q", network)
	}
	target, err := statute.ParseAddrSpec(address)
	if err != nil || target.Port <= 0 || target.Port > 65535 || len(target.FQDN) > 255 {
		return nil, "", errors.New("invalid SOCKS5 destination")
	}
	conn, peer, _, err := s.command(ctx, statute.CommandConnect, target)
	return conn, peer, err
}

func (s socks5Underlay) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("SOCKS5 UDP listen: unsupported network %q", network)
	}
	conn, _, err := s.openUDP(ctx, address)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (s socks5Underlay) openUDP(ctx context.Context, address string) (*socks5PacketConn, string, error) {
	local, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, "", fmt.Errorf("parse SOCKS5 UDP bind address: %w", err)
	}
	target := statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero}
	// SWu passes a wildcard for the ePDG family. Choose the family of the
	// relay instead, while honoring an explicitly requested address or port.
	if !local.IP.IsUnspecified() && len(local.IP) != 0 {
		target, err = statute.ParseAddrSpec(local.String())
		if err != nil {
			return nil, "", fmt.Errorf("encode SOCKS5 UDP bind address: %w", err)
		}
	} else {
		local.IP = nil
	}
	control, peer, reply, err := s.command(ctx, statute.CommandAssociate, target)
	if err != nil {
		return nil, peer, err
	}
	if reply.Port == 0 || (len(reply.IP) == 0 && reply.FQDN == "") {
		control.Close()
		return nil, peer, errors.New("invalid SOCKS5 UDP relay address")
	}
	relayAddress := reply.String()
	if reply.IP.IsUnspecified() {
		// Some servers advertise an unspecified address with a usable port.
		host, _, err := net.SplitHostPort(peer)
		if err != nil {
			control.Close()
			return nil, peer, err
		}
		relayAddress = net.JoinHostPort(host, strconv.Itoa(reply.Port))
	}
	dialer := net.Dialer{LocalAddr: local, Timeout: socks5HandshakeTimeout}
	udp, err := dialer.DialContext(ctx, "udp", relayAddress)
	if err != nil {
		control.Close()
		return nil, peer, fmt.Errorf("open SOCKS5 UDP relay: %w", err)
	}
	conn := &socks5PacketConn{udp: udp, control: control, done: make(chan struct{})}
	go conn.watchControl()
	return conn, peer, nil
}

func (s socks5Underlay) command(ctx context.Context, command byte, target statute.AddrSpec) (_ net.Conn, peer string, _ statute.AddrSpec, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, socks5HandshakeTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.proxy.Host, strconv.Itoa(s.proxy.Port)))
	if err != nil {
		return nil, "", statute.AddrSpec{}, errors.Join(errSOCKS5Unreachable, err)
	}
	peer = conn.RemoteAddr().String()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		if resultErr != nil {
			_ = conn.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, peer, statute.AddrSpec{}, err
	}
	if err := s.authenticate(conn); err != nil {
		return nil, peer, statute.AddrSpec{}, fmt.Errorf("negotiate SOCKS5 authentication: %w", err)
	}
	request := statute.Request{Version: statute.VersionSocks5, Command: command, DstAddr: target}
	if _, err := conn.Write(request.Bytes()); err != nil {
		return nil, peer, statute.AddrSpec{}, fmt.Errorf("send SOCKS5 request: %w", err)
	}
	reply, err := statute.ParseReply(conn)
	if err != nil {
		return nil, peer, statute.AddrSpec{}, fmt.Errorf("read SOCKS5 reply: %w", err)
	}
	if reply.Reserved != 0 {
		return nil, peer, statute.AddrSpec{}, errors.New("invalid SOCKS5 reply")
	}
	if reply.Response != statute.RepSuccess {
		if command == statute.CommandAssociate {
			return nil, peer, statute.AddrSpec{}, fmt.Errorf("%w (reply %d)", errSOCKS5UDPRejected, reply.Response)
		}
		return nil, peer, statute.AddrSpec{}, fmt.Errorf("SOCKS5 CONNECT rejected (reply %d)", reply.Response)
	}
	if !stop() {
		return nil, peer, statute.AddrSpec{}, ctx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, peer, statute.AddrSpec{}, err
	}
	return conn, peer, reply.BndAddr, nil
}

func (s socks5Underlay) authenticate(conn net.Conn) error {
	method := byte(statute.MethodNoAuth)
	if s.proxy.Username != "" {
		method = statute.MethodUserPassAuth
	}
	request := statute.NewMethodRequest(statute.VersionSocks5, []byte{method})
	if _, err := conn.Write(request.Bytes()); err != nil {
		return err
	}
	reply, err := statute.ParseMethodReply(conn)
	if err != nil {
		return err
	}
	if reply.Ver != statute.VersionSocks5 || reply.Method != method {
		return errSOCKS5Auth
	}
	if method == statute.MethodNoAuth {
		return nil
	}
	credentials := statute.NewUserPassRequest(1, []byte(s.proxy.Username), []byte(s.proxy.Password))
	if _, err := conn.Write(credentials.Bytes()); err != nil {
		return err
	}
	var authReply [2]byte
	if _, err := io.ReadFull(conn, authReply[:]); err != nil {
		return err
	}
	if authReply != [2]byte{1, 0} {
		return errSOCKS5Auth
	}
	return nil
}

type socks5PacketConn struct {
	udp      net.Conn
	control  net.Conn
	done     chan struct{}
	lost     atomic.Bool
	close    sync.Once
	closeErr error
	readMu   sync.Mutex
	readBuf  [64 * 1024]byte
}

func (c *socks5PacketConn) watchControl() {
	defer close(c.done)
	_, _ = io.Copy(io.Discard, c.control)
	c.shutdown(true)
}

func (c *socks5PacketConn) LocalAddr() net.Addr {
	return c.udp.LocalAddr()
}

func (c *socks5PacketConn) SetDeadline(deadline time.Time) error {
	return c.udp.SetDeadline(deadline)
}

func (c *socks5PacketConn) SetReadDeadline(deadline time.Time) error {
	return c.udp.SetReadDeadline(deadline)
}

func (c *socks5PacketConn) SetWriteDeadline(deadline time.Time) error {
	return c.udp.SetWriteDeadline(deadline)
}

func (c *socks5PacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		n, err := c.udp.Read(c.readBuf[:])
		if err != nil {
			if c.lost.Load() {
				return 0, nil, errSOCKS5ControlLost
			}
			return 0, nil, err
		}
		data := c.readBuf[:n]
		if n < 4 || data[0] != 0 || data[1] != 0 || data[2] != 0 {
			continue
		}
		packet, err := statute.ParseDatagram(data)
		// SWu and the STUN probe send to numeric IPs and match replies by IP.
		if err != nil || len(packet.DstAddr.IP) == 0 || packet.DstAddr.Port == 0 {
			continue
		}
		source := &net.UDPAddr{IP: append(net.IP(nil), packet.DstAddr.IP...), Port: packet.DstAddr.Port}
		return copy(p, packet.Data), source, nil
	}
}

func (c *socks5PacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	target, ok := addr.(*net.UDPAddr)
	if !ok || target == nil || target.IP.To16() == nil || target.Port <= 0 || target.Port > 65535 || target.Zone != "" {
		return 0, errors.New("invalid SOCKS5 UDP destination")
	}
	packet, err := statute.NewDatagram(target.String(), p)
	if err != nil {
		return 0, err
	}
	if _, err := c.udp.Write(packet.Bytes()); err != nil {
		if c.lost.Load() {
			return 0, errSOCKS5ControlLost
		}
		return 0, err
	}
	return len(p), nil
}

func (c *socks5PacketConn) Close() error {
	c.shutdown(false)
	<-c.done
	return c.closeErr
}

func (c *socks5PacketConn) shutdown(controlLost bool) {
	c.close.Do(func() {
		// Set the reason before closing the UDP socket wakes pending reads.
		c.lost.Store(controlLost)
		c.closeErr = errors.Join(c.control.Close(), c.udp.Close())
	})
}
