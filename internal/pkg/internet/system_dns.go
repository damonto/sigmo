package internet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"slices"

	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

var (
	errSystemDNSUnavailable = errors.New("system default DNS is unavailable")
	errSystemDNSConflict    = errors.New("another connection controls the default DNS")
)

const systemDNSKey = "internet.system_dns"

var rootDNSDomain = dnsDomain{Name: ".", RouteOnly: true}

// Keep the active and previous DNS scopes in the same record as pending writes.
type systemDNSState struct {
	Owners  []systemDNSOwner `json:"owners,omitempty"`
	Pending *systemDNSOwner  `json:"pending,omitempty"`
}

type systemDNSOwner struct {
	Modem     string            `json:"modem"`
	Interface string            `json:"interface"`
	Link      linkIdentity      `json:"link"`
	Servers   []netip.Addr      `json:"servers"`
	Routes    []routeStateRoute `json:"routes"`
}

func (c *Connector) loadSystemDNS(ctx context.Context) (systemDNSState, error) {
	var state systemDNSState
	err := c.state.Get(ctx, "system", systemDNSKey, &state)
	if errors.Is(err, storage.ErrNotFound) {
		err = nil
	}
	return state, err
}

func (c *Connector) saveSystemDNS(ctx context.Context, state systemDNSState) error {
	if len(state.Owners) == 0 && state.Pending == nil {
		return c.state.Delete(ctx, "system", systemDNSKey)
	}
	return c.state.Put(ctx, "system", systemDNSKey, state)
}

func (c *Connector) beginSystemDNS(ctx context.Context, modemID string, tracked trackedConnection) (systemDNSState, error) {
	state, err := c.loadSystemDNS(ctx)
	if err != nil {
		return state, err
	}
	if state.Pending != nil {
		return state, fmt.Errorf("%w: previous DNS change is awaiting cleanup", errSystemDNSUnavailable)
	}
	if err := c.systemDNS.check(ctx, tracked.interfaceName); err != nil {
		return state, err
	}
	link, err := c.routeOperationSet().linkID(tracked.interfaceName)
	if err != nil {
		return state, fmt.Errorf("%w: identify DNS interface %s: %w", errSystemDNSUnavailable, tracked.interfaceName, err)
	}
	owner := systemDNSOwner{
		Modem: modemID, Interface: tracked.interfaceName, Link: link,
		Servers: defaultDNSServers(tracked.dns, tracked.routes), Routes: routeStateRoutes(tracked.routes),
	}
	if len(owner.Servers) == 0 {
		return state, fmt.Errorf("%w: %s has no carrier DNS for its available IP families", errSystemDNSUnavailable, tracked.interfaceName)
	}
	configs, err := c.systemDNS.configuration(ctx)
	if err != nil {
		return state, fmt.Errorf("%w: %w", errSystemDNSUnavailable, err)
	}
	if err := c.checkDNSScopes(configs, state.Owners); err != nil {
		return state, err
	}
	if dnsConfigured(configs[link.Index]) {
		return state, fmt.Errorf("%w: %s already has per-link DNS settings", errSystemDNSConflict, tracked.interfaceName)
	}
	staged := state
	staged.Pending = &owner
	if err := c.saveSystemDNS(ctx, staged); err != nil {
		return state, err
	}
	return staged, nil
}

func (c *Connector) configureDefaultRouteDNS(ctx context.Context, modemID string, tracked trackedConnection) {
	if c.systemDNS == nil || c.state == nil {
		return
	}
	state, err := c.beginSystemDNS(ctx, modemID, tracked)
	if err != nil {
		useHostDNS := errors.Is(err, errSystemDNSUnavailable) || errors.Is(err, errSystemDNSConflict)
		if state.Pending != nil || len(state.Owners) != 0 {
			if cleanupErr := c.restoreSystemDNS(ctx, state, ""); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
				useHostDNS = false
			}
		}
		if useHostDNS {
			slog.Debug("use existing host DNS for default route", "interface", tracked.interfaceName, "error", err)
		} else {
			slog.Warn("configure optional DNS for default route", "interface", tracked.interfaceName, "error", err)
		}
		return
	}
	if err := c.commitSystemDNS(ctx, state); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectionCleanupTimeout)
		defer cancel()
		err = errors.Join(err, c.restoreSystemDNS(cleanupCtx, state, ""))
		slog.Warn("use existing host DNS after default route change", "interface", tracked.interfaceName, "error", err)
	}
}

