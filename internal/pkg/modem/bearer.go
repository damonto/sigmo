package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	ErrUnsupportedBearerAuth   = errors.New("bearer authentication method is unsupported")
	ErrUnsupportedBearerIPType = errors.New("bearer IP type is unsupported")
)

type BearerIPMethod uint32

const (
	BearerIPMethodUnknown BearerIPMethod = iota
	BearerIPMethodPPP
	BearerIPMethodStatic
	BearerIPMethodDHCP
)

func (m BearerIPMethod) String() string {
	switch m {
	case BearerIPMethodPPP:
		return "ppp"
	case BearerIPMethodStatic:
		return "static"
	case BearerIPMethodDHCP:
		return "dhcp"
	default:
		return "unknown"
	}
}

type Bearer struct {
	modem      *Modem
	core       *wwanmodem.Bearer
	mu         sync.RWMutex
	infoValue  wwanmodem.BearerInfo
	properties BearerProperties
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

type BearerIPConfig struct {
	Method  BearerIPMethod
	Address string
	Prefix  uint32
	Gateway string
	DNS     []string
	MTU     uint32
}

func (c BearerIPConfig) ConfiguredAddress() bool {
	return (c.Method == BearerIPMethodStatic || c.Method == BearerIPMethodDHCP) && strings.TrimSpace(c.Address) != ""
}

type BearerStats struct {
	RXBytes  uint64
	TXBytes  uint64
	Duration uint32
}

type BearerProperties struct {
	APN         string
	IPType      string
	Username    string
	Password    string
	AllowedAuth string
}

func (m *Modem) ConnectBearer(ctx context.Context, prefs BearerProperties) (*Bearer, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	ipFamily, err := semanticIPFamily(prefs.IPType)
	if err != nil {
		return nil, err
	}
	auth, err := semanticAuthentication(prefs.AllowedAuth)
	if err != nil {
		return nil, err
	}
	core, err := m.core.Connect(ctx, wwanmodem.ConnectConfig{APN: strings.TrimSpace(prefs.APN), IPFamily: ipFamily, Username: strings.TrimSpace(prefs.Username), Password: prefs.Password, Authentication: auth})
	if err != nil {
		return nil, err
	}
	prefs.APN = strings.TrimSpace(prefs.APN)
	prefs.IPType = normalizeIPType(prefs.IPType)
	prefs.Username = strings.TrimSpace(prefs.Username)
	prefs.AllowedAuth = normalizeAuthenticationName(prefs.AllowedAuth)
	return m.bearerAdapter(ctx, core, prefs), nil
}

func (m *Modem) DisconnectBearer(ctx context.Context, id uint64) error {
	bearer, ok := m.Bearer(ctx, id)
	if !ok {
		return nil
	}
	return bearer.Disconnect(ctx)
}

func (m *Modem) DeleteBearer(ctx context.Context, id uint64) error {
	return m.DisconnectBearer(ctx, id)
}

func (m *Modem) Bearers(ctx context.Context) ([]*Bearer, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]*Bearer, 0)
	active := make(map[uint64]struct{})
	for _, core := range m.core.Bearers() {
		bearer := m.bearerAdapter(ctx, core, bearerPropertiesFromInfo(core.Info()))
		active[bearer.Path()] = struct{}{}
		result = append(result, bearer)
	}
	m.removeMissingBearerAdapters(active)
	return result, nil
}

func (m *Modem) Bearer(ctx context.Context, id uint64) (*Bearer, bool) {
	if m == nil || m.core == nil {
		return nil, false
	}
	core, ok := m.core.Bearer(id)
	if !ok {
		return nil, false
	}
	return m.bearerAdapter(ctx, core, bearerPropertiesFromInfo(core.Info())), true
}

func (m *Modem) bearerAdapter(ctx context.Context, core *wwanmodem.Bearer, properties BearerProperties) *Bearer {
	if m == nil || core == nil {
		return nil
	}
	info := cloneBearerInfo(core.Info())
	m.bearerMu.Lock()
	if m.bearers == nil {
		m.bearers = make(map[uint64]*Bearer)
	}
	if existing := m.bearers[info.ID]; existing != nil {
		existing.updateInfo(info)
		existing.mergeProperties(properties)
		m.bearerMu.Unlock()
		return existing
	}
	b := &Bearer{modem: m, core: core, infoValue: info, properties: properties}
	watchCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	b.cancel = cancel
	b.wg.Add(1)
	m.bearers[info.ID] = b
	m.bearerMu.Unlock()
	go b.watch(watchCtx)
	return b
}

func (m *Modem) removeMissingBearerAdapters(active map[uint64]struct{}) {
	if m == nil {
		return
	}
	var stale []*Bearer
	m.bearerMu.Lock()
	for id, bearer := range m.bearers {
		if _, ok := active[id]; ok {
			continue
		}
		delete(m.bearers, id)
		stale = append(stale, bearer)
	}
	m.bearerMu.Unlock()
	for _, bearer := range stale {
		bearer.closeAdapter()
	}
}

