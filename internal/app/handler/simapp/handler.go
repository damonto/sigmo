package simapp

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mstk "github.com/damonto/sigmo/internal/pkg/modem/stk"
)

const (
	errorCodeSIMApplicationFailed = "sim_application_failed"
	simAppSessionMaxRetries       = 5
)

var simAppSessionRetryDelay = 2 * time.Second

var simAppWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Handler struct {
	registry *mmodem.Registry
	openCard func(context.Context, *mmodem.Modem) (mstk.Card, error)
	menus    menuCache
}

func New(registry *mmodem.Registry) *Handler {
	return &Handler{
		registry: registry,
		openCard: mstk.OpenCard,
	}
}

func (h *Handler) Session(c *echo.Context) error {
	ctx := c.Request().Context()
	device, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSIMApplicationFailed)
	}

	conn, err := simAppWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session := newWSSession(conn, cancel, device.EquipmentIdentifier, &h.menus)

	h.runSessionLoop(sessionCtx, device.EquipmentIdentifier, device, session)
	return nil
}

func (h *Handler) runSessionLoop(ctx context.Context, id string, device *mmodem.Modem, session *wsSession) {
	retries := 0
	for {
		current, ok := h.findSessionModem(ctx, id, device, session)
		if !ok {
			return
		}
		if current == nil {
			continue
		}
		device = current
		if h.runSessionAttempt(ctx, current, session) {
			return
		}
		retries++
		if retries >= simAppSessionMaxRetries {
			current.Logger().Warn("SIM Application session retry limit reached", "retries", retries)
			session.disconnect()
			return
		}
		if !sleepSessionRetry(ctx, session.disconnectCh) {
			return
		}
	}
}

func (h *Handler) findSessionModem(ctx context.Context, id string, fallback *mmodem.Modem, session *wsSession) (*mmodem.Modem, bool) {
	if h.registry == nil {
		return fallback, true
	}
	current, err := h.registry.Find(ctx, id)
	if err == nil {
		return current, true
	}
	if ctx.Err() != nil {
		session.disconnect()
		return nil, false
	}
	logger := mmodem.LoggerForIMEI(id)
	if fallback != nil {
		logger = fallback.Logger()
	}
	logger.Warn("SIM Application modem unavailable", "error", err)
	session.sendIfConnected(statusMessage(false, session.currentProfileICCID(), nil))
	if session.disconnected() {
		return nil, false
	}
	if !sleepSessionRetry(ctx, session.disconnectCh) {
		return nil, false
	}
	return nil, true
}

func (h *Handler) runSessionAttempt(ctx context.Context, device *mmodem.Modem, session *wsSession) bool {
	session.clearRootMenu()
	card, err := h.openCardFunc()(ctx, device)
	if err != nil {
		if ctx.Err() == nil {
			device.Logger().Warn("SIM Application session unavailable", "error", err)
			session.sendIfConnected(statusMessage(false, session.currentProfileICCID(), nil))
		}
		return ctx.Err() != nil || session.disconnected()
	}
	defer func() {
		if card.Close == nil {
			return
		}
		if err := card.Close(); err != nil {
			device.Logger().Warn("close SIM Application session", "error", err)
		}
	}()

	attemptCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	session.setProfileICCID(card.ICCID)
	var cached *wsMenu
	if card.Ready == nil {
		cached = h.menus.Get(device.EquipmentIdentifier, card.ICCID)
	}
	session.setRootMenu(cached)
	if err := session.send(statusMessage(cached != nil, card.ICCID, cached)); err != nil {
		return true
	}

	wg.Go(func() {
		session.rootSelectionLoop(attemptCtx, envelopeRootSelector{sender: card.STK}, card.Ready)
	})
	runErr := make(chan error, 1)
	wg.Go(func() {
		runErr <- card.STK.Run(attemptCtx, session.callbacks())
	})

	select {
	case <-session.disconnectCh:
		cancel()
		return true
	case <-ctx.Done():
		session.disconnect()
		return true
	case err := <-runErr:
		cancel()
		session.clearRootMenu()
		if ctx.Err() == nil {
			if err != nil {
				device.Logger().Warn("SIM Application session stopped", "error", err)
			}
			session.sendIfConnected(statusMessage(false, card.ICCID, nil))
		}
		return ctx.Err() != nil || session.disconnected()
	}
}

func sleepSessionRetry(ctx context.Context, disconnectCh <-chan struct{}) bool {
	if simAppSessionRetryDelay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-disconnectCh:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(simAppSessionRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-disconnectCh:
		return false
	case <-timer.C:
		return true
	}
}

func (h *Handler) openCardFunc() func(context.Context, *mmodem.Modem) (mstk.Card, error) {
	if h.openCard != nil {
		return h.openCard
	}
	return mstk.OpenCard
}
