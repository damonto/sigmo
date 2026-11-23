package internet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const routePromotionKey = "internet.route_promotion"

// RunDefaultRouteRecovery retries route restoration and optional DNS cleanup.
func (c *Connector) RunDefaultRouteRecovery(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			routeCtx, cancelRoutes := context.WithTimeout(ctx, connectionCleanupTimeout)
			routeErr := c.recoverDefaultRoutes(routeCtx)
			cancelRoutes()
			if routeErr != nil && ctx.Err() == nil {
				slog.Warn("reconcile default routes", "error", routeErr)
			}
			dnsCtx, cancelDNS := context.WithTimeout(ctx, connectionCleanupTimeout)
			dnsErr := c.recoverSystemDNS(dnsCtx)
			cancelDNS()
			if dnsErr != nil && ctx.Err() == nil {
				slog.Warn("reconcile system default DNS", "error", dnsErr)
			}
		}
	}
}

// routePromotion records the route change until it has been applied. The
// existing route state records any other routes displaced by the promotion.
type routePromotion struct {
	Modem     string            `json:"modem"`
	Interface string            `json:"interface"`
	Link      linkIdentity      `json:"link"`
	Previous  []routeStateRoute `json:"previous"`
	Added     []routeStateRoute `json:"added"`
}

func (c *Connector) loadRoutePromotion(ctx context.Context) (*routePromotion, error) {
	var pending routePromotion
	if err := c.state.Get(ctx, "system", routePromotionKey, &pending); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pending default route: %w", err)
	}
	return &pending, nil
}

func (c *Connector) beginRoutePromotion(ctx context.Context, modemID string, tracked trackedConnection) (*routePromotion, error) {
	ops := c.routeOperationSet()
	current, err := ops.defaultRoutes()
	if err != nil {
		return nil, fmt.Errorf("list default routes: %w", err)
	}
	preferred := slices.Clone(tracked.routes)
	setRouteMetric(preferred, defaultRouteMetric)
	if err := c.checkRouteTakeover(current, preferred); err != nil {
		return nil, err
	}
	if c.state == nil {
		return nil, nil
	}
	if pending, err := c.loadRoutePromotion(ctx); err != nil {
		return nil, err
	} else if pending != nil {
		return nil, fmt.Errorf("%w: a previous route change is awaiting recovery", ErrDefaultRouteConflict)
	}
	link, err := ops.linkID(tracked.interfaceName)
	if err != nil {
		return nil, fmt.Errorf("identify default route interface: %w", err)
	}
	added := slices.DeleteFunc(preferred, func(route netlink.DefaultRoute) bool {
		return defaultRouteExists(route, current)
	})
	pending := &routePromotion{
		Modem: modemID, Interface: tracked.interfaceName, Link: link,
		Previous: routeStateRoutes(tracked.routes), Added: routeStateRoutes(added),
	}
	if err := c.state.Put(ctx, "system", routePromotionKey, pending); err != nil {
		return nil, fmt.Errorf("record pending default route: %w", err)
	}
	return pending, nil
}

func (c *Connector) finishRoutePromotion(ctx context.Context) error {
	if err := c.state.Delete(ctx, "system", routePromotionKey); err != nil {
		return fmt.Errorf("clear pending default route: %w", err)
	}
	return nil
}

