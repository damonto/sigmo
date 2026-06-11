//go:build esim_transfer || wifi_calling

package websheet

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultTTL           = 10 * time.Minute
	defaultClientTimeout = 45 * time.Second
	defaultBasePath      = "/api/v1/websheets"
	callbackURLToken     = "{{CALLBACK_URL}}"
	targetQueryParam     = "target_query"
)

var (
	ErrNotFound  = errors.New("websheet session not found")
	ErrExpired   = errors.New("websheet session expired")
	ErrUnsafeURL = errors.New("websheet URL is not allowed")
)

//go:embed bridge.js
var websheetBridgeJS string

type Config struct {
	TTL               time.Duration
	BasePath          string
	AllowPrivateHosts bool
	Now               func() time.Time
}

type Broker struct {
	mu                sync.Mutex
	sessions          map[string]*Session
	ttl               time.Duration
	basePath          string
	allowPrivateHosts bool
	now               func() time.Time
}

type Request struct {
	URL         string
	UserData    string
	ContentType string
	Title       string
}

type Info struct {
	ID       string `json:"id"`
	EmbedURL string `json:"embedUrl"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url"`
	Method   string `json:"method"`
}

type Callback struct {
	Source             string `json:"source,omitempty"`
	Controller         string `json:"controller,omitempty"`
	Method             string `json:"method,omitempty"`
	Event              string `json:"event"`
	ResultCode         string `json:"resultCode,omitempty"`
	Href               string `json:"href,omitempty"`
	ActivationCode     string `json:"activationCode,omitempty"`
	DefaultSMDPAddress string `json:"defaultSmdpAddress,omitempty"`
	SMDPFQDN           string `json:"smdpFqdn,omitempty"`
	ICCID              string `json:"iccid,omitempty"`
	IMEI               string `json:"imei,omitempty"`
	NextAction         string `json:"nextAction,omitempty"`
}

type Session struct {
	id                string
	target            *url.URL
	userData          string
	contentType       string
	title             string
	expiresAt         time.Time
	basePath          string
	client            *http.Client
	now               func() time.Time
	allowPrivateHosts bool

	callbackCh chan Callback
	doneCh     chan struct{}
	doneOnce   sync.Once
}

func New(cfg Config) *Broker {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	basePath := strings.TrimRight(cfg.BasePath, "/")
	if basePath == "" {
		basePath = defaultBasePath
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Broker{
		sessions:          make(map[string]*Session),
		ttl:               ttl,
		basePath:          basePath,
		allowPrivateHosts: cfg.AllowPrivateHosts,
		now:               now,
	}
}

func (b *Broker) Create(ctx context.Context, req Request) (*Session, error) {
	if b == nil {
		return nil, errors.New("websheet broker is nil")
	}
	target, err := parseAllowedURL(ctx, req.URL, b.allowPrivateHosts)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create websheet cookie jar: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	session := &Session{
		id:                id,
		target:            target,
		userData:          strings.TrimSpace(req.UserData),
		contentType:       strings.TrimSpace(req.ContentType),
		title:             strings.TrimSpace(req.Title),
		expiresAt:         b.now().Add(b.ttl),
		basePath:          b.basePath,
		now:               b.now,
		allowPrivateHosts: b.allowPrivateHosts,
		callbackCh:        make(chan Callback, 1),
		doneCh:            make(chan struct{}),
	}
	session.client = &http.Client{
		Jar:     jar,
		Timeout: defaultClientTimeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			_, err := parseAllowedURL(r.Context(), r.URL.String(), b.allowPrivateHosts)
			return err
		},
	}

	b.mu.Lock()
	b.sessions[id] = session
	b.mu.Unlock()
	return session, nil
}

func (b *Broker) Get(id string) (*Session, error) {
	if b == nil {
		return nil, ErrNotFound
	}
	b.mu.Lock()
	session := b.sessions[id]
	if session != nil && session.expired() {
		delete(b.sessions, id)
		session = nil
	}
	b.mu.Unlock()
	if session == nil {
		return nil, ErrNotFound
	}
	return session, nil
}

func (b *Broker) Delete(id string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.sessions, id)
	b.mu.Unlock()
}

func (s *Session) Info() Info {
	return Info{
		ID:       s.id,
		EmbedURL: s.basePath + "/" + url.PathEscape(s.id),
		Title:    s.title,
		URL:      s.target.String(),
		Method:   s.method(),
	}
}

