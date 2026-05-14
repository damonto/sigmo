package internet

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	socks5 "github.com/things-go/go-socks5"
)

const proxyDialTimeout = 30 * time.Second

var (
	ErrProxyPasswordRequired  = errors.New("proxy password is required")
	ErrProxyInterfaceRequired = errors.New("proxy interface is required")
	ErrProxyUsernameRequired  = errors.New("proxy username is required")
	ErrProxyStart             = errors.New("proxy start")
)

type ProxyConfig struct {
	ListenAddress string
	HTTPPort      int
	SOCKS5Port    int
	Password      string
}

type ProxyStatus struct {
	Enabled       bool
	Username      string
	Password      string
	HTTPAddress   string
	SOCKS5Address string
}

type ProxyBinding struct {
	Username      string
	InterfaceName string
}

type proxyDialFunc func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error)

type Proxy struct {
	mu       sync.Mutex
	cfg      ProxyConfig
	active   map[string]string
	sessions map[string]map[proxySession]struct{}
	dialFunc proxyDialFunc

	httpServer    *http.Server
	httpListener  net.Listener
	httpAddress   string
	socksServer   *socks5.Server
	socksListener net.Listener
	socksAddress  string
}

type proxySession interface {
	Close() error
}

type proxyConn struct {
	net.Conn
	once    sync.Once
	onClose func(*proxyConn)
}

func (c *proxyConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		if c.onClose != nil {
			c.onClose(c)
		}
	})
	return err
}

func NewProxy(cfg ProxyConfig) *Proxy {
	return newProxyWithDial(cfg, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
		return boundDialerWithTimeout(interfaceName, proxyDialTimeout).DialContext(ctx, network, address)
	})
}

func newProxyWithDial(cfg ProxyConfig, dialFunc proxyDialFunc) *Proxy {
	return &Proxy{
		cfg:      cfg,
		active:   make(map[string]string),
		sessions: make(map[string]map[proxySession]struct{}),
		dialFunc: dialFunc,
	}
}

func (p *Proxy) UpdateConfig(cfg ProxyConfig) error {
	p.mu.Lock()
	if p.cfg == cfg {
		p.mu.Unlock()
		return nil
	}

	oldCfg := p.cfg
	sessions := p.takeAllSessionsLocked()
	err := p.stopLocked()
	p.cfg = cfg
	if len(p.active) > 0 && strings.TrimSpace(cfg.Password) != "" {
		if startErr := p.startLocked(); startErr != nil {
			cleanupErr := p.stopLocked()
			p.cfg = oldCfg
			var restoreErr error
			if strings.TrimSpace(oldCfg.Password) != "" {
				restoreErr = p.startLocked()
			}
			p.mu.Unlock()
			closeErr := closeProxySessions(sessions)
			return errors.Join(
				err,
				fmt.Errorf("start proxy with new config: %w", startErr),
				cleanupErr,
				restoreErr,
				closeErr,
			)
		}
	}
	p.mu.Unlock()
	return errors.Join(err, closeProxySessions(sessions))
}

func (p *Proxy) Register(binding ProxyBinding) (ProxyStatus, error) {
	binding.Username = strings.TrimSpace(binding.Username)
	binding.InterfaceName = strings.TrimSpace(binding.InterfaceName)
	if binding.Username == "" {
		return ProxyStatus{}, ErrProxyUsernameRequired
	}
	if binding.InterfaceName == "" {
		return ProxyStatus{}, ErrProxyInterfaceRequired
	}

	p.mu.Lock()

	if strings.TrimSpace(p.cfg.Password) == "" {
		p.mu.Unlock()
		return ProxyStatus{}, ErrProxyPasswordRequired
	}
	oldInterfaceName, oldActive := p.active[binding.Username]
	p.active[binding.Username] = binding.InterfaceName
	if err := p.ensureStartedLocked(); err != nil {
		if oldActive {
			p.active[binding.Username] = oldInterfaceName
		} else {
			delete(p.active, binding.Username)
		}
		p.mu.Unlock()
		return ProxyStatus{}, err
	}
	var sessions []proxySession
	if oldInterfaceName != "" && oldInterfaceName != binding.InterfaceName {
		sessions = p.takeUserSessionsLocked(binding.Username)
	}
	status := p.statusLocked(binding.Username)
	p.mu.Unlock()

	return status, closeProxySessions(sessions)
}

func (p *Proxy) Unregister(username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}

	p.mu.Lock()

	delete(p.active, username)
	sessions := p.takeUserSessionsLocked(username)
	var stopErr error
	if len(p.active) == 0 {
		sessions = append(sessions, p.takeAllSessionsLocked()...)
		stopErr = p.stopLocked()
	}
	p.mu.Unlock()

	return errors.Join(stopErr, closeProxySessions(sessions))
}

