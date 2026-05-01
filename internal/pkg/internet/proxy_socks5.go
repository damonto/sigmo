package internet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

const (
	proxyUDPBufferSize        = 64 * 1024
	proxyUDPTargetIdleTimeout = 2 * time.Minute
)

var proxyUDPAssociationIdleTimeout = time.Minute

type socks5UDPTarget struct {
	conn       net.Conn
	dstAddr    statute.AddrSpec
	clientAddr *net.UDPAddr
}

type socks5UDPAssociation struct {
	once    sync.Once
	relay   *net.UDPConn
	control io.Closer
	onClose func(*socks5UDPAssociation)
}

func (a *socks5UDPAssociation) Close() error {
	var result error
	a.once.Do(func() {
		if a.relay != nil {
			result = errors.Join(result, ignoreProxyCloseError(a.relay.Close()))
		}
		if a.control != nil {
			result = errors.Join(result, ignoreProxyCloseError(a.control.Close()))
		}
		if a.onClose != nil {
			a.onClose(a)
		}
	})
	return result
}

func (p *Proxy) startSOCKS5ProxyLocked() error {
	listener, err := net.Listen("tcp", net.JoinHostPort(p.cfg.ListenAddress, strconv.Itoa(p.cfg.SOCKS5Port))) //nolint:noctx
	if err != nil {
		return proxyStartError("listen socks5 proxy", err)
	}

	server := socks5.NewServer(
		socks5.WithCredential(proxyCredentials{proxy: p}),
		socks5.WithResolver(deferSOCKS5DNSResolver{}),
		socks5.WithRule(socks5.NewPermitConnAndAss()),
		socks5.WithDialAndRequest(p.dialSOCKS5),
		socks5.WithAssociateHandle(p.handleSOCKS5Associate),
	)
	p.socksServer = server
	p.socksListener = listener
	p.socksAddress = listener.Addr().String()

	go func() {
		err := server.Serve(listener)
		if ignoreSOCKS5ProxyServeError(err) {
			return
		}
		p.handleServeExit("socks5", nil, server, err)
	}()
	return nil
}

func ignoreSOCKS5ProxyServeError(err error) bool {
	return err == nil || errors.Is(err, net.ErrClosed)
}

func (p *Proxy) dialSOCKS5(ctx context.Context, network string, address string, request *socks5.Request) (net.Conn, error) {
	return p.dial(ctx, proxyUsernameFromRequest(request), network, address)
}

func (p *Proxy) handleSOCKS5Associate(ctx context.Context, writer io.Writer, request *socks5.Request) error {
	username := proxyUsernameFromRequest(request)
	relayAddr, err := socks5UDPRelayAddr(request)
	if err != nil {
		if replyErr := socks5.SendReply(writer, statute.RepServerFailure, nil); replyErr != nil {
			return errors.Join(err, fmt.Errorf("send socks5 udp failure reply: %w", replyErr))
		}
		return err
	}

	relay, err := net.ListenUDP("udp", relayAddr)
	if err != nil {
		if replyErr := socks5.SendReply(writer, statute.RepServerFailure, nil); replyErr != nil {
			return errors.Join(fmt.Errorf("listen socks5 udp relay: %w", err), fmt.Errorf("send socks5 udp failure reply: %w", replyErr))
		}
		return fmt.Errorf("listen socks5 udp relay: %w", err)
	}
	association := &socks5UDPAssociation{relay: relay}
	if control, ok := writer.(io.Closer); ok {
		association.control = control
	}
	association.onClose = func(association *socks5UDPAssociation) {
		p.removeSession(username, association)
	}
	if err := p.trackSession(username, association); err != nil {
		replyErr := socks5.SendReply(writer, statute.RepServerFailure, nil)
		closeErr := association.Close()
		return errors.Join(err, replyErr, closeErr)
	}

	if err := socks5.SendReply(writer, statute.RepSuccess, relay.LocalAddr()); err != nil {
		closeErr := association.Close()
		return errors.Join(fmt.Errorf("send socks5 udp associate reply: %w", err), closeErr)
	}

	relayDone := make(chan error, 1)
	go func() {
		relayDone <- p.relaySOCKS5UDP(ctx, username, relay, socks5UDPAllowedSource(request))
	}()

	controlDone := make(chan error, 1)
	go func() {
		controlDone <- drainSOCKS5Control(request.Reader)
	}()

	var (
		controlErr     error
		relayErr       error
		controlStopped bool
		relayStopped   bool
	)
	select {
	case controlErr = <-controlDone:
		controlStopped = true
	case relayErr = <-relayDone:
		relayStopped = true
	}

	closeErr := association.Close()
	if !controlStopped {
		controlErr = <-controlDone
	}
	if !relayStopped {
		relayErr = <-relayDone
	}
	return errors.Join(controlErr, closeErr, relayErr)
}

func proxyUsernameFromRequest(request *socks5.Request) string {
	if request == nil || request.AuthContext == nil {
		return ""
	}
	return request.AuthContext.Payload["username"]
}

func socks5UDPRelayAddr(request *socks5.Request) (*net.UDPAddr, error) {
	tcpAddr, ok := request.LocalAddr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("socks5 udp local address is %T, want *net.TCPAddr", request.LocalAddr)
	}
	return &net.UDPAddr{IP: tcpAddr.IP, Port: 0}, nil
}

