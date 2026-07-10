//go:build ims

package call

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pims "github.com/damonto/sigmo/pro/ims"
)

const imsHangupCleanupTimeout = 3 * time.Second

type callActions struct {
	records *callRecords
	routes  *callRoutes
}

func (a *callActions) Dial(ctx context.Context, modem *mmodem.Modem, number string, route string) (storage.Call, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return storage.Call{}, ErrNumberRequired
	}
	route = normalizeRoute(route)
	if !validRoute(route) {
		return storage.Call{}, ErrInvalidRoute
	}
	if isUSSDDialString(number) {
		return storage.Call{}, ErrUSSDDialString
	}
	number, err := normalizeDialString(number)
	if err != nil {
		return storage.Call{}, err
	}
	selected, err := a.routes.selectRoute(ctx, modem, route)
	if err != nil {
		return storage.Call{}, err
	}
	switch selected {
	case RouteWiFiCalling, RouteVoLTE:
		voice := a.routes.voice(selected)
		call, err := voice.DialCall(ctx, modem, number)
		if err != nil {
			if errors.Is(err, pims.ErrNotConnected) {
				return storage.Call{}, notConnectedError(selected)
			}
			if call.ID != "" {
				failedCall := callFromIMS(call)
				if _, saveErr := a.records.saveAndPublish(ctx, failedCall); saveErr != nil {
					return storage.Call{}, errors.Join(fmt.Errorf("dial %s: %w", routeLabel(selected), err), fmt.Errorf("save failed call: %w", saveErr))
				}
			}
			return storage.Call{}, fmt.Errorf("dial %s: %w", routeLabel(selected), err)
		}
		stored := callFromIMS(call)
		stored, err = a.records.saveAndPublish(ctx, stored)
		if err != nil {
			return storage.Call{}, err
		}
		return stored, nil
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrNoRouteAvailable
	}
}

