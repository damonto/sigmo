package internet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const qualcomm410CleanupTimeout = time.Minute

type qualcomm410Link interface {
	Close() error
}

// qualcomm410State records the selected DATA5 data path and its lifecycle.
// ModemManager continues to own the Internet bearer; link only keeps raw-IP
// sticky after that bearer has been connected and configured.
type qualcomm410State struct {
	selected             bool
	link                 qualcomm410Link
	reconnectPending     bool
	reconnectPreferences Preferences
	reloadPending        bool
}

func (s *qualcomm410State) scheduleReconnect(prefs Preferences) {
	s.reconnectPending = true
	s.reconnectPreferences = prefs
}

func (s *qualcomm410State) clearReconnect() {
	s.reconnectPending = false
	s.reconnectPreferences = Preferences{}
}

// qualcomm410InterfaceState records the host setting changed while the
// ModemManager bearer uses the Qualcomm 410 point-to-point data path.
type qualcomm410InterfaceState struct {
	originalIPv6 netlink.IPv6Autoconfiguration
	restoreIPv6  bool
}

type qualcomm410NetworkOps struct {
	readIPv6Autoconfiguration    func(string) (netlink.IPv6Autoconfiguration, error)
	setIPv6Autoconfiguration     func(string, netlink.IPv6Autoconfiguration) error
	disableIPv6Autoconfiguration func(string) error
	flushDefaultRoutes           func(string) error
	flushGlobalAddresses         func(string) error
	setUp                        func(string) error
	setMTU                       func(string, uint32) error
	defaultRoutes                func() ([]netlink.DefaultRoute, error)
	addPointToPointAddress       func(string, netip.Addr, netip.Addr) error
	deletePointToPointAddress    func(string, netip.Addr, netip.Addr) error
	addDefaultRoute              func(netlink.DefaultRoute) error
	deleteDefaultRoute           func(netlink.DefaultRoute) error
}

func (o qualcomm410NetworkOps) routeOps() defaultRouteOps {
	return defaultRouteOps{
		defaultRoutes:      o.defaultRoutes,
		addDefaultRoute:    o.addDefaultRoute,
		deleteDefaultRoute: o.deleteDefaultRoute,
	}
}

var (
	openInternetQualcomm410Link = func(ctx context.Context, cfg mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		return mmodem.OpenBAMDMUXLink(ctx, cfg)
	}
	validateInternetQualcomm410Layout    = mmodem.ValidateQualcomm410ModemLayout
	currentQualcomm410Bearer             = currentBearer
	cleanupInternetQualcomm410StaleState = func(ctx context.Context, connector *Connector, modemID string) error {
		return connector.cleanupStaleConnectionState(ctx, modemID, mmodem.Qualcomm410InternetInterface)
	}
	reconnectInternetQualcomm410Bearer = func(ctx context.Context, connector *Connector, access internetModem, prefs Preferences) error {
		_, err := connector.connect(ctx, access, prefs, false)
		return err
	}
	disconnectInternetQualcomm410Bearer = func(ctx context.Context, connector *Connector, access internetModem) error {
		return connector.disconnect(ctx, access, false)
	}
	systemQualcomm410NetworkOps = qualcomm410NetworkOps{
		readIPv6Autoconfiguration:    netlink.ReadIPv6Autoconfiguration,
		setIPv6Autoconfiguration:     netlink.SetIPv6Autoconfiguration,
		disableIPv6Autoconfiguration: netlink.DisableIPv6Autoconfiguration,
		flushDefaultRoutes:           netlink.FlushDefaultRoutes,
		flushGlobalAddresses:         netlink.FlushGlobalAddresses,
		setUp:                        netlink.SetUp,
		setMTU:                       netlink.SetMTU,
		defaultRoutes:                netlink.DefaultRoutes,
		addPointToPointAddress:       netlink.AddPointToPointAddress,
		deletePointToPointAddress:    netlink.DeletePointToPointAddress,
		addDefaultRoute:              netlink.AddDefaultRoute,
		deleteDefaultRoute:           netlink.DeleteDefaultRoute,
	}
)

func (c *Connector) qualcomm410SelectedFor(modemID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qualcomm410States[modemID].selected
}