func (m *Modem) removeBearerAdapter(id uint64, expected *Bearer) {
	if m == nil {
		return
	}
	m.bearerMu.Lock()
	if m.bearers[id] == expected {
		delete(m.bearers, id)
	}
	m.bearerMu.Unlock()
}

func (m *Modem) closeBearerAdapters() {
	if m == nil {
		return
	}
	m.bearerMu.Lock()
	bearers := make([]*Bearer, 0, len(m.bearers))
	for _, bearer := range m.bearers {
		bearers = append(bearers, bearer)
	}
	m.bearers = nil
	m.bearerMu.Unlock()
	for _, bearer := range bearers {
		bearer.closeAdapter()
	}
}

func (b *Bearer) watch(ctx context.Context) {
	defer b.wg.Done()
	for ctx.Err() == nil {
		stream, err := b.core.Watch(ctx)
		if err == nil {
			err = b.consume(ctx, stream)
		}
		if ctx.Err() != nil || errors.Is(err, wwanmodem.ErrClosed) {
			return
		}
		slog.Warn("bearer watcher stopped", "bearer", b.Path(), "generation", b.modem.Generation(), "error", err)
		if err := sleepContext(ctx, modemWatchRetryDelay); err != nil {
			return
		}
	}
}

func (b *Bearer) consume(ctx context.Context, stream <-chan wwanmodem.Result[wwanmodem.BearerEvent]) error {
	if stream == nil {
		return errors.New("bearer watcher returned a nil stream")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-stream:
			if !ok {
				return errors.New("bearer stream closed")
			}
			if result.Err != nil {
				return result.Err
			}
			b.updateInfo(result.Value.Info)
		}
	}
}

func (b *Bearer) updateInfo(info wwanmodem.BearerInfo) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.infoValue = cloneBearerInfo(info)
	if b.properties.APN == "" {
		b.properties.APN = strings.TrimSpace(info.APN)
	}
	b.mu.Unlock()
}

func (b *Bearer) mergeProperties(properties BearerProperties) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if strings.TrimSpace(properties.APN) != "" {
		b.properties.APN = strings.TrimSpace(properties.APN)
	}
	if strings.TrimSpace(properties.IPType) != "" {
		b.properties.IPType = normalizeIPType(properties.IPType)
	}
	if strings.TrimSpace(properties.Username) != "" {
		b.properties.Username = strings.TrimSpace(properties.Username)
	}
	if properties.Password != "" {
		b.properties.Password = properties.Password
	}
	if strings.TrimSpace(properties.AllowedAuth) != "" {
		b.properties.AllowedAuth = normalizeAuthenticationName(properties.AllowedAuth)
	}
	b.mu.Unlock()
}

