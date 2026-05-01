package internet

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (p *Proxy) startHTTPProxyLocked() error {
	listener, err := net.Listen("tcp", net.JoinHostPort(p.cfg.ListenAddress, strconv.Itoa(p.cfg.HTTPPort))) //nolint:noctx
	if err != nil {
		return proxyStartError("listen http proxy", err)
	}

	server := &http.Server{
		Handler:           http.HandlerFunc(p.serveHTTPProxy),
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.httpServer = server
	p.httpListener = listener
	p.httpAddress = listener.Addr().String()

	go func() {
		err := server.Serve(listener)
		if ignoreHTTPProxyServeError(err) {
			return
		}
		p.handleServeExit("http", server, nil, err)
	}()
	return nil
}

func ignoreHTTPProxyServeError(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)
}

func (p *Proxy) serveHTTPProxy(w http.ResponseWriter, r *http.Request) {
	username, ok := p.authenticateHTTP(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="sigmo"`)
		http.Error(w, http.StatusText(http.StatusProxyAuthRequired), http.StatusProxyAuthRequired)
		return
	}

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r, username)
		return
	}
	p.handleForward(w, r, username)
}

func (p *Proxy) authenticateHTTP(r *http.Request) (string, bool) {
	username, password, ok := parseProxyBasicAuth(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return "", false
	}
	return username, p.validCredential(username, password)
}

func parseProxyBasicAuth(header string) (string, string, bool) {
	scheme, value, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(data), ":")
	if !ok {
		return "", "", false
	}
	return username, password, true
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request, username string) {
	targetAddress, ok := connectAddress(r)
	if !ok {
		http.Error(w, "bad CONNECT address", http.StatusBadRequest)
		return
	}

	target, err := p.dial(r.Context(), username, "tcp", targetAddress)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		closeProxyPipeConn(target)
		http.Error(w, "hijacking is not supported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		closeProxyPipeConn(target)
		return
	}

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		closeProxyPipeConn(client)
		closeProxyPipeConn(target)
		return
	}
	proxyTCP(client, target)
}

func connectAddress(r *http.Request) (string, bool) {
	address := r.Host
	if r.URL != nil && r.URL.Host != "" {
		address = r.URL.Host
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return "", false
	}
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address, true
	}
	if strings.Contains(address, ":") {
		return "", false
	}
	return net.JoinHostPort(address, "443"), true
}

func proxyTCP(client net.Conn, target net.Conn) {
	done := make(chan struct{}, 2)
	copyAndClose := func(dst net.Conn, src net.Conn) {
		_, _ = io.Copy(dst, src)
		closeProxyPipeConn(dst)
		closeProxyPipeConn(src)
		done <- struct{}{}
	}
	go copyAndClose(target, client)
	go copyAndClose(client, target)
	<-done
}

func closeProxyPipeConn(conn net.Conn) {
	_ = ignoreProxyCloseError(conn.Close())
}

func (p *Proxy) handleForward(w http.ResponseWriter, r *http.Request, username string) {
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	outReq.Header = cloneProxyHeader(r.Header)
	removeHopByHopHeaders(outReq.Header)

	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return p.dial(ctx, username, network, address)
		},
	}
	defer transport.CloseIdleConnections()

	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyProxyHeader(w.Header(), resp.Header)
	removeHopByHopHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return
	}
}

func cloneProxyHeader(header http.Header) http.Header {
	clone := make(http.Header, len(header))
	copyProxyHeader(clone, header)
	return clone
}

func copyProxyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
}

func removeHopByHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				header.Del(name)
			}
		}
	}
	for name := range header {
		if isHopByHopHeader(name) {
			header.Del(name)
		}
	}
}

func isHopByHopHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade":
		return true
	default:
		return false
	}
}