func (c *Connector) qualcomm410StateFor(modemID string) qualcomm410State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qualcomm410States[modemID]
}

func (c *Connector) setQualcomm410State(modemID string, state qualcomm410State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.qualcomm410States == nil {
		c.qualcomm410States = make(map[string]qualcomm410State)
	}
	if !state.selected && state.link == nil && !state.reconnectPending && !state.reloadPending {
		delete(c.qualcomm410States, modemID)
		return
	}
	c.qualcomm410States[modemID] = state
}

// SelectQualcomm410Mode records how the next Internet bearer must be
// configured without opening DATA5 or changing an existing bearer.
func (c *Connector) SelectQualcomm410Mode(modem *mmodem.Modem) error {
	if modem == nil {
		return ErrModemRequired
	}
	if err := validateInternetQualcomm410Layout(modem); err != nil {
		return fmt.Errorf("validate Qualcomm 410 layout: %w", err)
	}
	modemID := modem.EquipmentIdentifier
	defer c.lockModem(modemID)()

	state := c.qualcomm410StateFor(modemID)
	state.selected = true
	c.setQualcomm410State(modemID, state)
	return nil
}

// SetQualcomm410Enabled switches the normal ModemManager bearer into or out
// of the Qualcomm 410 DATA5 data-plane mode. It never opens an Internet PDN in
// Sigmo: DATA5 remains owned by ModemManager, while Sigmo only holds WDA raw-IP
// after ModemManager has connected and Sigmo has applied the point-to-point
// addresses reported by the bearer.
func (c *Connector) SetQualcomm410Enabled(ctx context.Context, modem *mmodem.Modem, enabled bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	modemID := modem.EquipmentIdentifier
	defer c.lockModem(modemID)()

	state := c.qualcomm410StateFor(modemID)
	if !enabled && !state.selected && state.link == nil && !state.reconnectPending {
		return nil
	}

	access := modemAccess{modem: modem}
	if enabled {
		if err := validateInternetQualcomm410Layout(modem); err != nil {
			return fmt.Errorf("validate Qualcomm 410 layout: %w", err)
		}
		if state.selected && state.link != nil {
			if state.reloadPending {
				return c.resumeQualcomm410AfterReloadLocked(ctx, access)
			}
			if state.reconnectPending {
				if err := c.retryQualcomm410ReconnectLocked(ctx, access, state); err != nil {
					return fmt.Errorf("reconnect ModemManager Internet bearer for Qualcomm 410: %w", err)
				}
				return nil
			}
			return c.ensureQualcomm410BearerLocked(ctx, access)
		}
		return c.enableQualcomm410Locked(ctx, access, state)
	}
	return c.disableQualcomm410Locked(ctx, access, state)
}

// InvalidateQualcomm410 drops the WDA holder when ModemManager removes a
// modem. QMI clients cannot survive that device generation; the next enable
// restores an active bearer or waits for the next Internet connection.
func (c *Connector) InvalidateQualcomm410(modemID string) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return nil
	}
	defer c.lockModem(modemID)()

	state := c.qualcomm410StateFor(modemID)
	if !state.selected && state.link == nil {
		return nil
	}
	if tracked, ok := c.connection(modemID); ok && !state.reconnectPending {
		state.scheduleReconnect(tracked.prefs)
	}
	state.reloadPending = true
	var err error
	if state.link != nil {
		err = state.link.Close()
	}
	state.link = nil
	c.setQualcomm410State(modemID, state)
	if err != nil {
		return fmt.Errorf("release invalidated Qualcomm 410 Internet WDA client: %w", err)
	}
	return nil
}