func (b *Bearer) closeAdapter() {
	if b == nil {
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
}

func (b *Bearer) Path() uint64 {
	if b == nil || b.core == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.infoValue.ID
}

func (b *Bearer) Interface(ctx context.Context) (string, error) {
	info, err := b.info(ctx)
	return info.Network.Interface, err
}

func (b *Bearer) Connected(ctx context.Context) (bool, error) {
	info, err := b.info(ctx)
	return info.Connected, err
}

func (b *Bearer) IP4Config(ctx context.Context) (BearerIPConfig, error) {
	info, err := b.info(ctx)
	if err != nil {
		return BearerIPConfig{}, err
	}
	return ipConfig(info.Network, false), nil
}

func (b *Bearer) IP6Config(ctx context.Context) (BearerIPConfig, error) {
	info, err := b.info(ctx)
	if err != nil {
		return BearerIPConfig{}, err
	}
	return ipConfig(info.Network, true), nil
}

func (b *Bearer) Stats(ctx context.Context) (BearerStats, error) {
	if b == nil || b.core == nil {
		return BearerStats{}, errModemRequired
	}
	stats, err := b.core.Stats(ctx)
	if err != nil {
		return BearerStats{}, err
	}
	return BearerStats{RXBytes: stats.RXBytes, TXBytes: stats.TXBytes, Duration: uint32(stats.Duration.Seconds())}, nil
}

func (b *Bearer) APN(ctx context.Context) (string, error) {
	info, err := b.info(ctx)
	return info.APN, err
}

func (b *Bearer) Properties(ctx context.Context) (BearerProperties, error) {
	info, err := b.info(ctx)
	if err != nil {
		return BearerProperties{}, err
	}
	b.mu.RLock()
	properties := b.properties
	b.mu.RUnlock()
	if properties.APN == "" {
		properties.APN = strings.TrimSpace(info.APN)
	}
	if properties.IPType == "" {
		properties.IPType = ipTypeFromNetwork(info.Network)
	}
	return properties, nil
}

func (b *Bearer) Disconnect(ctx context.Context) error {
	if b == nil || b.core == nil {
		return nil
	}
	if err := b.core.Disconnect(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	b.infoValue.Connected = false
	id := b.infoValue.ID
	b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
	if b.modem != nil {
		b.modem.removeBearerAdapter(id, b)
	}
	b.wg.Wait()
	return nil
}

func (b *Bearer) info(ctx context.Context) (wwanmodem.BearerInfo, error) {
	if b == nil || b.core == nil {
		return wwanmodem.BearerInfo{}, errModemRequired
	}
	if err := ctx.Err(); err != nil {
		return wwanmodem.BearerInfo{}, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneBearerInfo(b.infoValue), nil
}

func cloneBearerInfo(info wwanmodem.BearerInfo) wwanmodem.BearerInfo {
	info.Network.Addresses = slices.Clone(info.Network.Addresses)
	info.Network.Gateways = slices.Clone(info.Network.Gateways)
	info.Network.DNS = slices.Clone(info.Network.DNS)
	return info
}

func bearerPropertiesFromInfo(info wwanmodem.BearerInfo) BearerProperties {
	return BearerProperties{APN: strings.TrimSpace(info.APN), IPType: ipTypeFromNetwork(info.Network)}
}

func ipTypeFromNetwork(network wwanmodem.NetworkConfig) string {
	var ipv4, ipv6 bool
	for _, prefix := range network.Addresses {
		ipv4 = ipv4 || prefix.Addr().Is4()
		ipv6 = ipv6 || prefix.Addr().Is6()
	}
	switch {
	case ipv4 && ipv6:
		return "ipv4v6"
	case ipv4:
		return "ipv4"
	case ipv6:
		return "ipv6"
	default:
		return ""
	}
}

func ipConfig(network wwanmodem.NetworkConfig, ipv6 bool) BearerIPConfig {
	addresses := network.Addresses
	if !ipv6 {
		addresses = slices.DeleteFunc(slices.Clone(addresses), func(prefix netip.Prefix) bool { return prefix.Addr().Is6() })
	} else {
		addresses = slices.DeleteFunc(slices.Clone(addresses), func(prefix netip.Prefix) bool { return !prefix.Addr().Is6() })
	}
	config := BearerIPConfig{Method: BearerIPMethodStatic, MTU: network.MTU}
	if len(addresses) > 0 {
		config.Address = addresses[0].Addr().String()
		config.Prefix = uint32(addresses[0].Bits())
	}
	for _, gateway := range network.Gateways {
		if gateway.Is6() == ipv6 {
			config.Gateway = gateway.String()
			break
		}
	}
	for _, dns := range network.DNS {
		if dns.Is6() == ipv6 {
			config.DNS = append(config.DNS, dns.String())
		}
	}
	return config
}

func semanticIPFamily(value string) (wwanmodem.IPFamily, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ipv4":
		return wwanmodem.IPFamilyIPv4, nil
	case "ipv6":
		return wwanmodem.IPFamilyIPv6, nil
	case "", "ipv4v6", "ipv4+ipv6":
		return wwanmodem.IPFamilyIPv4v6, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerIPType, value)
	}
}

func semanticAuthentication(value string) (wwanmodem.Authentication, error) {
	switch normalizeAuthenticationName(value) {
	case "none":
		return wwanmodem.AuthenticationNone, nil
	case "", "pap|chap":
		return wwanmodem.AuthenticationPAP | wwanmodem.AuthenticationCHAP, nil
	case "pap":
		return wwanmodem.AuthenticationPAP, nil
	case "chap":
		return wwanmodem.AuthenticationCHAP, nil
	case "mschapv2":
		return wwanmodem.AuthenticationMSCHAPv2, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerAuth, value)
	}
}

func normalizeIPType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "ipv4+ipv6" {
		return "ipv4v6"
	}
	return value
}

func normalizeAuthenticationName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "default", "auto":
		return ""
	case "mschap":
		return "mschapv2"
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '|' || r == ',' || r == ' ' })
	if len(parts) == 2 && slices.Contains(parts, "pap") && slices.Contains(parts, "chap") {
		return "pap|chap"
	}
	return value
}

func BearerIPFamily(ipType string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(ipType)) {
	case "ipv4":
		return 1, nil
	case "ipv6":
		return 2, nil
	case "", "ipv4v6", "ipv4+ipv6":
		return 4, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerIPType, ipType)
	}
}

func BearerAllowedAuth(auth string) (uint32, error) {
	switch normalizeAuthenticationName(auth) {
	case "none":
		return 1, nil
	case "", "pap|chap":
		return 2 | 4, nil
	case "pap":
		return 2, nil
	case "chap":
		return 4, nil
	case "mschapv2":
		return 16, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedBearerAuth, auth)
	}
}
