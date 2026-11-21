package ims

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const wifiProxyScope = "wifi_calling"
const wifiProxyKey = "socks5_proxies"

var (
	errInvalidWiFiProxy  = errors.New("invalid Wi-Fi Calling proxy")
	errWiFiProxyNotFound = errors.New("Wi-Fi Calling proxy not found")
	proxyHostname        = regexp.MustCompile("^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$")
)

// WiFiProxySettings contains the settings accepted when creating or editing a proxy.
type WiFiProxySettings struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

// WiFiProxySummary is the editable proxy metadata returned to a signed-in client.
type WiFiProxySummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	HasPassword bool   `json:"hasPassword"`
}

type wifiProxy struct {
	ID string `json:"id"`
	WiFiProxySettings
}

func (p wifiProxy) summary() WiFiProxySummary {
	return WiFiProxySummary{
		ID: p.ID, Name: p.Name, Host: p.Host, Port: p.Port, Username: p.Username, HasPassword: p.Password != "",
	}
}

func normalizeWiFiProxy(settings WiFiProxySettings, previous wifiProxy) (WiFiProxySettings, error) {
	settings.Name = strings.TrimSpace(settings.Name)
	settings.Host = strings.TrimSpace(settings.Host)
	if len(settings.Name) == 0 || len(settings.Name) > 128 {
		return WiFiProxySettings{}, fmt.Errorf("%w: name must be 1 to 128 bytes", errInvalidWiFiProxy)
	}
	if settings.Port == 0 {
		settings.Port = 1080
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return WiFiProxySettings{}, fmt.Errorf("%w: port must be between 1 and 65535", errInvalidWiFiProxy)
	}
	if addr, err := netip.ParseAddr(settings.Host); err == nil {
		settings.Host = addr.Unmap().String()
	} else if !validProxyHostname(settings.Host) {
		return WiFiProxySettings{}, fmt.Errorf("%w: enter a hostname or IP address", errInvalidWiFiProxy)
	}
	if settings.Username == "" {
		if settings.Password != "" {
			return WiFiProxySettings{}, fmt.Errorf("%w: username is required with a password", errInvalidWiFiProxy)
		}
		return settings, nil
	}
	if settings.Password == "" {
		settings.Password = previous.Password
	}
	if len(settings.Username) > 255 || len(settings.Password) == 0 || len(settings.Password) > 255 {
		return WiFiProxySettings{}, fmt.Errorf("%w: credentials must be 1 to 255 bytes each", errInvalidWiFiProxy)
	}
	return settings, nil
}

func validProxyHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !proxyHostname.MatchString(label) {
			return false
		}
	}
	return true
}

type wifiProxyCatalog struct {
	store *storage.Store
	mu    sync.Mutex
}

func (c *wifiProxyCatalog) load(ctx context.Context) ([]wifiProxy, error) {
	if c == nil || c.store == nil {
		return nil, ErrUnavailable
	}
	var proxies []wifiProxy
	if err := c.store.Get(ctx, wifiProxyScope, wifiProxyKey, &proxies); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("read Wi-Fi Calling proxies: %w", err)
	}
	return proxies, nil
}

func (c *wifiProxyCatalog) get(ctx context.Context, id string) (wifiProxy, error) {
	proxies, err := c.load(ctx)
	if err != nil {
		return wifiProxy{}, err
	}
	for _, proxy := range proxies {
		if proxy.ID == id {
			return proxy, nil
		}
	}
	return wifiProxy{}, errWiFiProxyNotFound
}

func (c *wifiProxyCatalog) save(ctx context.Context, id string, settings WiFiProxySettings) (WiFiProxySummary, error) {
	if c == nil {
		return WiFiProxySummary{}, ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	proxies, err := c.load(ctx)
	if err != nil {
		return WiFiProxySummary{}, err
	}
	var previous wifiProxy
	index := len(proxies)
	if id != "" {
		index = slices.IndexFunc(proxies, func(proxy wifiProxy) bool { return proxy.ID == id })
		if index < 0 {
			return WiFiProxySummary{}, errWiFiProxyNotFound
		}
		previous = proxies[index]
	} else {
		id = rand.Text()
	}
	settings, err = normalizeWiFiProxy(settings, previous)
	if err != nil {
		return WiFiProxySummary{}, err
	}
	proxy := wifiProxy{ID: id, WiFiProxySettings: settings}
	if index == len(proxies) {
		proxies = append(proxies, proxy)
	} else {
		proxies[index] = proxy
	}
	if err := c.store.Put(ctx, wifiProxyScope, wifiProxyKey, proxies); err != nil {
		return WiFiProxySummary{}, fmt.Errorf("save Wi-Fi Calling proxy: %w", err)
	}
	return proxy.summary(), nil
}