func (c *Connector) enableQualcomm410Locked(ctx context.Context, access internetModem, previous qualcomm410State) error {
	state := previous
	var err error
	if state.reloadPending {
		state, err = c.prepareQualcomm410AfterReloadLocked(ctx, access, state)
		if err != nil {
			return err
		}
	}
	if state.reconnectPending && !state.selected {
		// A failed switch back to the normal bearer leaves reconnect pending.
		// Re-enabling means that retry must use the Qualcomm 410 path.
		state.selected = true
		c.setQualcomm410State(access.id(), state)
	}
	if !state.reconnectPending {
		current, currentErr := currentQualcomm410Bearer(ctx, access)
		if currentErr != nil {
			return fmt.Errorf("read Internet bearer before holding Qualcomm 410 WDA client: %w", currentErr)
		}
		if !current.connected {
			// WDA is sticky state, not a cold-start data-plane initializer. Let
			// ModemManager own the first setup and hold WDA only after Connect
			// has configured the bearer on the host.
			state.selected = true
			c.setQualcomm410State(access.id(), state)
			return nil
		}
		state.scheduleReconnect(c.qmapMigrationPreferences(ctx, access, current.bearer))
		if err := disconnectInternetQualcomm410Bearer(ctx, c, access); err != nil {
			return fmt.Errorf("disconnect Internet bearer before holding Qualcomm 410 WDA client: %w", err)
		}
		state.selected = true
		c.setQualcomm410State(access.id(), state)
	}

	holdErr := c.openQualcomm410HolderLocked(ctx, access.id())
	state = c.qualcomm410StateFor(access.id())

	if state.reconnectPending {
		if err := c.retryQualcomm410ReconnectLocked(ctx, access, state); err != nil {
			return errors.Join(holdErr, fmt.Errorf("reconnect ModemManager Internet bearer for Qualcomm 410: %w", err))
		}
		return nil
	}
	if holdErr != nil {
		return holdErr
	}
	return c.ensureQualcomm410BearerLocked(ctx, access)
}

func (c *Connector) openQualcomm410HolderLocked(ctx context.Context, modemID string) error {
	state := c.qualcomm410StateFor(modemID)
	if !state.selected || state.link != nil {
		return nil
	}
	link, err := openInternetQualcomm410Link(ctx, mmodem.BAMDMUXLinkConfig{
		ControlPort:   mmodem.Qualcomm410InternetQMI,
		InterfaceName: mmodem.Qualcomm410InternetInterface,
	})
	if err != nil {
		return fmt.Errorf("hold Qualcomm 410 Internet WDA client: %w", err)
	}
	state.link = link
	c.setQualcomm410State(modemID, state)
	return nil
}

// holdQualcomm410AfterInternetConnectedLocked must be called while the modem
// operation lock is held and only after the MM bearer has been configured.
func (c *Connector) holdQualcomm410AfterInternetConnectedLocked(ctx context.Context, modemID string) error {
	holdErr := c.openQualcomm410HolderLocked(ctx, modemID)
	state := c.qualcomm410StateFor(modemID)
	if !state.selected {
		return holdErr
	}
	state.clearReconnect()
	state.reloadPending = false
	c.setQualcomm410State(modemID, state)
	return holdErr
}

func (c *Connector) ensureQualcomm410BearerLocked(ctx context.Context, access internetModem) error {
	current, err := currentQualcomm410Bearer(ctx, access)
	if err != nil {
		return fmt.Errorf("read Internet bearer before enabling Qualcomm 410: %w", err)
	}
	if !current.connected {
		return nil
	}
	if tracked, ok := c.connection(access.id()); ok && tracked.bearerPath == current.bearer.Path() && len(tracked.peers) > 0 {
		return nil
	}
	prefs := c.qmapMigrationPreferences(ctx, access, current.bearer)
	if err := disconnectInternetQualcomm410Bearer(ctx, c, access); err != nil {
		return fmt.Errorf("disconnect Internet bearer before enabling Qualcomm 410: %w", err)
	}
	state := c.qualcomm410StateFor(access.id())
	state.scheduleReconnect(prefs)
	c.setQualcomm410State(access.id(), state)
	if err := c.retryQualcomm410ReconnectLocked(ctx, access, state); err != nil {
		return fmt.Errorf("reconnect ModemManager Internet bearer for Qualcomm 410: %w", err)
	}
	return nil
}

