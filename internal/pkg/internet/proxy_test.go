package internet

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/things-go/go-socks5/statute"
)

func TestParseProxyBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		header       string
		wantUser     string
		wantPassword string
		wantOK       bool
	}{
		{
			name:         "valid",
			header:       "Basic " + base64.StdEncoding.EncodeToString([]byte("wwan0:secret")),
			wantUser:     "wwan0",
			wantPassword: "secret",
			wantOK:       true,
		},
		{
			name:   "wrong scheme",
			header: "Bearer token",
		},
		{
			name:   "invalid base64",
			header: "Basic not-base64",
		},
		{
			name:   "missing separator",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte("wwan0")),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotUser, gotPassword, gotOK := parseProxyBasicAuth(tt.header)
			if gotOK != tt.wantOK {
				t.Fatalf("parseProxyBasicAuth() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotUser != tt.wantUser {
				t.Fatalf("parseProxyBasicAuth() user = %q, want %q", gotUser, tt.wantUser)
			}
			if gotPassword != tt.wantPassword {
				t.Fatalf("parseProxyBasicAuth() password = %q, want %q", gotPassword, tt.wantPassword)
			}
		})
	}
}

func TestProxyRegister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     ProxyConfig
		iface   string
		wantErr error
	}{
		{
			name: "missing password",
			cfg: ProxyConfig{
				ListenAddress: "127.0.0.1",
			},
			iface:   "wwan0",
			wantErr: ErrProxyPasswordRequired,
		},
		{
			name: "missing interface",
			cfg: ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			},
			wantErr: ErrProxyInterfaceRequired,
		},
		{
			name: "starts listeners",
			cfg: ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			},
			iface: "wwan0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proxy := newProxyWithDial(tt.cfg, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				return nil, errors.New("dial unused")
			})
			t.Cleanup(func() {
				if err := proxy.Unregister(tt.iface); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			got, err := proxy.Register(tt.iface)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if !got.Enabled {
				t.Fatal("Register() status enabled = false, want true")
			}
			if got.Username != tt.iface {
				t.Fatalf("Register() username = %q, want %q", got.Username, tt.iface)
			}
			if got.Password != tt.cfg.Password {
				t.Fatalf("Register() password = %q, want %q", got.Password, tt.cfg.Password)
			}
			if got.HTTPAddress == "" {
				t.Fatal("Register() HTTPAddress is empty")
			}
			if got.SOCKS5Address == "" {
				t.Fatal("Register() SOCKS5Address is empty")
			}
		})
	}
}

func TestProxyStatusDoesNotStartListeners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "active state only"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				return nil, errors.New("dial unused")
			})
			proxy.mu.Lock()
			proxy.active["wwan0"] = struct{}{}
			proxy.mu.Unlock()

			status := proxy.Status("wwan0")
			if status.Enabled {
				t.Fatal("Status().Enabled = true, want false")
			}
			if status.HTTPAddress != "" {
				t.Fatalf("Status().HTTPAddress = %q, want empty", status.HTTPAddress)
			}
			if status.SOCKS5Address != "" {
				t.Fatalf("Status().SOCKS5Address = %q, want empty", status.SOCKS5Address)
			}
			proxy.mu.Lock()
			started := proxy.startedLocked()
			proxy.mu.Unlock()
			if started {
				t.Fatal("Status() started proxy listeners")
			}
		})
	}
}

func TestProxyUpdateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		start      ProxyConfig
		update     ProxyConfig
		wantOldOK  bool
		wantNewOK  bool
		wantActive bool
	}{
		{
			name: "updates active proxy password",
			start: ProxyConfig{
				ListenAddress: "127.0.0.1",
				HTTPPort:      0,
				SOCKS5Port:    0,
				Password:      "old",
			},
			update: ProxyConfig{
				ListenAddress: "127.0.0.1",
				HTTPPort:      0,
				SOCKS5Port:    0,
				Password:      "new",
			},
			wantNewOK:  true,
			wantActive: true,
		},
		{
			name: "blank password stops active listeners",
			start: ProxyConfig{
				ListenAddress: "127.0.0.1",
				HTTPPort:      0,
				SOCKS5Port:    0,
				Password:      "old",
			},
			update: ProxyConfig{
				ListenAddress: "127.0.0.1",
				HTTPPort:      0,
				SOCKS5Port:    0,
			},
			wantOldOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proxy := newProxyWithDial(tt.start, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				return nil, errors.New("dial should not be called")
			})
			if _, err := proxy.Register("wwan0"); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			if err := proxy.UpdateConfig(tt.update); err != nil {
				t.Fatalf("UpdateConfig() error = %v", err)
			}

			if got := proxy.validCredential("wwan0", "old"); got != tt.wantOldOK {
				t.Fatalf("valid old credential = %v, want %v", got, tt.wantOldOK)
			}
			if got := proxy.validCredential("wwan0", "new"); got != tt.wantNewOK {
				t.Fatalf("valid new credential = %v, want %v", got, tt.wantNewOK)
			}
			if got := proxy.Status("wwan0").Enabled; got != tt.wantActive {
				t.Fatalf("proxy active = %v, want %v", got, tt.wantActive)
			}
		})
	}
}

