package wwan

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/damonto/wwan-go/qcom"
	usim "github.com/damonto/wwan-go/sim"
	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

const (
	qmiCachedSetupMenuRef = ^uint32(0)
	qmiSTKAlphaIDTLV      = 0x05
	qmiSTKItemTLV         = 0x0F
	qmiSTKItemIDTLV       = 0x10
)

var qmiCATReplayWait = 500 * time.Millisecond

type qmiCATCommandSource interface {
	CachedProactiveCommand(ctx context.Context, commandID qcom.CATCachedCommandID) (qcom.CATCommand, error)
	ForceClaimCommands(ctx context.Context, config qcom.CATEventClaimConfig) (<-chan qcom.CATCommand, qcom.CATEventClaim, error)
}

type qmiSTKResponder interface {
	Respond(ctx context.Context, session usim.STKSession, response stkpkg.TerminalResponse) error
}

type qmiCATReaderConfig struct {
	Adapter              *usim.QCOM
	CAT                  qmiCATCommandSource
	Power                qmiSIMPowerControl
	Slot                 uint8
	IMEI                 string
	ConfigurationChanged bool
}

type qmiCATReader struct {
	*usim.QCOM
	cat                  qmiCATCommandSource
	power                qmiSIMPowerControl
	responder            qmiSTKResponder
	slot                 uint8
	imei                 string
	configurationChanged bool
	ready                chan struct{}
	readyOnce            sync.Once

	mu                    sync.Mutex
	cachedResponsePending bool
}

type qmiCATWatch struct {
	commands <-chan qcom.CATCommand
	cancel   context.CancelFunc
}

type qmiCachedSetupMenu struct {
	command qcom.CATCommand
	session usim.STKSession
}

func newQMICATReader(cfg qmiCATReaderConfig) *qmiCATReader {
	return &qmiCATReader{
		QCOM:                 cfg.Adapter,
		cat:                  cfg.CAT,
		power:                cfg.Power,
		responder:            cfg.Adapter,
		slot:                 cfg.Slot,
		imei:                 cfg.IMEI,
		configurationChanged: cfg.ConfigurationChanged,
		ready:                make(chan struct{}),
	}
}

func (r *qmiCATReader) Commands(ctx context.Context, profile stkpkg.Profile) (<-chan usim.STKSession, error) {
	cached, cacheErr := r.cat.CachedProactiveCommand(ctx, qcom.CATCachedCommandSetupMenu)
	claimConfig := qcom.CATEventClaimConfig{
		RawMask:          profile.QMIEventMask(),
		FullFunctionMask: profile.QMIFullFunctionMask(),
	}
	watch, err := r.claimCommands(ctx, claimConfig)
	if err != nil {
		return nil, fmt.Errorf("claim QMI CAT commands: %w", err)
	}
	r.markReady()

	cachedSession, cachedOK := r.cachedSetupMenu(cached, cacheErr)
	out := make(chan usim.STKSession, 8)
	go r.runCommands(ctx, out, watch, claimConfig, cachedSession, cachedOK)
	return out, nil
}

// CATReady closes after the command indication watch owns the requested CAT
// events. Callers may display cached menus before this point, but must not let
// users activate them yet.
func (r *qmiCATReader) CATReady() <-chan struct{} {
	return r.ready
}

func (r *qmiCATReader) Respond(ctx context.Context, session usim.STKSession, response stkpkg.TerminalResponse) error {
	if r.consumeCachedResponse(session.Ref) {
		return nil
	}
	return r.responder.Respond(ctx, session, response)
}

func (r *qmiCATReader) cachedSetupMenu(command qcom.CATCommand, cacheErr error) (qmiCachedSetupMenu, bool) {
	if cacheErr != nil {
		slog.Debug("QMI CAT setup menu cache unavailable", "imei", r.imei, "error", cacheErr)
		return qmiCachedSetupMenu{}, false
	}
	if r.configurationChanged {
		return qmiCachedSetupMenu{}, false
	}
	session, err := qmiSTKSession(command, qmiCachedSetupMenuRef)
	if err != nil {
		slog.Warn("decode cached QMI CAT setup menu", "imei", r.imei, "error", err)
		return qmiCachedSetupMenu{}, false
	}
	if _, ok := session.Command.(stkpkg.SetupMenuCommand); !ok {
		slog.Warn("cached QMI CAT command is not setup menu", "imei", r.imei, "command", fmt.Sprintf("%T", session.Command))
		return qmiCachedSetupMenu{}, false
	}
	return qmiCachedSetupMenu{command: command, session: session}, true
}