func defaultDNSServers(addresses []string, routes []netlink.DefaultRoute) []netip.Addr {
	var servers []netip.Addr
	for _, raw := range addresses {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		addr = addr.Unmap().WithZone("")
		if !addr.IsGlobalUnicast() && !addr.IsLinkLocalUnicast() {
			continue
		}
		family := netlink.FamilyIPv6
		if addr.Is4() {
			family = netlink.FamilyIPv4
		}
		if _, ok := preferredMetric(family, routes); ok && !slices.Contains(servers, addr) {
			servers = append(servers, addr)
		}
	}
	return servers
}

func (c *Connector) commitSystemDNS(ctx context.Context, state systemDNSState) error {
	owner := *state.Pending
	current, err := c.routeOperationSet().defaultRoutes()
	if err != nil {
		return err
	}
	preferred, err := defaultRoutesFromState(owner.Routes)
	if err != nil {
		return err
	}
	if len(preferred) == 0 || slices.ContainsFunc(preferred, func(route netlink.DefaultRoute) bool { return !routePreferred(route, current) }) {
		return fmt.Errorf("%w: %s does not have the preferred routes", errSystemDNSConflict, owner.Interface)
	}
	if err := c.switchSystemDNS(ctx, state.Owners, &owner); err != nil {
		return err
	}
	state.Owners = append(withoutDNSOwner(state.Owners, owner.Interface), owner)
	state.Pending = nil
	return c.saveSystemDNS(ctx, state)
}

func (c *Connector) switchSystemDNS(ctx context.Context, owners []systemDNSOwner, next *systemDNSOwner) error {
	var live []systemDNSOwner
	for _, owner := range owners {
		id, err := c.routeOperationSet().linkID(owner.Interface)
		if errors.Is(err, os.ErrNotExist) || err == nil && id != owner.Link {
			continue
		}
		if err != nil {
			return err
		}
		live = append(live, owner)
	}
	if next == nil && len(live) == 0 {
		return nil
	}
	configs, err := c.systemDNS.configuration(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errSystemDNSUnavailable, err)
	}
	if next != nil {
		if err := c.checkDNSScopes(configs, append(live, *next)); err != nil {
			return err
		}
		if !canWriteDNS(configs[next.Link.Index], *next) {
			return fmt.Errorf("%w: DNS settings changed on %s", errSystemDNSConflict, next.Interface)
		}
	}
	for _, owner := range live {
		if next != nil && owner.Link == next.Link {
			continue
		}
		if !c.sameDNSLink(owner) {
			continue
		}
		config, err := c.clearOwnedDNS(ctx, owner, configs[owner.Link.Index])
		if err != nil {
			return err
		}
		configs[owner.Link.Index] = config
	}
	if next == nil {
		return nil
	}
	if err := c.checkDNSScopes(configs, []systemDNSOwner{*next}); err != nil {
		return err
	}
	return c.applyOwnedDNS(ctx, *next, configs[next.Link.Index])
}

func (c *Connector) checkDNSScopes(configs map[int32]linkDNS, owners []systemDNSOwner) error {
	for index, config := range configs {
		if len(config.servers) == 0 || !slices.ContainsFunc(config.domains, func(domain dnsDomain) bool { return domain.Name == "." }) {
			continue
		}
		owned := slices.ContainsFunc(owners, func(owner systemDNSOwner) bool {
			return owner.Link.Index == index && c.sameDNSLink(owner) && hasOwnedDNS(config, owner)
		})
		if !owned {
			return fmt.Errorf("%w: interface index %d already has the default DNS domain (~.)", errSystemDNSConflict, index)
		}
	}
	return nil
}