func TestProxyUpdateConfigRestoresOldListenersOnStartError(t *testing.T) {
	t.Parallel()

	occupied, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() {
		if err := occupied.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	proxy := newProxyWithDial(ProxyConfig{
		ListenAddress: "127.0.0.1",
		HTTPPort:      0,
		SOCKS5Port:    0,
		Password:      "old",
	}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
		return nil, errors.New("dial should not be called")
	})
	if _, err := proxy.Register("wwan0"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	t.Cleanup(func() {
		if err := proxy.Unregister("wwan0"); err != nil {
			t.Fatalf("Unregister() error = %v", err)
		}
	})

	err = proxy.UpdateConfig(ProxyConfig{
		ListenAddress: "127.0.0.1",
		HTTPPort:      occupiedPort,
		SOCKS5Port:    0,
		Password:      "new",
	})
	if err == nil {
		t.Fatal("UpdateConfig() error = nil")
	}
	if got := proxy.validCredential("wwan0", "old"); !got {
		t.Fatal("old credential is invalid after failed update")
	}
	if got := proxy.validCredential("wwan0", "new"); got {
		t.Fatal("new credential is valid after failed update")
	}
	status := proxy.Status("wwan0")
	if !status.Enabled {
		t.Fatal("proxy is disabled after failed update")
	}
	if status.Password != "old" {
		t.Fatalf("Status().Password = %q, want old", status.Password)
	}
	conn, err := net.DialTimeout("tcp", status.HTTPAddress, time.Second)
	if err != nil {
		t.Fatalf("DialTimeout() restored HTTP proxy error = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestProxyUnregisterClosesInterfaceSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "closes outbound connections for removed interface"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu    sync.Mutex
				peers = make(map[string]net.Conn)
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				local, peer := net.Pipe()
				mu.Lock()
				peers[interfaceName] = peer
				mu.Unlock()
				return local, nil
			})
			if _, err := proxy.Register("wwan0"); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			conn, err := proxy.dial(context.Background(), "wwan0", "tcp", "example.com:443")
			if err != nil {
				t.Fatalf("dial() error = %v", err)
			}
			defer conn.Close()

			mu.Lock()
			peer := peers["wwan0"]
			mu.Unlock()
			if peer == nil {
				t.Fatal("dial peer is nil")
			}
			defer peer.Close()

			if err := proxy.Unregister("wwan0"); err != nil {
				t.Fatalf("Unregister() error = %v", err)
			}
			waitConnClosed(t, peer)
		})
	}
}

func TestProxyRestartsAfterUnexpectedListenerExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "restarts listeners while interface remains active"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				return nil, errors.New("dial unused")
			})
			if _, err := proxy.Register("wwan0"); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			proxy.mu.Lock()
			httpServer := proxy.httpServer
			proxy.mu.Unlock()
			if httpServer == nil {
				t.Fatal("httpServer is nil")
			}

			proxy.handleServeExit("http", httpServer, nil, errors.New("listener stopped"))
			status := proxy.Status("wwan0")
			if !status.Enabled {
				t.Fatalf("Status().Enabled = false, want true")
			}
			if status.HTTPAddress == "" {
				t.Fatal("Status().HTTPAddress is empty")
			}
			if status.SOCKS5Address == "" {
				t.Fatal("Status().SOCKS5Address is empty")
			}
		})
	}
}

func TestHTTPProxyAuthentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		username   string
		password   string
		wantStatus int
	}{
		{
			name:       "valid credentials",
			username:   "wwan0",
			password:   "secret",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown interface",
			username:   "wwan1",
			password:   "secret",
			wantStatus: http.StatusProxyAuthRequired,
		},
		{
			name:       "wrong password",
			username:   "wwan0",
			password:   "wrong",
			wantStatus: http.StatusProxyAuthRequired,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := newIPv4HTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "ok")
			}))

			var (
				mu       sync.Mutex
				gotIface string
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				mu.Lock()
				gotIface = interfaceName
				mu.Unlock()
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			proxyURL, err := url.Parse("http://" + status.HTTPAddress)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			client := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
			}
			req, err := http.NewRequest(http.MethodGet, target.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			req.Header.Set("Proxy-Authorization", basicProxyAuth(tt.username, tt.password))

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				mu.Lock()
				if gotIface != tt.username {
					t.Fatalf("dial interface = %q, want %q", gotIface, tt.username)
				}
				mu.Unlock()
			}
		})
	}
}

func TestSOCKS5ProxyAuthentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		username  string
		password  string
		wantAuth  byte
		wantIface string
	}{
		{
			name:      "valid credentials",
			username:  "wwan0",
			password:  "secret",
			wantAuth:  0,
			wantIface: "wwan0",
		},
		{
			name:     "unknown interface",
			username: "wwan1",
			password: "secret",
			wantAuth: 1,
		},
		{
			name:     "wrong password",
			username: "wwan0",
			password: "wrong",
			wantAuth: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := newIPv4HTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "ok")
			}))

			var (
				mu       sync.Mutex
				gotIface string
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				mu.Lock()
				gotIface = interfaceName
				mu.Unlock()
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			authStatus, body := socks5HTTPGet(t, status.SOCKS5Address, tt.username, tt.password, target.URL)
			if authStatus != tt.wantAuth {
				t.Fatalf("SOCKS5 auth status = %d, want %d", authStatus, tt.wantAuth)
			}
			if tt.wantAuth != 0 {
				return
			}
			if !strings.Contains(body, "ok") {
				t.Fatalf("SOCKS5 response body = %q, want it to contain ok", body)
			}
			mu.Lock()
			if gotIface != tt.wantIface {
				t.Fatalf("dial interface = %q, want %q", gotIface, tt.wantIface)
			}
			mu.Unlock()
		})
	}
}

func TestSOCKS5ProxyDefersDomainResolutionToBoundDialer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "domain address reaches dialer unchanged"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := newIPv4HTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "ok")
			}))
			_, port, err := net.SplitHostPort(target.Listener.Addr().String())
			if err != nil {
				t.Fatalf("SplitHostPort() error = %v", err)
			}
			domainAddress := net.JoinHostPort("example.test", port)

			var (
				mu         sync.Mutex
				gotAddress string
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				mu.Lock()
				gotAddress = address
				mu.Unlock()
				if address == domainAddress {
					address = target.Listener.Addr().String()
				}
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			body := socks5DomainHTTPGet(t, status.SOCKS5Address, "wwan0", "secret", "example.test", port)
			if !strings.Contains(body, "ok") {
				t.Fatalf("SOCKS5 response body = %q, want it to contain ok", body)
			}
			mu.Lock()
			defer mu.Unlock()
			if gotAddress != domainAddress {
				t.Fatalf("dial address = %q, want %q", gotAddress, domainAddress)
			}
		})
	}
}

func TestSOCKS5ProxyUDPAssociate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "udp packets use authenticated interface"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			udpTarget := newUDPEchoServer(t)
			var (
				mu               sync.Mutex
				gotIface         string
				gotNetwork       string
				gotDeadlineCalls int
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				mu.Lock()
				gotIface = interfaceName
				gotNetwork = network
				mu.Unlock()
				var dialer net.Dialer
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				if network != "udp" {
					return conn, nil
				}
				return &deadlineConn{
					Conn: conn,
					onSetDeadline: func(time.Time) {
						mu.Lock()
						gotDeadlineCalls++
						mu.Unlock()
					},
				}, nil
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			body := socks5UDPEcho(t, status.SOCKS5Address, "wwan0", "secret", udpTarget, []byte("ping"))
			if body != "echo:ping" {
				t.Fatalf("UDP response = %q, want %q", body, "echo:ping")
			}
			mu.Lock()
			defer mu.Unlock()
			if gotIface != "wwan0" {
				t.Fatalf("dial interface = %q, want %q", gotIface, "wwan0")
			}
			if gotNetwork != "udp" {
				t.Fatalf("dial network = %q, want %q", gotNetwork, "udp")
			}
			if gotDeadlineCalls == 0 {
				t.Fatal("UDP target deadline was not refreshed")
			}
		})
	}
}