func (a *callActions) Answer(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		voice := a.routes.voice(call.Route)
		updated, err := voice.AnswerCall(ctx, modem, call.ID)
		if err := mapIMSActionError(call.Route, "answer", err); err != nil {
			return storage.Call{}, err
		}
		return a.records.saveAndPublish(ctx, callFromIMS(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (a *callActions) Reject(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		voice := a.routes.voice(call.Route)
		updated, err := voice.RejectCall(ctx, modem, call.ID)
		if err := mapIMSActionError(call.Route, "reject", err); err != nil {
			return storage.Call{}, err
		}
		return a.records.saveAndPublish(ctx, callFromIMS(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (a *callActions) Update(ctx context.Context, modem *mmodem.Modem, callID string, req UpdateRequest) (storage.Call, error) {
	req.State = strings.TrimSpace(req.State)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Hold = strings.TrimSpace(req.Hold)
	if req.State != "" && req.Hold != "" {
		return storage.Call{}, ErrCallUpdateConflict
	}
	if req.Hold != "" {
		return a.SetHold(ctx, modem, callID, req.Hold)
	}
	switch req.State {
	case StateActive:
		return a.Answer(ctx, modem, callID)
	case StateEnded:
		if req.Reason == ReasonBusy {
			return a.Reject(ctx, modem, callID)
		}
		return a.Hangup(ctx, modem, callID)
	default:
		return storage.Call{}, ErrInvalidCallState
	}
}

func (a *callActions) SetHold(ctx context.Context, modem *mmodem.Modem, callID string, hold string) (storage.Call, error) {
	hold = strings.TrimSpace(hold)
	if hold != HoldLocal && hold != HoldNone {
		return storage.Call{}, ErrInvalidCallHold
	}
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		voice := a.routes.voice(call.Route)
		var updated pims.VoiceCall
		if hold == HoldLocal {
			updated, err = voice.HoldCall(ctx, modem, call.ID)
		} else {
			updated, err = voice.ResumeCall(ctx, modem, call.ID)
		}
		if err := mapIMSActionError(call.Route, "update hold", err); err != nil {
			return storage.Call{}, err
		}
		return a.records.saveAndPublish(ctx, callFromIMS(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (a *callActions) Hangup(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		ended, err := a.endIMSCall(ctx, call)
		if err != nil {
			return storage.Call{}, err
		}
		a.cleanupIMSHangup(ctx, modem, call)
		return ended, nil
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (a *callActions) endIMSCall(ctx context.Context, call storage.Call) (storage.Call, error) {
	if isTerminalCallState(call.State) {
		return call, nil
	}
	now := time.Now()
	call.State = StateEnded
	call.EndedAt = now
	call.UpdatedAt = now
	return a.records.saveAndPublish(ctx, call)
}

func (a *callActions) cleanupIMSHangup(ctx context.Context, modem *mmodem.Modem, call storage.Call) {
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imsHangupCleanupTimeout)
		defer cancel()
		voice := a.routes.voice(call.Route)
		if voice == nil {
			return
		}
		if _, err := voice.HangupCall(cleanupCtx, modem, call.ID); err != nil {
			if errors.Is(err, pims.ErrNotConnected) || errors.Is(err, pims.ErrUnavailable) {
				slog.Debug("clean up IMS hangup", "call_id", call.ID, "route", call.Route, "error", err)
				return
			}
			slog.Warn("clean up IMS hangup", "call_id", call.ID, "route", call.Route, "error", err)
		}
	}()
}

func (a *callActions) SendDTMF(ctx context.Context, modem *mmodem.Modem, callID string, digits string) error {
	digits = strings.TrimSpace(digits)
	if digits == "" {
		return ErrDTMFDigitsRequired
	}
	if !validDTMFDigits(digits) {
		return ErrInvalidDTMFDigit
	}
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return err
	}
	if !dtmfCallState(call.State) {
		return ErrInvalidDTMFCallState
	}
	if call.Hold == HoldLocal || call.Hold == HoldLocalRemote {
		return ErrCallOnHold
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		voice := a.routes.voice(call.Route)
		if err := voice.SendCallDTMF(ctx, modem, call.ID, digits); err != nil {
			return mapIMSActionError(call.Route, "send DTMF", err)
		}
		return nil
	case RouteModem:
		return ErrModemCallingUnavailable
	default:
		return ErrInvalidRoute
	}
}

func (a *callActions) Delete(ctx context.Context, modem *mmodem.Modem, callID string) error {
	call, err := a.records.callForAction(ctx, modem, callID)
	if err != nil {
		return err
	}
	return a.records.deleteCall(ctx, call)
}

func dtmfCallState(state string) bool {
	return state == StateEarlyMedia || state == StateActive || state == StateConfirmed
}

func validDTMFDigits(digits string) bool {
	for _, digit := range digits {
		if !validDTMFDigit(digit) {
			return false
		}
	}
	return true
}

func validDTMFDigit(digit rune) bool {
	return digit >= '0' && digit <= '9' ||
		digit == '*' ||
		digit == '#' ||
		digit >= 'A' && digit <= 'D' ||
		digit >= 'a' && digit <= 'd'
}

func mapIMSActionError(route string, action string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pims.ErrNotConnected):
		return notConnectedError(route)
	case errors.Is(err, pims.ErrUnavailable):
		return ErrCallNotFound
	case errors.Is(err, pims.ErrUnsupportedDTMF):
		return ErrUnsupportedDTMF
	default:
		return fmt.Errorf("%s %s: %w", action, routeLabel(route), err)
	}
}

func notConnectedError(route string) error {
	if route == RouteVoLTE {
		return ErrVoLTENotConnected
	}
	return ErrWiFiCallingNotConnected
}

func routeLabel(route string) string {
	if route == RouteVoLTE {
		return "VoLTE"
	}
	return "Wi-Fi Calling"
}