func socks5UDPAllowedSource(request *socks5.Request) *net.UDPAddr {
	if request == nil || request.DestAddr == nil {
		return &net.UDPAddr{}
	}
	ip := request.DestAddr.IP
	if len(ip) == 0 || ip.IsUnspecified() {
		ip = tcpRemoteIP(request.RemoteAddr)
	}
	return &net.UDPAddr{IP: append(net.IP(nil), ip...), Port: request.DestAddr.Port}
}

func drainSOCKS5Control(reader io.Reader) error {
	buf := make([]byte, 1024)
	for {
		if _, err := reader.Read(buf); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (p *Proxy) relaySOCKS5UDP(ctx context.Context, username string, relay *net.UDPConn, allowedSource *net.UDPAddr) error {
	var (
		mu        sync.Mutex
		targets   = make(map[string]socks5UDPTarget)
		clientSrc *net.UDPAddr
		buf       = make([]byte, proxyUDPBufferSize)
	)
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for key, target := range targets {
			closeProxyPipeConn(target.conn)
			delete(targets, key)
		}
	}()

	refreshSOCKS5UDPDeadline(relay, proxyUDPAssociationIdleTimeout)
	for {
		n, srcAddr, err := relay.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if isProxyTimeoutError(err) {
				return nil
			}
			return fmt.Errorf("read socks5 udp relay: %w", err)
		}
		if !socks5UDPSourceAllowed(allowedSource, clientSrc, srcAddr) {
			continue
		}
		if clientSrc == nil {
			clientSrc = cloneUDPAddr(srcAddr)
		}
		refreshSOCKS5UDPDeadline(relay, proxyUDPAssociationIdleTimeout)

		packet, err := statute.ParseDatagram(buf[:n])
		if err != nil || packet.Frag != 0 {
			continue
		}

		key := srcAddr.String() + "--" + packet.DstAddr.String()
		mu.Lock()
		target, ok := targets[key]
		mu.Unlock()
		if !ok {
			conn, err := p.dial(ctx, username, "udp", packet.DstAddr.String())
			if err != nil {
				slog.Error("socks5 udp target dial failed", "target", packet.DstAddr.String(), "error", err)
				continue
			}
			refreshSOCKS5UDPDeadline(conn, proxyUDPTargetIdleTimeout)
			target = socks5UDPTarget{
				conn:       conn,
				dstAddr:    packet.DstAddr,
				clientAddr: cloneUDPAddr(srcAddr),
			}
			mu.Lock()
			targets[key] = target
			mu.Unlock()
			go p.relaySOCKS5UDPResponse(relay, key, target, &mu, targets)
		}

		refreshSOCKS5UDPDeadline(target.conn, proxyUDPTargetIdleTimeout)
		if _, err := target.conn.Write(packet.Data); err != nil {
			slog.Error("socks5 udp target write failed", "target", packet.DstAddr.String(), "error", err)
			mu.Lock()
			delete(targets, key)
			mu.Unlock()
			closeProxyPipeConn(target.conn)
		}
	}
}

func (p *Proxy) relaySOCKS5UDPResponse(relay *net.UDPConn, key string, target socks5UDPTarget, mu *sync.Mutex, targets map[string]socks5UDPTarget) {
	defer func() {
		mu.Lock()
		delete(targets, key)
		mu.Unlock()
		closeProxyPipeConn(target.conn)
	}()

	buf := make([]byte, proxyUDPBufferSize)
	for {
		n, err := target.conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || isProxyTimeoutError(err) {
				return
			}
			slog.Error("socks5 udp target read failed", "target", target.dstAddr.String(), "error", err)
			return
		}
		refreshSOCKS5UDPDeadline(target.conn, proxyUDPTargetIdleTimeout)
		response := statute.Datagram{
			RSV:     0,
			Frag:    0,
			DstAddr: target.dstAddr,
			Data:    append([]byte(nil), buf[:n]...),
		}
		if _, err := relay.WriteToUDP(response.Bytes(), target.clientAddr); err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("socks5 udp client write failed", "client", target.clientAddr.String(), "error", err)
			return
		}
	}
}

func refreshSOCKS5UDPDeadline(conn net.Conn, timeout time.Duration) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		slog.Debug("refresh socks5 udp deadline", "error", err)
	}
}

func isProxyTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func socks5UDPSourceAllowed(allowedSource *net.UDPAddr, pinnedSource *net.UDPAddr, srcAddr *net.UDPAddr) bool {
	if srcAddr == nil {
		return false
	}
	if pinnedSource != nil {
		return sameUDPAddr(pinnedSource, srcAddr)
	}
	if allowedSource == nil {
		return true
	}
	if len(allowedSource.IP) != 0 && !allowedSource.IP.IsUnspecified() && !allowedSource.IP.Equal(srcAddr.IP) {
		return false
	}
	return allowedSource.Port == 0 || allowedSource.Port == srcAddr.Port
}

func tcpRemoteIP(addr net.Addr) net.IP {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil {
		return nil
	}
	return tcpAddr.IP
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	return &net.UDPAddr{
		IP:   append(net.IP(nil), addr.IP...),
		Port: addr.Port,
		Zone: addr.Zone,
	}
}

func sameUDPAddr(a *net.UDPAddr, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Port == b.Port && a.Zone == b.Zone && a.IP.Equal(b.IP)
}