func TestSOCKS5ProxyUDPAssociatePinsClientSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "second udp socket cannot reuse association"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			udpTarget := newUDPEchoServer(t)
			var (
				mu        sync.Mutex
				dialCount int
			)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				if network == "udp" {
					mu.Lock()
					dialCount++
					mu.Unlock()
				}
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			control, relayUDPAddr := openSOCKS5UDPAssociation(t, status.SOCKS5Address, "wwan0", "secret")
			defer control.Close()

			client := newUDPClient(t)
			defer client.Close()
			sendSOCKS5UDPDatagram(t, client, relayUDPAddr, udpTarget, []byte("allowed"))
			response := readSOCKS5UDPDatagram(t, client)
			if string(response.Data) != "echo:allowed" {
				t.Fatalf("first UDP response = %q, want %q", response.Data, "echo:allowed")
			}

			intruder := newUDPClient(t)
			defer intruder.Close()
			if err := intruder.SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
				t.Fatalf("SetDeadline() error = %v", err)
			}
			sendSOCKS5UDPDatagram(t, intruder, relayUDPAddr, udpTarget, []byte("blocked"))
			var buf [1024]byte
			_, _, err = intruder.ReadFromUDP(buf[:])
			if !isProxyTimeoutError(err) {
				t.Fatalf("intruder ReadFromUDP() error = %v, want timeout", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if dialCount != 1 {
				t.Fatalf("UDP dial count = %d, want 1", dialCount)
			}
		})
	}
}

func TestSOCKS5ProxyUDPAssociateClosesOnUnregister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "unregister closes udp association control connection"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			udpTarget := newUDPEchoServer(t)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			control, relayUDPAddr := openSOCKS5UDPAssociation(t, status.SOCKS5Address, "wwan0", "secret")
			defer control.Close()

			client := newUDPClient(t)
			defer client.Close()
			sendSOCKS5UDPDatagram(t, client, relayUDPAddr, udpTarget, []byte("before"))
			response := readSOCKS5UDPDatagram(t, client)
			if string(response.Data) != "echo:before" {
				t.Fatalf("UDP response = %q, want %q", response.Data, "echo:before")
			}

			if err := proxy.Unregister("wwan0"); err != nil {
				t.Fatalf("Unregister() error = %v", err)
			}
			waitConnClosed(t, control)
		})
	}
}

func TestSOCKS5ProxyUDPAssociateClosesAfterIdle(t *testing.T) {
	tests := []struct {
		name        string
		idleTimeout time.Duration
	}{
		{name: "idle udp association closes control connection", idleTimeout: 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldIdleTimeout := proxyUDPAssociationIdleTimeout
			proxyUDPAssociationIdleTimeout = tt.idleTimeout
			t.Cleanup(func() {
				proxyUDPAssociationIdleTimeout = oldIdleTimeout
			})

			udpTarget := newUDPEchoServer(t)
			proxy := newProxyWithDial(ProxyConfig{
				ListenAddress: "127.0.0.1",
				Password:      "secret",
			}, func(ctx context.Context, interfaceName string, network string, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, address)
			})
			status, err := proxy.Register("wwan0")
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			t.Cleanup(func() {
				if err := proxy.Unregister("wwan0"); err != nil {
					t.Fatalf("Unregister() error = %v", err)
				}
			})

			control, relayUDPAddr := openSOCKS5UDPAssociation(t, status.SOCKS5Address, "wwan0", "secret")
			defer control.Close()

			client := newUDPClient(t)
			defer client.Close()
			sendSOCKS5UDPDatagram(t, client, relayUDPAddr, udpTarget, []byte("before-idle"))
			response := readSOCKS5UDPDatagram(t, client)
			if string(response.Data) != "echo:before-idle" {
				t.Fatalf("UDP response = %q, want %q", response.Data, "echo:before-idle")
			}

			waitConnClosed(t, control)
		})
	}
}