func (r *qmiCATReader) runCommands(
	ctx context.Context,
	out chan<- usim.STKSession,
	watch qmiCATWatch,
	claimConfig qcom.CATEventClaimConfig,
	cached qmiCachedSetupMenu,
	cachedOK bool,
) {
	defer close(out)
	defer func() { stopQMICATWatch(watch) }()

	if cachedOK {
		r.markCachedResponsePending()
		if !sendQMICATSession(ctx, out, cached.session) {
			return
		}
		slog.Info("restored QMI CAT setup menu from modem cache", "imei", r.imei)
	}
	dedupeCachedReplay := cachedOK

	var replayTimer *time.Timer
	var replayTimeout <-chan time.Time
	switch {
	case r.configurationChanged:
		if err := r.recoverSetupMenu(ctx, &watch, claimConfig); err != nil {
			if ctx.Err() == nil {
				sendQMICATSession(ctx, out, usim.STKSession{Err: err})
			}
			return
		}
	case !cachedOK:
		replayTimer = time.NewTimer(qmiCATReplayWait)
		replayTimeout = replayTimer.C
		defer replayTimer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-replayTimeout:
			replayTimeout = nil
			if err := r.recoverSetupMenu(ctx, &watch, claimConfig); err != nil {
				if ctx.Err() == nil {
					sendQMICATSession(ctx, out, usim.STKSession{Err: err})
				}
				return
			}
		case command, ok := <-watch.commands:
			if !ok {
				return
			}
			if dedupeCachedReplay {
				dedupeCachedReplay = false
				if sameQMICATCommand(command, cached.command) {
					slog.Info("discarded replay of cached QMI CAT setup menu", "imei", r.imei, "ref", command.Ref)
					continue
				}
			}
			session, err := qmiSTKSession(command, command.Ref)
			if err != nil {
				slog.Warn("decode live QMI CAT command", "imei", r.imei, "error", err)
				continue
			}
			if _, ok := session.Command.(stkpkg.SetupMenuCommand); ok && replayTimeout != nil {
				if !replayTimer.Stop() {
					select {
					case <-replayTimer.C:
					default:
					}
				}
				replayTimeout = nil
			}
			if !sendQMICATSession(ctx, out, session) {
				return
			}
		}
	}
}

func (r *qmiCATReader) recoverSetupMenu(ctx context.Context, watch *qmiCATWatch, claimConfig qcom.CATEventClaimConfig) error {
	stopQMICATWatch(*watch)
	*watch = qmiCATWatch{}

	if err := r.power.PowerOffSIM(ctx, r.slot); err != nil {
		return fmt.Errorf("power off SIM for QMI CAT recovery: %w", err)
	}
	slog.Info("SIM powered off for QMI CAT recovery", "imei", r.imei, "slot", r.slot)

	timer := time.NewTimer(qmiSIMPowerCycleDelay)
	<-timer.C

	// Once the SIM is off, cancellation must not leave it without power.
	restoreCtx := context.WithoutCancel(ctx)
	if err := qmiPowerOnSIM(restoreCtx, r.power, qcom.PowerOnSIMRequest{
		Slot:                r.slot,
		IgnoreHotSwapSwitch: true,
	}); err != nil {
		return fmt.Errorf("power on SIM after QMI CAT recovery: %w", err)
	}
	slog.Info("SIM powered on for QMI CAT recovery", "imei", r.imei, "slot", r.slot)
	if err := ctx.Err(); err != nil {
		return err
	}

	newWatch, err := r.claimCommands(ctx, claimConfig)
	if err != nil {
		return fmt.Errorf("reclaim QMI CAT commands after SIM power-on: %w", err)
	}
	*watch = newWatch
	return nil
}

func (r *qmiCATReader) claimCommands(ctx context.Context, config qcom.CATEventClaimConfig) (qmiCATWatch, error) {
	watchCtx, cancel := context.WithCancel(ctx)
	commands, claim, err := r.cat.ForceClaimCommands(watchCtx, config)
	if err != nil {
		cancel()
		return qmiCATWatch{}, err
	}
	if claim.ReleasedClientID != 0 {
		slog.Info(
			"claimed QMI CAT commands",
			"imei", r.imei,
			"clientID", claim.ClientID,
			"releasedClientID", claim.ReleasedClientID,
		)
	}
	return qmiCATWatch{commands: commands, cancel: cancel}, nil
}

