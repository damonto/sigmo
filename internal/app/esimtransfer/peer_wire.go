package esimtransfer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const peerVersion = 1

var errPeerUnsupported = errors.New("this Sigmo service does not support remote transfer")
var errPeerEnded = errors.New("remote transfer ended; check both devices before retrying")
var errPeerResultUncertain = errors.New("remote transfer result is uncertain; check both devices before retrying")

var peerVPNPrefix = netip.MustParsePrefix("100.64.0.0/10")

// These are transport DTOs, not a general-purpose remote card protocol.
type peerMessage struct {
	Type        string           `json:"type"`
	ID          uint64           `json:"id,omitempty"`
	Code        string           `json:"code,omitempty"`
	Version     int              `json:"version,omitempty"`
	Target      *peerTarget      `json:"target,omitempty"`
	Profile     *ProfileResponse `json:"profile,omitempty"`
	SourceName  string           `json:"sourceName,omitempty"`
	SourceEID   string           `json:"sourceEid,omitempty"`
	Device      *peerDevice      `json:"device,omitempty"`
	Identity    *peerIdentity    `json:"identity,omitempty"`
	AKA         *peerAKARequest  `json:"aka,omitempty"`
	AKAResponse *peerAKAResponse `json:"akaResponse,omitempty"`
	ICCID       string           `json:"iccid,omitempty"`
	Message     string           `json:"message,omitempty"`
	Outcome     string           `json:"outcome,omitempty"`
	Stage       string           `json:"stage,omitempty"`
	Requests    []peerRequest    `json:"requests,omitempty"`
}

type peerCatalogSource struct {
	Source   SourceResponse    `json:"source"`
	Profiles []ProfileResponse `json:"profiles"`
	EIDs     map[string]string `json:"eids"`
}

type peerSelection struct {
	SourceType SourceType `json:"sourceType"`
	SourceID   string     `json:"sourceId"`
	ProfileID  string     `json:"profileId"`
	SourceIMEI string     `json:"sourceImei,omitempty"`
}

// peerWire has one reader and a serialized writer. Context cancellation closes
// the socket, including when an in-flight card operation uses a background ctx.
type peerWire struct {
	ctx       context.Context
	cancel    context.CancelFunc
	conn      *websocket.Conn
	messages  chan peerMessage
	writeMu   sync.Mutex
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func newPeerWire(ctx context.Context, conn *websocket.Conn, allowed func() bool) *peerWire {
	ctx, cancel := context.WithCancel(ctx)
	w := &peerWire{ctx: ctx, cancel: cancel, conn: conn, messages: make(chan peerMessage, 4)}
	conn.SetReadLimit(4 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	w.wg.Go(func() {
		defer stopClose()
		defer conn.Close()
		defer cancel()
		for {
			var message peerMessage
			if err := conn.ReadJSON(&message); err != nil {
				return
			}
			select {
			case w.messages <- message:
			case <-ctx.Done():
				return
			default:
				return
			}
		}
	})
	w.wg.Go(func() {
		defer conn.Close()
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if allowed != nil && !allowed() {
					cancel()
					return
				}
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					cancel()
					return
				}
			}
		}
	})
	return w
}

func (w *peerWire) close() {
	w.closeOnce.Do(func() {
		w.cancel()
		_ = w.conn.Close() // A terminal connection has no recovery on close failure.
		w.wg.Wait()

	})
}
func (w *peerWire) send(message peerMessage) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.ctx.Err() != nil {
		return w.ctx.Err()
	}
	if err := w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		w.cancel()
		return err
	}
	if err := w.conn.WriteJSON(message); err != nil {
		w.cancel()
		return err
	}
	return nil
}
func (w *peerWire) receive(ctx context.Context) (peerMessage, error) {
	select {
	case <-w.ctx.Done():
		return peerMessage{}, errors.New("remote transfer connection lost; check both devices before retrying")
	case <-ctx.Done():
		return peerMessage{}, ctx.Err()
	case message := <-w.messages:
		if w.ctx.Err() != nil {
			return peerMessage{}, w.ctx.Err()
		}
		if message.Type == "error" {
			// Never forward arbitrary peer text into TS.43 error logs.
			switch message.Code {
			case "unsupported":
				return peerMessage{}, errPeerUnsupported
			case "declined":
				return peerMessage{}, errPeerDeclined
			case "expired":
				return peerMessage{}, errPeerExpired
			default:
				return peerMessage{}, errPeerEnded
			}
		}
		return message, nil
	}
}

func peerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid Sigmo service address")
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/api/v1") + "/api/v1/esim-transfers/remote-sessions"
	u.RawPath = ""
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	return u, nil
}

func privatePeerAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsPrivate() || address.IsLoopback() || peerVPNPrefix.Contains(address)
}

func dialPeer(ctx context.Context, raw string) (*websocket.Conn, error) {
	u, err := peerURL(raw)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	if u.Scheme == "ws" {
		// Validate resolved addresses at the actual dial, avoiding DNS rebinding and
		// redirects. Plain HTTP is restricted to local/private and VPN addresses.
		dialer.Proxy = nil
		dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range addresses {
				if !privatePeerAddress(ip) {
					return nil, errors.New("HTTP Sigmo addresses must resolve to a private network")
				}
			}
			var last error
			for _, ip := range addresses {
				conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last == nil {
				last = errors.New("Sigmo address has no network addresses")
			}
			return nil, last
		}
	}
	conn, response, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, errPeerUnsupported
		}
		// Dial errors can include the address. Keep credentials and remote replies out
		// of UI messages and logs, including reverse proxy authentication responses.
		if response != nil {
			return nil, errors.New("Sigmo service rejected the connection (HTTP " + strconv.Itoa(response.StatusCode) + ")")
		}
		return nil, errors.New("cannot connect to the Sigmo service")
	}
	return conn, nil
}