func socks5HTTPGet(t *testing.T, proxyAddress string, username string, password string, targetURL string) (byte, string) {
	t.Helper()

	conn, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		t.Fatalf("write methods error = %v", err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(conn, method); err != nil {
		t.Fatalf("read method error = %v", err)
	}
	if method[0] != 5 || method[1] != 2 {
		t.Fatalf("method response = %#v, want user/pass", method)
	}

	authRequest := []byte{1, byte(len(username))}
	authRequest = append(authRequest, username...)
	authRequest = append(authRequest, byte(len(password)))
	authRequest = append(authRequest, password...)
	if _, err := conn.Write(authRequest); err != nil {
		t.Fatalf("write auth error = %v", err)
	}
	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResponse); err != nil {
		t.Fatalf("read auth error = %v", err)
	}
	if authResponse[1] != 0 {
		return authResponse[1], ""
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		t.Fatalf("target host %q is not ipv4", host)
	}
	portNumber, err := net.LookupPort("tcp", port)
	if err != nil {
		t.Fatalf("LookupPort() error = %v", err)
	}
	connectRequest := []byte{5, 1, 0, 1}
	connectRequest = append(connectRequest, ip...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portNumber))
	connectRequest = append(connectRequest, portBytes...)
	if _, err := conn.Write(connectRequest); err != nil {
		t.Fatalf("write connect error = %v", err)
	}
	connectResponse := make([]byte, 10)
	if _, err := io.ReadFull(conn, connectResponse); err != nil {
		t.Fatalf("read connect error = %v", err)
	}
	if connectResponse[1] != 0 {
		t.Fatalf("connect response = %#v, want success", connectResponse)
	}

	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", u.Host); err != nil {
		t.Fatalf("write http request error = %v", err)
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return authResponse[1], string(data)
}

func socks5DomainHTTPGet(t *testing.T, proxyAddress string, username string, password string, host string, port string) string {
	t.Helper()

	conn, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		t.Fatalf("write methods error = %v", err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(conn, method); err != nil {
		t.Fatalf("read method error = %v", err)
	}
	if method[0] != 5 || method[1] != 2 {
		t.Fatalf("method response = %#v, want user/pass", method)
	}

	authRequest := []byte{1, byte(len(username))}
	authRequest = append(authRequest, username...)
	authRequest = append(authRequest, byte(len(password)))
	authRequest = append(authRequest, password...)
	if _, err := conn.Write(authRequest); err != nil {
		t.Fatalf("write auth error = %v", err)
	}
	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResponse); err != nil {
		t.Fatalf("read auth error = %v", err)
	}
	if authResponse[1] != 0 {
		t.Fatalf("auth response = %#v, want success", authResponse)
	}

	portNumber, err := net.LookupPort("tcp", port)
	if err != nil {
		t.Fatalf("LookupPort() error = %v", err)
	}
	connectRequest := []byte{5, 1, 0, 3, byte(len(host))}
	connectRequest = append(connectRequest, host...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portNumber))
	connectRequest = append(connectRequest, portBytes...)
	if _, err := conn.Write(connectRequest); err != nil {
		t.Fatalf("write connect error = %v", err)
	}
	connectResponse := make([]byte, 10)
	if _, err := io.ReadFull(conn, connectResponse); err != nil {
		t.Fatalf("read connect error = %v", err)
	}
	if connectResponse[1] != 0 {
		t.Fatalf("connect response = %#v, want success", connectResponse)
	}

	hostHeader := net.JoinHostPort(host, port)
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", hostHeader); err != nil {
		t.Fatalf("write http request error = %v", err)
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return string(data)
}

func socks5UDPEcho(t *testing.T, proxyAddress string, username string, password string, targetAddress string, payload []byte) string {
	t.Helper()

	control, relayUDPAddr := openSOCKS5UDPAssociation(t, proxyAddress, username, password)
	defer control.Close()

	client := newUDPClient(t)
	defer client.Close()
	sendSOCKS5UDPDatagram(t, client, relayUDPAddr, targetAddress, payload)
	response := readSOCKS5UDPDatagram(t, client)
	if response.DstAddr.String() != targetAddress {
		t.Fatalf("response address = %q, want %q", response.DstAddr.String(), targetAddress)
	}
	return string(response.Data)
}

