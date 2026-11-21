package automation

import (
	"context"
	"errors"
	"time"

	"github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/ims"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const readinessTimeout = time.Minute

var errRouteUnavailable = errors.New("automation route unavailable")

func (a Action) unavailableCode() string {
	switch {
	case a.WiFiCallingOnly:
		return "wifi_calling_unavailable"
	case a.Type == actionCall:
		return "call_unavailable"
	default:
		return "network_unavailable"
	}
}

func (e *Execution) canCall(action Action) bool {
	return e.allowsIMSAccess(action, ims.AccessWiFiCalling) || e.allowsIMSAccess(action, ims.AccessVoLTE)
}

func (e *Execution) allowsIMSAccess(action Action, access ims.Access) bool {
	switch access {
	case ims.AccessWiFiCalling:
		return action.Type == actionSMS || e.policy.Allows(feature.WiFiCalling)
	case ims.AccessVoLTE:
		return !action.WiFiCallingOnly && (action.Type == actionSMS || e.policy.Allows(feature.VoLTE))
	default:
		return false
	}
}

func (e *Execution) waitReady(ctx context.Context, line activeLine, action Action) (string, string) {
	waitCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for waitCtx.Err() == nil {
		route, code := e.readyRoute(waitCtx, line, action, false)
		if waitCtx.Err() == nil && (route != "" || code != "") {
			return route, code
		}
		select {
		case <-waitCtx.Done():
		case <-line.device.Done():
			return "", "line_unavailable"
		case <-ticker.C:
		}
	}
	if ctx.Err() == nil && !action.WiFiCallingOnly && action.Type != actionData {
		// Check once with a live context after the preferred route's deadline.
		finalCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		route, code := e.readyRoute(finalCtx, line, action, true)
		if finalCtx.Err() == nil && (route != "" || code != "") {
			return route, code
		}
	}
	return "", action.unavailableCode()
}

func (e *Execution) readyRoute(ctx context.Context, line activeLine, action Action, afterDeadline bool) (string, string) {
	device := line.device
	select {
	case <-device.Done():
		return "", "line_unavailable"
	default:
	}
	snapshot := device.Snapshot()
	if snapshot.SIMIdentity != line.identity || device.SIMReloadPending() {
		return "", "line_unavailable"
	}
	if snapshot.Locked() {
		return "", "sim_locked"
	}
	if action.Type == actionData {
		switch {
		case cellularReady(snapshot):
			return call.RouteModem, ""
		case snapshot.AirplaneMode():
			return "", "data_unavailable"
		default:
			return "", ""
		}
	}
	if action.Type == actionCall {
		if e.internet.HasOngoingCall(device.EquipmentIdentifier) {
			return "", "call_in_progress"
		}
		if !e.canCall(action) {
			return "", action.unavailableCode()
		}
	}

	if !afterDeadline {
		if e.allowsIMSAccess(action, ims.AccessWiFiCalling) {
			enabled, connected, err := e.internet.AutomationAccessStatus(ctx, device, ims.AccessWiFiCalling)
			switch {
			case err != nil:
				return "", ""
			case enabled && connected:
				return call.RouteWiFiCalling, ""
			case enabled:
				return "", ""
			}
		}
		if action.WiFiCallingOnly {
			return "", action.unavailableCode()
		}
	}
	if !e.allowsIMSAccess(action, ims.AccessVoLTE) {
		return "", action.unavailableCode()
	}
	volteEnabled, volteConnected, err := e.internet.AutomationAccessStatus(ctx, device, ims.AccessVoLTE)
	if err != nil {
		return "", ""
	}
	if volteEnabled && volteConnected {
		return call.RouteVoLTE, ""
	}
	if !afterDeadline && !volteEnabled {
		if action.Type == actionCall {
			return "", action.unavailableCode()
		}
		if cellularReady(snapshot) {
			return call.RouteModem, ""
		}
	}
	return "", ""
}

func cellularReady(snapshot mmodem.ModemSnapshot) bool {
	return !snapshot.AirplaneMode() &&
		(snapshot.Status.Registration == wwanmodem.RegistrationHome || snapshot.Status.Registration == wwanmodem.RegistrationRoaming)
}

func (e *Execution) checkRoute(ctx context.Context, device *mmodem.Modem, action Action, route string) error {
	switch route {
	case call.RouteModem:
		if action.Type != actionSMS || action.WiFiCallingOnly || !cellularReady(device.Snapshot()) {
			return errRouteUnavailable
		}
		for _, access := range []ims.Access{ims.AccessWiFiCalling, ims.AccessVoLTE} {
			enabled, _, err := e.internet.AutomationAccessStatus(ctx, device, access)
			if err != nil || enabled {
				return errRouteUnavailable
			}
		}
	case call.RouteWiFiCalling, call.RouteVoLTE:
		access := ims.Access(route)
		if !e.allowsIMSAccess(action, access) {
			return errRouteUnavailable
		}
		enabled, connected, err := e.internet.AutomationAccessStatus(ctx, device, access)
		if err != nil || !enabled || !connected {
			return errRouteUnavailable
		}
	default:
		return errRouteUnavailable
	}
	return ctx.Err()
}