func (r *qmiCATReader) markReady() {
	if r.ready == nil {
		return
	}
	r.readyOnce.Do(func() { close(r.ready) })
}

func stopQMICATWatch(watch qmiCATWatch) {
	if watch.cancel != nil {
		watch.cancel()
	}
	if watch.commands == nil {
		return
	}
	for range watch.commands {
	}
}

func sameQMICATCommand(a, b qcom.CATCommand) bool {
	return a.Ref == b.Ref && bytes.Equal(a.Data, b.Data)
}

func (r *qmiCATReader) markCachedResponsePending() {
	r.mu.Lock()
	r.cachedResponsePending = true
	r.mu.Unlock()
}

func (r *qmiCATReader) consumeCachedResponse(ref uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ref != qmiCachedSetupMenuRef || !r.cachedResponsePending {
		return false
	}
	r.cachedResponsePending = false
	return true
}

func qmiSTKSession(command qcom.CATCommand, ref uint32) (usim.STKSession, error) {
	var proactive stkpkg.ProactiveCommand
	if err := proactive.UnmarshalBinary(command.Data); err != nil {
		return usim.STKSession{}, err
	}
	parsed := proactive.Command
	if malformed, ok := parsed.(stkpkg.MalformedCommand); ok {
		if selectItem, ok := qmiUTF8SelectItem(malformed); ok {
			slog.Warn("accepted non-standard UTF-8 QMI CAT menu items", "ref", ref)
			parsed = selectItem
		}
	}
	return usim.STKSession{Ref: ref, Command: parsed}, nil
}

// Some 9eSIM applets put UTF-8 directly in Alpha Identifier TLVs. Keep the
// standard decoder as the primary path and recover only malformed SELECT ITEM
// commands whose text is valid, non-ASCII UTF-8.
func qmiUTF8SelectItem(command stkpkg.MalformedCommand) (stkpkg.SelectItemCommand, bool) {
	frame := command.CommandFrame
	if frame.Details.Type != stkpkg.CommandSelectItem {
		return stkpkg.SelectItemCommand{}, false
	}

	menu := stkpkg.MenuCommand{
		CommandFrame:  frame,
		HelpAvailable: frame.Details.Qualifier&0x80 != 0,
	}
	usedUTF8 := false
	if item, ok := frame.TLVs.Find(qmiSTKAlphaIDTLV); ok {
		title, fallback, err := qmiAlphaIdentifier(item.Value)
		if err != nil {
			return stkpkg.SelectItemCommand{}, false
		}
		menu.Title = &title
		usedUTF8 = usedUTF8 || fallback
	}
	if item, ok := frame.TLVs.Find(qmiSTKItemIDTLV); ok && len(item.Value) > 0 {
		menu.DefaultItem = item.Value[0]
	}
	for _, item := range frame.TLVs.All(qmiSTKItemTLV) {
		if len(item.Value) == 0 {
			return stkpkg.SelectItemCommand{}, false
		}
		text, fallback, err := qmiAlphaIdentifier(item.Value[1:])
		if err != nil {
			return stkpkg.SelectItemCommand{}, false
		}
		menu.Items = append(menu.Items, stkpkg.Item{
			Identifier: item.Value[0],
			Text:       text,
		})
		usedUTF8 = usedUTF8 || fallback
	}
	if len(menu.Items) == 0 || !usedUTF8 {
		return stkpkg.SelectItemCommand{}, false
	}
	return stkpkg.SelectItemCommand{MenuCommand: menu}, true
}

func qmiAlphaIdentifier(data []byte) (stkpkg.AlphaIdentifier, bool, error) {
	var text stkpkg.AlphaIdentifier
	standardErr := text.UnmarshalBinary(data)
	if standardErr == nil {
		return text, false, nil
	}
	if !utf8.Valid(data) || utf8.RuneCount(data) == len(data) {
		return stkpkg.AlphaIdentifier{}, false, fmt.Errorf("decode QMI CAT alpha identifier: %w", standardErr)
	}
	return stkpkg.AlphaIdentifier{Value: string(data)}, true, nil
}

func sendQMICATSession(ctx context.Context, out chan<- usim.STKSession, session usim.STKSession) bool {
	select {
	case out <- session:
		return true
	case <-ctx.Done():
		return false
	}
}