func (c *Connector) disableQualcomm410Locked(ctx context.Context, access internetModem, state qualcomm410State) error {
	if !state.selected && state.link == nil {
		if state.reconnectPending {
			if err := c.retryQualcomm410ReconnectLocked(ctx, access, state); err != nil {
				return fmt.Errorf("restore normal ModemManager Internet bearer: %w", err)
			}
		}
		return nil
	}

	current, err := currentQualcomm410Bearer(ctx, access)
	if err != nil {
		return fmt.Errorf("read Internet bearer before disabling Qualcomm 410: %w", err)
	}
	reconnect := current.connected || state.reconnectPending
	prefs := state.reconnectPreferences
	if current.connected {
		prefs = c.qmapMigrationPreferences(ctx, access, current.bearer)
	}
	_, tracked := c.connection(access.id())
	if !tracked && current.bearer == nil {
		if err := cleanupInternetQualcomm410StaleState(ctx, c, access.id()); err != nil {
			return fmt.Errorf("clean stale Qualcomm 410 Internet network: %w", err)
		}
	} else if err := disconnectInternetQualcomm410Bearer(ctx, c, access); err != nil {
		return fmt.Errorf("disconnect Qualcomm 410 ModemManager bearer: %w", err)
	}

	var closeErr error
	if state.link != nil {
		closeErr = state.link.Close()
	}
	next := qualcomm410State{}
	if reconnect {
		next.scheduleReconnect(prefs)
	}
	c.setQualcomm410State(access.id(), next)
	if !reconnect {
		if closeErr != nil {
			return fmt.Errorf("release Qualcomm 410 Internet WDA client: %w", closeErr)
		}
		return nil
	}

	connectErr := c.retryQualcomm410ReconnectLocked(ctx, access, next)
	if connectErr != nil {
		connectErr = fmt.Errorf("restore normal ModemManager Internet bearer: %w", connectErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("release Qualcomm 410 Internet WDA client: %w", closeErr)
	}
	return errors.Join(closeErr, connectErr)
}

func (c *Connector) retryQualcomm410ReconnectLocked(ctx context.Context, access internetModem, state qualcomm410State) error {
	if !state.reconnectPending {
		return nil
	}
	if err := reconnectInternetQualcomm410Bearer(ctx, c, access, state.reconnectPreferences); err != nil {
		return err
	}
	state = c.qualcomm410StateFor(access.id())
	state.clearReconnect()
	c.setQualcomm410State(access.id(), state)
	return nil
}

func (c *Connector) resumeQualcomm410AfterReloadLocked(ctx context.Context, access internetModem) error {
	state := c.qualcomm410StateFor(access.id())
	var closeErr error
	if state.link != nil {
		closeErr = state.link.Close()
		state.link = nil
		c.setQualcomm410State(access.id(), state)
	}
	enableErr := c.enableQualcomm410Locked(ctx, access, state)
	if closeErr != nil {
		closeErr = fmt.Errorf("release invalidated Qualcomm 410 Internet WDA client before reopening: %w", closeErr)
	}
	return errors.Join(closeErr, enableErr)
}

func (c *Connector) prepareQualcomm410AfterReloadLocked(ctx context.Context, access internetModem, state qualcomm410State) (qualcomm410State, error) {
	current, err := currentQualcomm410Bearer(ctx, access)
	if err != nil {
		return state, fmt.Errorf("read Internet bearer after Qualcomm 410 reload: %w", err)
	}

	if tracked, ok := c.connection(access.id()); ok {
		if !state.reconnectPending {
			state.scheduleReconnect(tracked.prefs)
			c.setQualcomm410State(access.id(), state)
		}
		cleanupErr := c.cleanupTracked(ctx, access.id(), tracked)
		if cleanupErr == nil {
			cleanupErr = c.syncCleanedUpDefaultRouteState(ctx, tracked)
		}
		cleanupErr = errors.Join(cleanupErr, restoreStaleDefaultRouteStatesWithStore(ctx, c.persistence, routeStateRestoreTarget{modemID: access.id()}, netlinkDefaultRouteOps))
		if cleanupErr != nil {
			return state, fmt.Errorf("clean invalidated Qualcomm 410 bearer network: %w", cleanupErr)
		}
		c.deleteConnection(access.id())
	} else if err := cleanupInternetQualcomm410StaleState(ctx, c, access.id()); err != nil {
		return state, fmt.Errorf("clean stale Qualcomm 410 Internet network after reload: %w", err)
	}

	if current.connected {
		state = c.qualcomm410StateFor(access.id())
		state.scheduleReconnect(c.qmapMigrationPreferences(ctx, access, current.bearer))
		c.setQualcomm410State(access.id(), state)
		if err := current.bearer.Disconnect(ctx); err != nil {
			return state, fmt.Errorf("disconnect invalidated Qualcomm 410 ModemManager bearer: %w", err)
		}
	}

	state.reloadPending = false
	c.setQualcomm410State(access.id(), state)
	return state, nil
}

func (c *Connector) configureConnectedBearer(ctx context.Context, modemID string, bearer *mmodem.Bearer, prefs Preferences) (trackedConnection, error) {
	if c.qualcomm410SelectedFor(modemID) {
		return configureQualcomm410Bearer(ctx, c.persistence, modemID, bearer, prefs)
	}
	return configureBearer(ctx, c.persistence, modemID, bearer, prefs)
}

func configureQualcomm410Bearer(ctx context.Context, state connectionStateStore, modemID string, bearer *mmodem.Bearer, prefs Preferences) (trackedConnection, error) {
	var tracked trackedConnection
	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read ipv6 config: %w", err)
	}
	return configureQualcomm410BearerWithOps(ctx, state, modemID, interfaceName, ip4, ip6, prefs, systemQualcomm410NetworkOps)
}

