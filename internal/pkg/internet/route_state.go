package internet

import (
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type routeStateEntry struct {
	Modem     string
	Preferred []netlink.DefaultRoute
	Changes   []defaultRouteChange
}

type routeStateChange struct {
	Original    routeStateRoute `json:"original"`
	Replacement routeStateRoute `json:"replacement"`
}

type routeStateRoute struct {
	Interface string `json:"interface"`
	Family    int    `json:"family"`
	Protocol  int    `json:"protocol"`
	Scope     int    `json:"scope"`
	Gateway   string `json:"gateway,omitempty"`
	Source    string `json:"source,omitempty"`
	Metric    int    `json:"metric"`
}

type savedRouteState struct {
	ModemID   string
	Preferred []netlink.DefaultRoute
	Changes   []defaultRouteChange
}

func (e routeStateEntry) MarshalJSON() ([]byte, error) {
	type state struct {
		Modem     string             `json:"modem,omitempty"`
		Preferred []routeStateRoute  `json:"preferred"`
		Changes   []routeStateChange `json:"changes"`
	}
	return json.Marshal(state{
		Modem:     e.Modem,
		Preferred: routeStateRoutes(e.Preferred),
		Changes:   routeStateChanges(e.Changes),
	})
}

func (e *routeStateEntry) UnmarshalJSON(data []byte) error {
	type state struct {
		Modem     string             `json:"modem,omitempty"`
		Preferred []routeStateRoute  `json:"preferred"`
		Changes   []routeStateChange `json:"changes"`
	}
	var raw state
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	preferred, err := defaultRoutesFromState(raw.Preferred)
	if err != nil {
		return err
	}
	changes, err := defaultRouteChangesFromState(raw.Changes)
	if err != nil {
		return err
	}
	e.Modem = raw.Modem
	e.Preferred = preferred
	e.Changes = changes
	return nil
}

func routeStateRoutes(routes []netlink.DefaultRoute) []routeStateRoute {
	result := make([]routeStateRoute, 0, len(routes))
	for _, route := range routes {
		result = append(result, routeStateRouteFromDefault(route))
	}
	return result
}

func routeStateChanges(changes []defaultRouteChange) []routeStateChange {
	result := make([]routeStateChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, routeStateChange{
			Original:    routeStateRouteFromDefault(change.Original),
			Replacement: routeStateRouteFromDefault(change.Replacement),
		})
	}
	return result
}

func defaultRoutesFromState(routes []routeStateRoute) ([]netlink.DefaultRoute, error) {
	result := make([]netlink.DefaultRoute, 0, len(routes))
	for _, route := range routes {
		defaultRoute, err := defaultRouteFromState(route)
		if err != nil {
			return nil, err
		}
		result = append(result, defaultRoute)
	}
	return result, nil
}

func defaultRouteChangesFromState(changes []routeStateChange) ([]defaultRouteChange, error) {
	result := make([]defaultRouteChange, 0, len(changes))
	for _, change := range changes {
		original, err := defaultRouteFromState(change.Original)
		if err != nil {
			return nil, err
		}
		replacement, err := defaultRouteFromState(change.Replacement)
		if err != nil {
			return nil, err
		}
		result = append(result, defaultRouteChange{
			Original:    original,
			Replacement: replacement,
		})
	}
	return result, nil
}

func routeStateRouteFromDefault(route netlink.DefaultRoute) routeStateRoute {
	state := routeStateRoute{
		Interface: route.Interface,
		Family:    route.Family,
		Protocol:  route.Protocol,
		Scope:     route.Scope,
		Metric:    route.Metric,
	}
	if route.Gateway.IsValid() {
		state.Gateway = route.Gateway.String()
	}
	if route.Source.IsValid() {
		state.Source = route.Source.String()
	}
	return state
}

func defaultRouteFromState(state routeStateRoute) (netlink.DefaultRoute, error) {
	route := netlink.DefaultRoute{
		Interface: state.Interface,
		Family:    state.Family,
		Protocol:  state.Protocol,
		Scope:     state.Scope,
		Metric:    state.Metric,
	}
	if state.Gateway == "" {
		return routeWithStateSource(route, state.Source)
	}
	gateway, err := netip.ParseAddr(state.Gateway)
	if err != nil {
		return netlink.DefaultRoute{}, fmt.Errorf("parse route state gateway: %w", err)
	}
	route.Gateway = gateway
	return routeWithStateSource(route, state.Source)
}

func routeWithStateSource(route netlink.DefaultRoute, source string) (netlink.DefaultRoute, error) {
	if source == "" {
		return route, nil
	}
	addr, err := netip.ParseAddr(source)
	if err != nil {
		return netlink.DefaultRoute{}, fmt.Errorf("parse route state source: %w", err)
	}
	route.Source = addr
	return route, nil
}
