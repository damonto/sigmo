package call

import (
	"context"
	"errors"
	"fmt"
	"time"

	pims "github.com/damonto/sigmo/internal/app/ims"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

var (
	// ErrAutomationUnanswered means the outgoing call did not connect before its deadline.
	ErrAutomationUnanswered = errors.New("automated call was not answered")
	// ErrAutomationEarlyEnd means the call ended before its requested duration.
	ErrAutomationEarlyEnd = errors.New("automated call ended early")
	// ErrAutomationHangupUnconfirmed means the live dialog could not be confirmed ended.
	ErrAutomationHangupUnconfirmed = errors.New("automated call hangup unconfirmed")
)

const automationAnswerTimeout = 60 * time.Second

// RunTimedCall dials on the pinned SIM activation and hangs up its own dialog
// after the requested connected duration.
func (c *Calls) RunTimedCall(ctx context.Context, modem *mmodem.Modem, identity mmodem.SIMIdentity, req DialRequest, duration time.Duration, busy func() bool) error {
	if modem == nil {
		return ErrCallProfileChanged
	}
	events, unsubscribe := c.Subscribe(SubscriptionConfig{ModemID: modem.EquipmentIdentifier, Buffer: 32})
	defer unsubscribe()

	started, err := c.dialTimed(ctx, modem, identity, req, busy)
	var deadline time.Time
	if !started.AnsweredAt.IsZero() {
		deadline = started.AnsweredAt.Add(duration)
	}
	if err != nil {
		if started.ID != "" && !isTerminalCallState(started.State) {
			err = errors.Join(err, c.finishTimedCall(ctx, modem, identity, started, deadline))
		}
		return err
	}
	if deadline.IsZero() {
		if isTerminalCallState(started.State) {
			return ErrAutomationUnanswered
		}
		return errors.Join(ErrAutomationUnanswered, c.finishTimedCall(ctx, modem, identity, started, deadline))
	}
	if isTerminalCallState(started.State) {
		return timedCallEnd(started, deadline)
	}

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), c.finishTimedCall(ctx, modem, identity, started, deadline))
		case <-modem.Done():
			return c.finishTimedCall(ctx, modem, identity, started, deadline)
		case event := <-events:
			if event.Call.ID == started.ID && event.Call.Route == started.Route && isTerminalCallState(event.Call.State) {
				return timedCallEnd(event.Call, deadline)
			}
		case <-timer.C:
			return c.finishTimedCall(ctx, modem, identity, started, deadline)
		}
	}
}

func timedCallEnd(ended storage.Call, deadline time.Time) error {
	if deadline.IsZero() || ended.EndedAt.Before(deadline) {
		return ErrAutomationEarlyEnd
	}
	return nil
}

func (c *Calls) dialTimed(ctx context.Context, modem *mmodem.Modem, identity mmodem.SIMIdentity, req DialRequest, busy func() bool) (storage.Call, error) {
	owner, err := clientID(req.ClientID)
	if err != nil {
		return storage.Call{}, err
	}
	reserveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	release, err := modem.ReserveSIMSlot(reserveCtx)
	cancel()
	if err != nil {
		return storage.Call{}, fmt.Errorf("reserve call SIM: %w", err)
	}
	defer release()
	if !modem.MatchesSIMActivation(identity) {
		return storage.Call{}, ErrCallProfileChanged
	}
	if busy() {
		return storage.Call{}, ErrCallInUse
	}

	dialCtx, cancel := context.WithTimeout(ctx, automationAnswerTimeout)
	defer cancel()
	started, err := c.dialForClientReserved(dialCtx, modem, req, owner)
	if err != nil && started.AnsweredAt.IsZero() && ctx.Err() == nil &&
		(errors.Is(dialCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		err = errors.Join(ErrAutomationUnanswered, err)
	}
	return started, err
}

func (c *Calls) finishTimedCall(ctx context.Context, modem *mmodem.Modem, identity mmodem.SIMIdentity, started storage.Call, deadline time.Time) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if current, err := c.records.store.Call(cleanup, started.ID); err == nil && isTerminalCallState(current.State) {
		return timedCallEnd(current, deadline)
	}
	if !modem.MatchesSIMActivation(identity) {
		return ErrAutomationHangupUnconfirmed
	}
	release, err := modem.ReserveSIMSlot(cleanup)
	if err != nil {
		return fmt.Errorf("%w: reserve call SIM: %w", ErrAutomationHangupUnconfirmed, err)
	}
	defer release()
	if !modem.MatchesSIMActivation(identity) {
		return ErrAutomationHangupUnconfirmed
	}

	voice := c.routes.voice(started.Route)
	if voice == nil {
		return ErrAutomationHangupUnconfirmed
	}
	ended, err := voice.HangupCall(cleanup, modem, started.ID)
	if err != nil {
		// A remote hangup can race our request after the terminal-state check.
		if current, readErr := c.records.store.Call(cleanup, started.ID); readErr == nil && isTerminalCallState(current.State) {
			return timedCallEnd(current, deadline)
		}
		return fmt.Errorf("%w: %w", ErrAutomationHangupUnconfirmed, err)
	}
	_, err = c.records.saveAndPublish(cleanup, callFromIMS(ended))
	return err
}

// AutomationRoute selects an authorized, currently connected voice route.
func (c *Calls) AutomationRoute(ctx context.Context, modem *mmodem.Modem, wifi, volte bool) (string, error) {
	for _, route := range []struct {
		name    string
		allowed bool
	}{{RouteWiFiCalling, wifi}, {RouteVoLTE, volte}} {
		if !route.allowed {
			continue
		}
		status, available, err := c.routes.routeStatus(ctx, modem, route.name)
		if err != nil && !errors.Is(err, pims.ErrNotConnected) {
			return "", err
		}
		if available && status.Connected {
			return route.name, nil
		}
	}
	return "", ErrNoRouteAvailable
}