func (c *Connector) applyOwnedDNS(ctx context.Context, owner systemDNSOwner, config linkDNS) error {
	if !c.sameDNSLink(owner) {
		return fmt.Errorf("%w: interface %s changed during promotion", errSystemDNSConflict, owner.Interface)
	}
	if !canWriteDNS(config, owner) {
		return fmt.Errorf("%w: DNS settings changed on %s", errSystemDNSConflict, owner.Interface)
	}
	if hasOwnedDNS(config, owner) {
		return nil
	}
	if len(config.servers) == 0 {
		if err := c.systemDNS.setServers(ctx, owner.Link.Index, owner.Servers); err != nil {
			return fmt.Errorf("%w: set DNS on %s: %w", errSystemDNSUnavailable, owner.Interface, err)
		}
	}
	if len(config.domains) == 0 {
		if err := c.systemDNS.setDomains(ctx, owner.Link.Index, []dnsDomain{rootDNSDomain}); err != nil {
			return fmt.Errorf("%w: set DNS domain on %s: %w", errSystemDNSUnavailable, owner.Interface, err)
		}
	}
	configs, err := c.systemDNS.configuration(ctx)
	if err != nil {
		return fmt.Errorf("%w: verify DNS on %s: %w", errSystemDNSUnavailable, owner.Interface, err)
	}
	if !hasOwnedDNS(configs[owner.Link.Index], owner) {
		return fmt.Errorf("%w: DNS settings changed on %s", errSystemDNSConflict, owner.Interface)
	}
	return c.checkDNSScopes(configs, []systemDNSOwner{owner})
}

func (c *Connector) clearOwnedDNS(ctx context.Context, owner systemDNSOwner, config linkDNS) (linkDNS, error) {
	if config.customServers {
		return config, nil
	}
	// Compare each property separately to remove a partially applied write.
	// Leave any externally replaced property in place.
	if slices.Equal(config.servers, owner.Servers) {
		if err := c.systemDNS.setServers(ctx, owner.Link.Index, nil); err != nil {
			return config, fmt.Errorf("clear DNS on %s: %w", owner.Interface, err)
		}
		config.servers = nil
	}
	if slices.Equal(config.domains, []dnsDomain{rootDNSDomain}) {
		if err := c.systemDNS.setDomains(ctx, owner.Link.Index, nil); err != nil {
			return config, fmt.Errorf("clear DNS domain on %s: %w", owner.Interface, err)
		}
		config.domains = nil
	}
	return config, nil
}

func (c *Connector) sameDNSLink(owner systemDNSOwner) bool {
	link, err := c.routeOperationSet().linkID(owner.Interface)
	return err == nil && link == owner.Link
}

func dnsConfigured(config linkDNS) bool {
	return len(config.servers) > 0 || len(config.domains) > 0
}

func hasOwnedDNS(config linkDNS, owner systemDNSOwner) bool {
	return !config.customServers && slices.Equal(config.servers, owner.Servers) && slices.Equal(config.domains, []dnsDomain{rootDNSDomain})
}

func canWriteDNS(config linkDNS, owner systemDNSOwner) bool {
	return !config.customServers && (len(config.servers) == 0 || slices.Equal(config.servers, owner.Servers)) &&
		(len(config.domains) == 0 || slices.Equal(config.domains, []dnsDomain{rootDNSDomain}))
}

func withoutDNSOwner(owners []systemDNSOwner, name string) []systemDNSOwner {
	return slices.DeleteFunc(slices.Clone(owners), func(owner systemDNSOwner) bool { return owner.Interface == name })
}

func (c *Connector) preferredDNSOwner(owners []systemDNSOwner, routes []netlink.DefaultRoute) (*systemDNSOwner, error) {
	for i := len(owners) - 1; i >= 0; i-- {
		owner := owners[i]
		if !c.sameDNSLink(owner) {
			continue
		}
		preferred, err := defaultRoutesFromState(owner.Routes)
		if err != nil {
			return nil, err
		}
		// A prior owner can still be preferred for the other IP family.
		if slices.ContainsFunc(preferred, func(route netlink.DefaultRoute) bool { return routePreferred(route, routes) }) {
			return &owner, nil
		}
	}
	return nil, nil
}