func configureQualcomm410BearerWithOps(ctx context.Context, state connectionStateStore, modemID, interfaceName string, ip4, ip6 mmodem.BearerIPConfig, prefs Preferences, ops qualcomm410NetworkOps) (tracked trackedConnection, err error) {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName != mmodem.Qualcomm410InternetInterface {
		return tracked, fmt.Errorf("Qualcomm 410 Internet bearer interface is %q, want %q", interfaceName, mmodem.Qualcomm410InternetInterface)
	}

	addresses, peers, routes, mtu, err := qualcomm410BearerNetwork(interfaceName, routeMetric(prefs.DefaultRoute), ip4, ip6)
	if err != nil {
		return tracked, err
	}
	tracked = trackedConnection{
		interfaceName: interfaceName,
		prefs:         prefs,
		routeMetric:   routeMetric(prefs.DefaultRoute),
		peers:         make(map[netip.Prefix]netip.Addr, len(peers)),
	}
	if !prefs.DefaultRoute && len(routes) > 0 {
		current, err := ops.defaultRoutes()
		if err != nil {
			return tracked, fmt.Errorf("list default routes: %w", err)
		}
		tracked.routeMetric = secondaryRouteMetricFor(routes, current)
		setRouteMetric(routes, tracked.routeMetric)
	}
	originalIPv6, err := ops.readIPv6Autoconfiguration(interfaceName)
	if err != nil {
		return tracked, fmt.Errorf("read Qualcomm 410 IPv6 autoconfiguration: %w", err)
	}
	tracked.qualcomm410InterfaceState = qualcomm410InterfaceState{
		originalIPv6: originalIPv6,
		restoreIPv6:  true,
	}

	release := false
	defer func() {
		if release {
			return
		}
		rollbackCtx, cancel := qualcomm410CleanupContext(ctx)
		defer cancel()
		rollbackErr := cleanupQualcomm410Applied(rollbackCtx, state, tracked, ops)
		if rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("roll back Qualcomm 410 bearer network: %w", rollbackErr))
		}
	}()

	if err := ops.disableIPv6Autoconfiguration(interfaceName); err != nil {
		return tracked, fmt.Errorf("disable Qualcomm 410 IPv6 autoconfiguration: %w", err)
	}
	if err := errors.Join(ops.flushDefaultRoutes(interfaceName), ops.flushGlobalAddresses(interfaceName)); err != nil {
		return tracked, fmt.Errorf("reset Qualcomm 410 bearer network: %w", err)
	}
	if err := ops.setUp(interfaceName); err != nil {
		return tracked, fmt.Errorf("set Qualcomm 410 bearer interface up: %w", err)
	}
	if mtu > 0 {
		if err := ops.setMTU(interfaceName, mtu); err != nil {
			return tracked, fmt.Errorf("set Qualcomm 410 bearer MTU %d: %w", mtu, err)
		}
	}
	for _, prefix := range addresses {
		peer := peers[prefix]
		if err := ops.addPointToPointAddress(interfaceName, prefix.Addr(), peer); err != nil {
			return tracked, fmt.Errorf("add Qualcomm 410 bearer address %s peer %s: %w", prefix, peer, err)
		}
		tracked.addresses = append(tracked.addresses, prefix)
		tracked.peers[prefix] = peer
	}
	if prefs.DefaultRoute {
		if err := restoreStaleDefaultRouteStatesWithStore(ctx, state, routeStateRestoreTarget{
			modemID: modemID, interfaceNames: []string{interfaceName},
		}, ops.routeOps()); err != nil {
			return tracked, fmt.Errorf("restore previous default route state: %w", err)
		}
		changes, err := takeoverDefaultRoutesWithStore(ctx, state, modemID, interfaceName, routes, ops.routeOps())
		tracked.routeChanges = changes
		if err != nil {
			return tracked, fmt.Errorf("take over default route: %w", err)
		}
	}
	for _, route := range routes {
		if err := ops.addDefaultRoute(route); err != nil {
			return tracked, fmt.Errorf("add Qualcomm 410 default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}

	release = true
	return tracked, nil
}

func qualcomm410BearerNetwork(interfaceName string, metric int, ip4, ip6 mmodem.BearerIPConfig) ([]netip.Prefix, map[netip.Prefix]netip.Addr, []netlink.DefaultRoute, uint32, error) {
	var addresses []netip.Prefix
	peers := make(map[netip.Prefix]netip.Addr, 2)
	var routes []netlink.DefaultRoute
	var mtu uint32

	for _, family := range []struct {
		config mmodem.BearerIPConfig
		family int
	}{
		{config: ip4, family: netlink.FamilyIPv4},
		{config: ip6, family: netlink.FamilyIPv6},
	} {
		prefix, peer, ok, err := qualcomm410AddressPair(family.config, family.family)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		if !ok {
			continue
		}
		addresses = append(addresses, prefix)
		peers[prefix] = peer
		route := netlink.DefaultRoute{
			Interface: interfaceName,
			Family:    family.family,
			Source:    prefix.Addr(),
			Metric:    metric,
		}
		if family.family == netlink.FamilyIPv4 {
			route.Gateway = peer
		}
		routes = append(routes, route)
		if family.config.MTU > 0 && (mtu == 0 || family.config.MTU < mtu) {
			mtu = family.config.MTU
		}
	}
	if len(addresses) == 0 {
		return nil, nil, nil, 0, ErrUnsupportedIPMethod
	}
	return addresses, peers, routes, mtu, nil
}

func qualcomm410AddressPair(config mmodem.BearerIPConfig, family int) (netip.Prefix, netip.Addr, bool, error) {
	if !config.ConfiguredAddress() {
		return netip.Prefix{}, netip.Addr{}, false, nil
	}
	local, err := netip.ParseAddr(strings.TrimSpace(config.Address))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, false, fmt.Errorf("parse Qualcomm 410 bearer address %q: %w", config.Address, err)
	}
	peer, err := netip.ParseAddr(strings.TrimSpace(config.Gateway))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, false, fmt.Errorf("parse Qualcomm 410 bearer peer %q: %w", config.Gateway, err)
	}
	local = local.Unmap()
	peer = peer.Unmap()
	wantIPv4 := family == netlink.FamilyIPv4
	if local.Is4() != wantIPv4 || peer.Is4() != wantIPv4 || local.IsUnspecified() || peer.IsUnspecified() {
		return netip.Prefix{}, netip.Addr{}, false, fmt.Errorf("Qualcomm 410 bearer address %s and peer %s do not match IP family", local, peer)
	}
	return netip.PrefixFrom(local, local.BitLen()), peer, true, nil
}