func (s *Session) WaitCallback(ctx context.Context) (Callback, error) {
	select {
	case callback := <-s.callbackCh:
		return callback, nil
	case <-s.doneCh:
		return Callback{Event: "finishFlow"}, nil
	case <-ctx.Done():
		return Callback{}, ctx.Err()
	}
}

func (s *Session) WaitDone(ctx context.Context) error {
	select {
	case <-s.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) Done() {
	s.doneOnce.Do(func() {
		close(s.doneCh)
	})
}

func (s *Session) Callback(callback Callback) {
	sendLatest(s.callbackCh, callback)
}

func (s *Session) ServeBootstrap(w http.ResponseWriter, r *http.Request) error {
	if s.expired() {
		return ErrExpired
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	target := *s.target
	if s.method() == http.MethodGet && s.userData != "" {
		appendRawQuery(&target, s.userData)
	}
	proxyURL := s.proxyURL(target.String(), token)
	if s.method() == http.MethodGet {
		http.Redirect(w, r, proxyURL, http.StatusFound)
		return nil
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := io.WriteString(w, s.postBootstrapHTML(proxyURL))
	return err
}

func (s *Session) Proxy(w http.ResponseWriter, r *http.Request) error {
	if s.expired() {
		return ErrExpired
	}
	rawTarget := strings.TrimSpace(r.URL.Query().Get("target"))
	if rawTarget == "" {
		rawTarget = s.proxyPathTarget(r)
	}
	if rawTarget == "" {
		rawTarget = s.target.String()
	}
	target, err := parseAllowedURL(r.Context(), rawTarget, s.allowPrivateHosts)
	if err != nil {
		return err
	}
	if callback, ok := callbackFromURL(target); ok {
		s.Callback(callback)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, err := io.WriteString(w, callbackHTML())
		return err
	}

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		return fmt.Errorf("create websheet request: %w", err)
	}
	copyProxyHeaders(req.Header, r.Header)
	origin := targetOrigin(target)
	if origin != "" {
		req.Header.Set("Referer", origin+"/")
		if body != nil {
			req.Header.Set("Origin", origin)
		}
	}
	if req.Header.Get("Content-Type") == "" && s.contentType != "" {
		req.Header.Set("Content-Type", s.contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("proxy websheet request: %w", err)
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	contentType := resp.Header.Get("Content-Type")
	if isHTML(contentType) {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read websheet response: %w", err)
		}
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		base := target
		if resp.Request != nil && resp.Request.URL != nil {
			base = resp.Request.URL
		}
		html := s.rewriteHTML(string(data), base, token, requestOrigin(r), shouldInjectBridge(r))
		_, err = io.WriteString(w, html)
		return err
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (s *Session) method() string {
	if s.contentType != "" {
		return http.MethodPost
	}
	return http.MethodGet
}

func (s *Session) expired() bool {
	return !s.now().Before(s.expiresAt)
}

func (s *Session) proxyURL(rawTarget string, token string) string {
	if target, err := url.Parse(rawTarget); err == nil && target.Scheme != "" && target.Host != "" {
		values := url.Values{}
		if target.RawQuery != "" {
			values.Set(targetQueryParam, target.RawQuery)
		}
		if token != "" {
			values.Set("token", token)
		}
		proxyPath := s.basePath + "/" + url.PathEscape(s.id) + "/proxy/" + target.Scheme + "/" + target.Host + target.EscapedPath()
		if target.EscapedPath() == "" {
			proxyPath += "/"
		}
		if encoded := values.Encode(); encoded != "" {
			proxyPath += "?" + encoded
		}
		return proxyPath
	}
	values := url.Values{}
	values.Set("target", rawTarget)
	if token != "" {
		values.Set("token", token)
	}
	return s.basePath + "/" + url.PathEscape(s.id) + "/proxy?" + values.Encode()
}

func (s *Session) proxyPathTarget(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	prefix := s.basePath + "/" + url.PathEscape(s.id) + "/proxy/"
	escapedPath := r.URL.EscapedPath()
	if !strings.HasPrefix(escapedPath, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(escapedPath, prefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	target := url.URL{
		Scheme: parts[0],
		Host:   parts[1],
	}
	if len(parts) == 3 {
		pathValue, err := url.PathUnescape("/" + parts[2])
		if err != nil {
			return ""
		}
		target.Path = pathValue
	} else {
		target.Path = "/"
	}
	target.RawQuery = r.URL.Query().Get(targetQueryParam)
	return target.String()
}

func (s *Session) callbackURL(token string) string {
	values := url.Values{}
	if token != "" {
		values.Set("token", token)
	}
	path := s.basePath + "/" + url.PathEscape(s.id) + "/callback"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}

func (s *Session) postBootstrapHTML(action string) string {
	values, err := url.ParseQuery(strings.TrimLeft(s.userData, "?&"))
	var body bytes.Buffer
	body.WriteString("<!doctype html><html><body>")
	body.WriteString(`<form id="websheet" method="post" action="`)
	body.WriteString(html.EscapeString(action))
	body.WriteString(`">`)
	if err == nil {
		for key, items := range values {
			for _, item := range items {
				body.WriteString(`<input type="hidden" name="`)
				body.WriteString(html.EscapeString(key))
				body.WriteString(`" value="`)
				body.WriteString(html.EscapeString(item))
				body.WriteString(`">`)
			}
		}
	} else if s.userData != "" {
		body.WriteString(`<input type="hidden" name="payload" value="`)
		body.WriteString(html.EscapeString(s.userData))
		body.WriteString(`">`)
	}
	body.WriteString(`</form><script>document.getElementById("websheet").submit();</script></body></html>`)
	return body.String()
}

func (s *Session) rewriteHTML(doc string, base *url.URL, token string, publicOrigin string, injectBridge bool) string {
	docBase := s.documentBaseURL(doc, base)
	rewritten := attrURLPattern.ReplaceAllStringFunc(doc, func(match string) string {
		parts := attrURLPattern.FindStringSubmatch(match)
		if len(parts) < 6 {
			return match
		}
		attr := parts[1]
		raw := parts[3]
		if raw == "" {
			raw = parts[4]
		}
		if raw == "" {
			raw = parts[5]
		}
		next, ok := s.rewriteURL(raw, docBase, token, publicOrigin)
		if !ok {
			return match
		}
		return attr + `="` + html.EscapeString(next) + `"`
	})
	if !injectBridge {
		return rewritten
	}
	script := s.bridgeScript(token, publicOrigin, docBase)
	lower := strings.ToLower(rewritten)
	if idx := strings.Index(lower, "<head"); idx >= 0 {
		if end := strings.Index(rewritten[idx:], ">"); end >= 0 {
			insertAt := idx + end + 1
			return rewritten[:insertAt] + script + rewritten[insertAt:]
		}
	}
	if idx := strings.LastIndex(lower, "</head>"); idx >= 0 {
		return rewritten[:idx] + script + rewritten[idx:]
	}
	if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
		return rewritten[:idx] + script + rewritten[idx:]
	}
	return script + rewritten
}

func shouldInjectBridge(r *http.Request) bool {
	if r == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest"))) {
	case "", "document", "iframe", "frame":
		return true
	default:
		return false
	}
}

func (s *Session) documentBaseURL(doc string, fallback *url.URL) *url.URL {
	match := baseHrefPattern.FindStringSubmatch(doc)
	if len(match) < 5 {
		return fallback
	}
	raw := match[2]
	if raw == "" {
		raw = match[3]
	}
	if raw == "" {
		raw = match[4]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	return fallback.ResolveReference(ref)
}

func (s *Session) rewriteURL(raw string, base *url.URL, token string, publicOrigin string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return raw, false
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") || strings.HasPrefix(lower, "data:") {
		return raw, false
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw, false
	}
	target := base.ResolveReference(ref)
	return absoluteLocalURL(s.proxyURL(target.String(), token), publicOrigin), true
}

func (s *Session) bridgeScript(token string, publicOrigin string, carrierBase *url.URL) string {
	callbackURL := absoluteLocalURL(s.callbackURL(token), publicOrigin)
	script := strings.ReplaceAll(websheetBridgeJS, callbackURLToken, jsString(callbackURL))
	script = strings.ReplaceAll(script, "{{ABSOLUTE_PATH_PROXY_PREFIX}}", jsString(s.absolutePathProxyPrefix(carrierBase, token, publicOrigin)))
	return "<script>\n" + script + "\n</script>"
}

func (s *Session) absolutePathProxyPrefix(carrierBase *url.URL, token string, publicOrigin string) string {
	origin := targetOrigin(carrierBase)
	if origin == "" {
		return ""
	}
	return strings.TrimRight(absoluteLocalURL(s.proxyURL(origin+"/", token), publicOrigin), "/")
}

var (
	attrURLPattern  = regexp.MustCompile("(?i)\\b(href|src|action)=(\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")
	baseHrefPattern = regexp.MustCompile("(?is)<base\\b[^>]*\\bhref=(\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))[^>]*>")
)

func requestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	return proto + "://" + host
}

func absoluteLocalURL(raw string, publicOrigin string) string {
	publicOrigin = strings.TrimRight(strings.TrimSpace(publicOrigin), "/")
	if publicOrigin == "" || !strings.HasPrefix(raw, "/") {
		return raw
	}
	return publicOrigin + raw
}

func copyProxyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "host", "referer", "origin", "content-length", "accept-encoding",
			"connection", "sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site", "sec-fetch-user":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func targetOrigin(target *url.URL) string {
	if target == nil || target.Scheme == "" || target.Host == "" {
		return ""
	}
	return target.Scheme + "://" + target.Host
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "content-security-policy", "content-security-policy-report-only", "x-frame-options", "content-length", "set-cookie":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHTML(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "html")
	}
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func parseAllowedURL(ctx context.Context, raw string, allowPrivate bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: URL is required", ErrUnsafeURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse websheet URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q", ErrUnsafeURL, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", ErrUnsafeURL)
	}
	if allowPrivate {
		return parsed, nil
	}
	if isLocalHostname(host) {
		return nil, fmt.Errorf("%w: local host %q", ErrUnsafeURL, host)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if unsafeIP(ip) {
			return nil, fmt.Errorf("%w: private address %q", ErrUnsafeURL, host)
		}
		return parsed, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve websheet host: %w", err)
	}
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if ok && unsafeIP(ip.Unmap()) {
			return nil, fmt.Errorf("%w: private address %q", ErrUnsafeURL, host)
		}
	}
	return parsed, nil
}

func isLocalHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func unsafeIP(ip netip.Addr) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func appendRawQuery(target *url.URL, raw string) {
	raw = strings.TrimLeft(raw, "?&")
	if raw == "" {
		return
	}
	if target.RawQuery == "" {
		target.RawQuery = raw
		return
	}
	target.RawQuery += "&" + raw
}

func callbackFromURL(target *url.URL) (Callback, bool) {
	if !strings.Contains(target.Path, "/_callback") {
		return Callback{}, false
	}
	query := target.Query()
	event := firstValue(query, "event", "callback", "action", "method")
	if event == "" {
		parts := strings.Split(strings.Trim(target.Path, "/"), "/")
		if len(parts) > 0 {
			event = parts[len(parts)-1]
		}
	}
	if normalize(event) == "callback" || normalize(event) == "esim" || event == "" {
		event = callbackEventFromQuery(query)
	}
	return Callback{
		Event:              event,
		ActivationCode:     firstValue(query, "activationCode", "activation_code"),
		DefaultSMDPAddress: firstValue(query, "defaultSmdpAddress", "default_smdp_address"),
		SMDPFQDN:           firstValue(query, "smdpFqdn", "smdp", "defaultSmdpAddress"),
		ICCID:              firstValue(query, "iccid", "ICCID"),
		IMEI:               firstValue(query, "imei", "IMEI"),
		NextAction:         firstValue(query, "nextAction", "next_action"),
	}, true
}

func callbackEventFromQuery(values url.Values) string {
	switch {
	case firstValue(values, "activationCode", "activation_code") != "":
		return "profileReadyWithActivationCode"
	case firstValue(values, "defaultSmdpAddress", "smdp", "smdpFqdn") != "":
		return "profileReadyWithDefaultSmdp"
	case firstValue(values, "nextAction", "next_action") != "":
		return "finishFlow"
	default:
		return "finishFlow"
	}
}

func normalize(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstValue(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func callbackHTML() string {
	return `<!doctype html><html><body><script>try{window.parent.postMessage({type:"sigmo-websheet-callback"},"*")}catch(_){}</script>Carrier flow returned to Sigmo.</body></html>`
}

func jsString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	return `"` + value + `"`
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate websheet id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func sendLatest[T any](ch chan T, value T) {
	select {
	case ch <- value:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- value
	}
}