func (c *Connector) restoreSystemDNS(ctx context.Context, state systemDNSState, name string) error {
	owners := withoutDNSOwner(state.Owners, name)
	previous := state.Owners
	if state.Pending != nil {
		previous = append(slices.Clone(previous), *state.Pending)
	}
	routes, err := c.routeOperationSet().defaultRoutes()
	if err != nil {
		return err
	}
	owners = slices.DeleteFunc(owners, func(owner systemDNSOwner) bool {
		id, err := c.routeOperationSet().linkID(owner.Interface)
		return errors.Is(err, os.ErrNotExist) || err == nil && id != owner.Link
	})
	next, err := c.preferredDNSOwner(owners, routes)
	if err != nil {
		return err
	}
	if err := c.switchSystemDNS(ctx, previous, next); err != nil {
		if !errors.Is(err, errSystemDNSConflict) {
			return err
		}
		// A VPN may have claimed ~. during a rollback or disconnect. Clear our
		// registrations and let the host policy take effect in that case.
		if cleanupErr := c.switchSystemDNS(ctx, previous, nil); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		slog.Debug("system default DNS controlled by another connection", "error", err)
	}
	if state.Pending == nil && len(state.Owners) == len(owners) {
		return nil
	}
	state.Owners = owners
	state.Pending = nil
	return c.saveSystemDNS(ctx, state)
}

func (c *Connector) releaseSystemDNS(ctx context.Context, name string) {
	c.releaseDNSOwner(ctx, func(owner systemDNSOwner) bool { return owner.Interface == name })
}

func (c *Connector) releaseModemSystemDNS(ctx context.Context, modemID string) {
	c.releaseDNSOwner(ctx, func(owner systemDNSOwner) bool { return owner.Modem == modemID })
}

func (c *Connector) releaseDNSOwner(ctx context.Context, matches func(systemDNSOwner) bool) {
	if c.systemDNS == nil || c.state == nil {
		return
	}
	state, err := c.loadSystemDNS(ctx)
	if err != nil {
		slog.Warn("read DNS state for cleanup", "error", err)
		return
	}
	if state.Pending != nil {
		return
	}
	for _, owner := range state.Owners {
		if matches(owner) {
			if err := c.restoreSystemDNS(ctx, state, owner.Interface); err != nil {
				slog.Warn("release default DNS", "interface", owner.Interface, "error", err)
			}
			return
		}
	}
}

func (c *Connector) recoverSystemDNS(ctx context.Context) error {
	if c.systemDNS == nil || c.state == nil {
		return nil
	}
	defer c.lockRoutes()()
	state, err := c.loadSystemDNS(ctx)
	if err != nil || (state.Pending == nil && len(state.Owners) == 0) {
		return err
	}
	return c.restoreSystemDNS(ctx, state, "")
}

func (c *Connector) adoptSystemDNS(ctx context.Context, modemID string, tracked trackedConnection) {
	if c.systemDNS == nil || !tracked.prefs.DefaultRoute {
		return
	}
	state, err := c.loadSystemDNS(ctx)
	if err == nil && state.Pending == nil && len(state.Owners) == 0 {
		_, found, loadErr := c.persistence.loadRouteStateForModem(ctx, modemID, tracked.interfaceName)
		err = loadErr
		// Most legacy defaults have no takeover record. Use the live bearer
		// and its exact static, source-addressed routes as corroboration.
		if err == nil && !found && (len(tracked.routes) == 0 || slices.ContainsFunc(tracked.routes, func(route netlink.DefaultRoute) bool {
			return !route.Source.IsValid() || route.Protocol != 0
		})) {
			slog.Debug("cannot establish ownership of legacy default route", "interface", tracked.interfaceName)
			return
		}
		if err == nil {
			c.configureDefaultRouteDNS(ctx, modemID, tracked)
		}
	}
	if err != nil {
		slog.Warn("adopt system default DNS", "interface", tracked.interfaceName, "error", err)
	}
}