func (p *Proxy) Status(username string) ProxyStatus {
	username = strings.TrimSpace(username)
	if username == "" {
		return ProxyStatus{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusLocked(username)
}

func (p *Proxy) startedLocked() bool {
	return p.httpServer != nil && p.socksServer != nil
}

func (p *Proxy) hasListenerStateLocked() bool {
	return p.httpServer != nil || p.httpListener != nil || p.socksServer != nil || p.socksListener != nil
}

func (p *Proxy) ensureStartedLocked() error {
	if p.startedLocked() {
		return nil
	}
	if p.hasListenerStateLocked() {
		if err := p.stopLocked(); err != nil {
			return fmt.Errorf("reset proxy listeners: %w", err)
		}
	}
	return p.startLocked()
}

func (p *Proxy) statusLocked(username string) ProxyStatus {
	_, active := p.active[username]
	if !active {
		return ProxyStatus{}
	}
	return ProxyStatus{
		Enabled:       p.startedLocked(),
		Username:      username,
		Password:      p.cfg.Password,
		HTTPAddress:   p.httpAddress,
		SOCKS5Address: p.socksAddress,
	}
}

func (p *Proxy) startLocked() error {
	if err := p.startHTTPProxyLocked(); err != nil {
		return err
	}
	if err := p.startSOCKS5ProxyLocked(); err != nil {
		return errors.Join(err, p.stopLocked())
	}
	return nil
}

func (p *Proxy) handleServeExit(kind string, httpServer *http.Server, socksServer *socks5.Server, err error) {
	p.mu.Lock()
	if httpServer != nil && p.httpServer != httpServer {
		p.mu.Unlock()
		return
	}
	if socksServer != nil && p.socksServer != socksServer {
		p.mu.Unlock()
		return
	}

	sessions := p.takeAllSessionsLocked()
	stopErr := p.stopLocked()
	var startErr error
	if len(p.active) > 0 {
		startErr = p.startLocked()
	}
	p.mu.Unlock()

	closeErr := closeProxySessions(sessions)
	slog.Error("proxy listener stopped", "kind", kind, "error", err)
	if stopErr != nil {
		slog.Error("proxy listener cleanup failed", "kind", kind, "error", stopErr)
	}
	if closeErr != nil {
		slog.Error("proxy session cleanup failed", "kind", kind, "error", closeErr)
	}
	if startErr != nil {
		slog.Error("proxy restart failed", "kind", kind, "error", startErr)
	}
}

func (p *Proxy) stopLocked() error {
	var result error
	if p.httpServer != nil {
		result = errors.Join(result, ignoreProxyCloseError(p.httpServer.Close()))
	}
	if p.socksListener != nil {
		result = errors.Join(result, ignoreProxyCloseError(p.socksListener.Close()))
	}
	p.httpServer = nil
	p.httpListener = nil
	p.httpAddress = ""
	p.socksServer = nil
	p.socksListener = nil
	p.socksAddress = ""
	return result
}

func ignoreProxyCloseError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (p *Proxy) dial(ctx context.Context, username string, network string, address string) (net.Conn, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, ErrProxyUsernameRequired
	}
	interfaceName, ok := p.interfaceForUser(username)
	if !ok {
		return nil, fmt.Errorf("proxy username %s is not active", username)
	}
	conn, err := p.dialFunc(ctx, interfaceName, network, address)
	if err != nil {
		return nil, err
	}
	return p.trackConn(username, conn)
}

func (p *Proxy) validCredential(username string, password string) bool {
	username = strings.TrimSpace(username)
	if username == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(password), []byte(p.cfg.Password)) != 1 {
		return false
	}
	_, ok := p.active[username]
	return ok
}

func (p *Proxy) interfaceForUser(username string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interfaceName, ok := p.active[username]
	return interfaceName, ok
}

func (p *Proxy) trackConn(username string, conn net.Conn) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("proxy dial returned nil connection")
	}
	tracked := &proxyConn{Conn: conn}
	tracked.onClose = func(conn *proxyConn) {
		p.removeSession(username, conn)
	}

	if err := p.trackSession(username, tracked); err != nil {
		closeErr := conn.Close()
		return nil, errors.Join(err, closeErr)
	}
	return tracked, nil
}

func (p *Proxy) trackSession(username string, session proxySession) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return ErrProxyUsernameRequired
	}

	p.mu.Lock()
	if _, ok := p.active[username]; !ok {
		p.mu.Unlock()
		return fmt.Errorf("proxy username %s is not active", username)
	}
	if p.sessions[username] == nil {
		p.sessions[username] = make(map[proxySession]struct{})
	}
	p.sessions[username][session] = struct{}{}
	p.mu.Unlock()
	return nil
}

func (p *Proxy) removeSession(username string, session proxySession) {
	username = strings.TrimSpace(username)
	if username == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	sessions := p.sessions[username]
	if sessions == nil {
		return
	}
	delete(sessions, session)
	if len(sessions) == 0 {
		delete(p.sessions, username)
	}
}

func (p *Proxy) takeUserSessionsLocked(username string) []proxySession {
	sessions := p.sessions[username]
	if len(sessions) == 0 {
		return nil
	}
	result := make([]proxySession, 0, len(sessions))
	for session := range sessions {
		result = append(result, session)
	}
	delete(p.sessions, username)
	return result
}

func (p *Proxy) takeAllSessionsLocked() []proxySession {
	var result []proxySession
	for _, sessions := range p.sessions {
		for session := range sessions {
			result = append(result, session)
		}
	}
	p.sessions = make(map[string]map[proxySession]struct{})
	return result
}

func closeProxySessions(sessions []proxySession) error {
	var result error
	for _, session := range sessions {
		result = errors.Join(result, ignoreProxyCloseError(session.Close()))
	}
	return result
}

func proxyStartError(action string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrProxyStart, action, err)
}

type proxyCredentials struct {
	proxy *Proxy
}

func (c proxyCredentials) Valid(user string, password string, _ string) bool {
	return c.proxy.validCredential(user, password)
}

type deferSOCKS5DNSResolver struct{}

func (deferSOCKS5DNSResolver) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	// Returning nil keeps the original FQDN so the interface-bound dialer resolves it.
	return ctx, nil, nil
}