func cleanupQualcomm410Applied(ctx context.Context, state connectionStateStore, tracked trackedConnection, ops qualcomm410NetworkOps) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var result error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		result = errors.Join(result, ops.deleteDefaultRoute(tracked.routes[i]))
	}
	result = errors.Join(result, cleanupDefaultRouteChangesWithStore(ctx, state, tracked.interfaceName, tracked.routeChanges, ops.routeOps()))
	for i := len(tracked.addresses) - 1; i >= 0; i-- {
		prefix := tracked.addresses[i]
		result = errors.Join(result, ops.deletePointToPointAddress(tracked.interfaceName, prefix.Addr(), tracked.peers[prefix]))
	}
	if tracked.qualcomm410InterfaceState.restoreIPv6 {
		if err := ops.setIPv6Autoconfiguration(tracked.interfaceName, tracked.qualcomm410InterfaceState.originalIPv6); err != nil {
			result = errors.Join(result, fmt.Errorf("restore Qualcomm 410 IPv6 autoconfiguration: %w", err))
		}
	}
	return result
}

func qualcomm410CleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), qualcomm410CleanupTimeout)
}

func (c *Connector) recoverConnectedBearer(ctx context.Context, modem internetModem, bearer *mmodem.Bearer, fallback Preferences) (trackedConnection, int, bool, error) {
	if !c.qualcomm410SelectedFor(modem.id()) {
		return recoverTrackedConnection(ctx, c.persistence, modem.id(), bearer, fallback)
	}
	return recoverQualcomm410TrackedConnection(ctx, c.persistence, modem.id(), bearer, fallback)
}