func openSOCKS5UDPAssociation(t *testing.T, proxyAddress string, username string, password string) (net.Conn, *net.UDPAddr) {
	t.Helper()

	control, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if err := control.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		control.Close()
		t.Fatalf("SetDeadline() error = %v", err)
	}

	if _, err := control.Write([]byte{5, 1, 2}); err != nil {
		t.Fatalf("write methods error = %v", err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(control, method); err != nil {
		t.Fatalf("read method error = %v", err)
	}
	if method[0] != 5 || method[1] != 2 {
		t.Fatalf("method response = %#v, want user/pass", method)
	}

	authRequest := []byte{1, byte(len(username))}
	authRequest = append(authRequest, username...)
	authRequest = append(authRequest, byte(len(password)))
	authRequest = append(authRequest, password...)
	if _, err := control.Write(authRequest); err != nil {
		t.Fatalf("write auth error = %v", err)
	}
	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(control, authResponse); err != nil {
		t.Fatalf("read auth error = %v", err)
	}
	if authResponse[1] != 0 {
		t.Fatalf("auth response = %#v, want success", authResponse)
	}

	associateRequest := []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}
	if _, err := control.Write(associateRequest); err != nil {
		t.Fatalf("write associate error = %v", err)
	}
	reply, err := statute.ParseReply(control)
	if err != nil {
		control.Close()
		t.Fatalf("ParseReply() error = %v", err)
	}
	if reply.Response != statute.RepSuccess {
		control.Close()
		t.Fatalf("associate response = %d, want success", reply.Response)
	}
	if err := control.SetDeadline(time.Time{}); err != nil {
		control.Close()
		t.Fatalf("clear deadline error = %v", err)
	}
	relayAddress := reply.BndAddr.String()
	if len(reply.BndAddr.IP) != 0 && reply.BndAddr.IP.IsUnspecified() {
		host, _, err := net.SplitHostPort(proxyAddress)
		if err != nil {
			control.Close()
			t.Fatalf("SplitHostPort() error = %v", err)
		}
		relayAddress = net.JoinHostPort(host, fmt.Sprint(reply.BndAddr.Port))
	}
	relayUDPAddr, err := net.ResolveUDPAddr("udp", relayAddress)
	if err != nil {
		control.Close()
		t.Fatalf("ResolveUDPAddr() error = %v", err)
	}
	return control, relayUDPAddr
}

func newUDPClient(t *testing.T) *net.UDPConn {
	t.Helper()

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	if err := client.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		client.Close()
		t.Fatalf("SetDeadline() error = %v", err)
	}
	return client
}

func sendSOCKS5UDPDatagram(t *testing.T, client *net.UDPConn, relayUDPAddr *net.UDPAddr, targetAddress string, payload []byte) {
	t.Helper()

	datagram, err := statute.NewDatagram(targetAddress, payload)
	if err != nil {
		t.Fatalf("NewDatagram() error = %v", err)
	}
	if _, err := client.WriteToUDP(datagram.Bytes(), relayUDPAddr); err != nil {
		t.Fatalf("WriteToUDP() error = %v", err)
	}
}

func readSOCKS5UDPDatagram(t *testing.T, client *net.UDPConn) statute.Datagram {
	t.Helper()

	buf := make([]byte, 1024)
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP() error = %v", err)
	}
	response, err := statute.ParseDatagram(buf[:n])
	if err != nil {
		t.Fatalf("ParseDatagram() error = %v", err)
	}
	return response
}

func basicProxyAuth(username string, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

type deadlineConn struct {
	net.Conn
	onSetDeadline func(time.Time)
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	if c.onSetDeadline != nil {
		c.onSetDeadline(t)
	}
	return c.Conn.SetDeadline(t)
}

func waitConnClosed(t *testing.T, conn net.Conn) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		var data [1]byte
		_, err := conn.Read(data[:])
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Read() error = nil, want closed connection")
		}
	case <-time.After(time.Second):
		t.Fatal("connection was not closed")
	}
}

func newIPv4HTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0") // nolint: noctx
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func newUDPEchoServer(t *testing.T) string {
	t.Helper()

	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}
	server, err := net.ListenUDP("udp4", addr)
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for {
			n, clientAddr, err := server.ReadFromUDP(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				return
			}
			response := append([]byte("echo:"), buf[:n]...)
			if _, err := server.WriteToUDP(response, clientAddr); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Close() error = %v", err)
		}
		<-done
	})
	return server.LocalAddr().String()
}