func (c *Connector) rollbackRoutePromotion(ctx context.Context, pending routePromotion) error {
	ops := c.routeOperationSet()
	id, err := ops.linkID(pending.Interface)
	if errors.Is(err, os.ErrNotExist) && id.Boot == pending.Link.Boot {
		return c.restoreOrphanedRouteState(ctx, pending.Modem, pending.Interface)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if id != pending.Link {
		return nil
	}
	current, err := ops.defaultRoutes()
	if err != nil {
		return err
	}
	added, err := defaultRoutesFromState(pending.Added)
	if err != nil {
		return err
	}
	if err := deleteDefaultRoutesWithOps(existingDefaultRoutes(added, current), ops); err != nil {
		return err
	}
	changes, found, err := c.persistence.loadRouteStateForModem(ctx, pending.Modem, pending.Interface)
	if err != nil {
		return err
	}
	if err := restoreDefaultRoutesWithOps(changes, ops); err != nil {
		return err
	}
	previous, err := defaultRoutesFromState(pending.Previous)
	if err != nil {
		return err
	}
	for _, route := range previous {
		if err := restoreOriginalDefaultRouteWithOps(route, ops); err != nil {
			return err
		}
	}
	if err := c.syncDefaultRouteRestore(ctx, changes); err != nil {
		return err
	}
	if found {
		return c.persistence.deleteRouteState(ctx, pending.Interface)
	}
	return nil
}

func (c *Connector) restoreOrphanedRouteState(ctx context.Context, modemID, interfaceName string) error {
	changes, found, err := c.persistence.loadRouteStateForModem(ctx, modemID, interfaceName)
	if err != nil || !found {
		return err
	}
	ops := c.routeOperationSet()
	routes, err := ops.defaultRoutes()
	if err != nil {
		return err
	}
	// A recorded route on another link can be restored if its original or
	// replacement is still present. That link may also have disappeared.
	changes = slices.DeleteFunc(changes, func(change defaultRouteChange) bool {
		return !defaultRouteExists(change.Replacement, routes) && !defaultRouteExists(change.Original, routes)
	})
	if err := restoreDefaultRoutesWithOps(changes, ops); err != nil {
		return err
	}
	if err := c.syncDefaultRouteRestore(ctx, changes); err != nil {
		return err
	}
	return c.persistence.deleteRouteState(ctx, interfaceName)
}

func (c *Connector) recoverDefaultRoutes(ctx context.Context) error {
	if c.state == nil {
		return nil
	}
	defer c.lockRoutes()()
	pending, err := c.loadRoutePromotion(ctx)
	if err != nil {
		return err
	}
	if pending != nil {
		if err := c.rollbackRoutePromotion(ctx, *pending); err != nil {
			return err
		}
		if err := c.finishRoutePromotion(ctx); err != nil {
			return err
		}
	}

	entries, err := c.persistence.loadAllRouteStates(ctx)
	if err != nil {
		return err
	}
	ops := c.routeOperationSet()
	for name, entry := range entries {
		if _, err := ops.linkID(name); errors.Is(err, os.ErrNotExist) {
			if err := c.restoreOrphanedRouteState(ctx, entry.ModemID, name); err != nil {
				return err
			}
		}
	}
	return c.reconcileDefaultRouteOwners(ctx)
}

func (c *Connector) reconcileDefaultRouteOwners(ctx context.Context) error {
	c.mu.Lock()
	owners := c.routeOwnersLocked()
	c.mu.Unlock()
	maps.DeleteFunc(owners, func(_ string, tracked trackedConnection) bool { return !tracked.prefs.DefaultRoute })
	if len(owners) == 0 {
		return nil
	}
	ops := c.routeOperationSet()
	routes, err := ops.defaultRoutes()
	if err != nil {
		return err
	}
	for _, modemID := range slices.Sorted(maps.Keys(owners)) {
		tracked := owners[modemID]
		if slices.ContainsFunc(tracked.routes, func(route netlink.DefaultRoute) bool {
			return route.Metric <= defaultRouteMetric && routePreferred(route, routes)
		}) {
			continue
		}
		if _, err := ops.linkID(tracked.interfaceName); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("identify tracked default route interface: %w", err)
		}
		updated, err := c.updateTrackedDefaultRoute(ctx, modemID, tracked, false)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.setRouteOwnerLocked(modemID, updated)
		c.preferences[modemID] = updated.prefs
		c.mu.Unlock()
		slog.Warn("released conflicting system default route", "interface", tracked.interfaceName)
		// Demotion can restore another owner's routes. Refresh the snapshot on
		// the next pass before deciding whether that owner should be demoted.
		err = c.syncAlwaysOnState(ctx, updated.profileID, updated.prefs)
		c.releaseSystemDNS(ctx, tracked.interfaceName)
		return err
	}
	return nil
}