func recoverQualcomm410TrackedConnection(ctx context.Context, stateStore connectionStateStore, modemID string, bearer *mmodem.Bearer, fallback Preferences) (trackedConnection, int, bool, error) {
	prefs := recoverPreferences(ctx, bearer, fallback)
	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read ipv6 config: %w", err)
	}

	routeState := routeStateForInterface(interfaceName)
	metric := routeState.Metric
	includeRoutes := routeState.Found
	if routeState.Found {
		prefs.DefaultRoute = routeState.DefaultRoute
	}
	proxyEnabled, proxyStateFound, err := stateStore.loadProxyStateForModem(ctx, modemID, interfaceName)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("load proxy state: %w", err)
	}
	if proxyStateFound {
		prefs.ProxyEnabled = proxyEnabled
	}

	addresses, peers, routes, _, err := qualcomm410BearerNetwork(interfaceName, metric, ip4, ip6)
	if errors.Is(err, ErrUnsupportedIPMethod) {
		return trackedConnection{}, metric, false, nil
	}
	if err != nil {
		return trackedConnection{}, 0, false, err
	}
	if !includeRoutes {
		routes = nil
	}
	routeChanges, routeStateFound, err := stateStore.loadRouteStateForModem(ctx, modemID, interfaceName)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("load default route state: %w", err)
	}
	if prefs.DefaultRoute && !routeStateFound {
		routeChanges, err = takeoverDefaultRoutesWithStore(ctx, stateStore, modemID, interfaceName, routes, netlinkDefaultRouteOps)
		if err != nil {
			return trackedConnection{}, 0, false, fmt.Errorf("take over recovered default route: %w", err)
		}
	}
	return trackedConnection{
		bearerPath: bearer.Path(), interfaceName: interfaceName, prefs: prefs,
		routeMetric: metric, addresses: addresses, peers: peers, routes: routes,
		routeChanges: routeChanges,
	}, metric, true, nil
}

func (c *Connector) cleanupConnectedBearer(ctx context.Context, modem internetModem, bearer *mmodem.Bearer, prefs Preferences) error {
	if !c.qualcomm410SelectedFor(modem.id()) {
		return cleanupBearer(ctx, c.persistence, modem.id(), bearer, prefs)
	}
	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
	if err != nil {
		return fmt.Errorf("read ipv6 config: %w", err)
	}
	routeState := routeStateForInterface(interfaceName)
	metric := routeMetric(prefs.DefaultRoute)
	if routeState.Found {
		metric = routeState.Metric
	}
	addresses, peers, routes, _, err := qualcomm410BearerNetwork(interfaceName, metric, ip4, ip6)
	if errors.Is(err, ErrUnsupportedIPMethod) {
		return nil
	}
	if err != nil {
		return err
	}
	if !routeState.Found {
		routes = nil
	}
	routeChanges, _, err := c.persistence.loadRouteStateForModem(ctx, modem.id(), interfaceName)
	if err != nil {
		return fmt.Errorf("load default route state: %w", err)
	}
	return cleanupApplied(ctx, c.persistence, trackedConnection{
		interfaceName: interfaceName, addresses: addresses, peers: peers,
		routes: routes, routeChanges: routeChanges,
	})
}
