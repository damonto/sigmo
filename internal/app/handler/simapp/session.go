package simapp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	usim "github.com/damonto/wwan-go/sim"
	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

type wsConn interface {
	ReadJSON(v any) error
	WriteJSON(v any) error
	Close() error
}

type pendingKind string

const (
	pendingNone    pendingKind = ""
	pendingSelect  pendingKind = "select"
	pendingInput   pendingKind = "input"
	pendingInkey   pendingKind = "inkey"
	pendingConfirm pendingKind = "confirm"
	pendingDisplay pendingKind = "display"
)

type wsSession struct {
	conn           wsConn
	disconnectCh   chan struct{}
	disconnectOnce sync.Once
	writeMu        sync.Mutex

	stateMu           sync.RWMutex
	imei              string
	profileICCID      string
	rootMenu          *wsMenu
	rootMenuSource    menuSource
	rootMenuProbeItem byte
	hasRootProbeItem  bool
	probe             probeState
	setupMenuMu       sync.Mutex
	setupMenuCh       chan struct{}
	setupMenuClosed   bool
	menus             *menuCache

	pendingMu sync.RWMutex
	pending   pendingKind

	rootCh    chan wsClientMessage
	selectCh  chan wsClientMessage
	inputCh   chan wsClientMessage
	inkeyCh   chan wsClientMessage
	confirmCh chan wsClientMessage
	backCh    chan wsClientMessage
}

func newWSSession(conn wsConn, cancel context.CancelFunc, imei string, menus *menuCache) *wsSession {
	session := &wsSession{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		imei:         strings.TrimSpace(imei),
		setupMenuCh:  make(chan struct{}),
		menus:        menus,
		rootCh:       make(chan wsClientMessage, 1),
		selectCh:     make(chan wsClientMessage, 1),
		inputCh:      make(chan wsClientMessage, 1),
		inkeyCh:      make(chan wsClientMessage, 1),
		confirmCh:    make(chan wsClientMessage, 1),
		backCh:       make(chan wsClientMessage, 1),
	}
	go session.readLoop(cancel)
	return session
}

func (s *wsSession) disconnect() {
	s.disconnectOnce.Do(func() {
		close(s.disconnectCh)
	})
}

func (s *wsSession) readLoop(cancel context.CancelFunc) {
	defer cancel()
	defer s.disconnect()
	for {
		var msg wsClientMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case wsTypeMenuSelection:
			if s.pendingKind() == pendingSelect {
				sendLatest(s.selectCh, msg)
				continue
			}
			sendLatest(s.rootCh, msg)
		case wsTypeInputResponse:
			if s.pendingKind() == pendingInput {
				sendLatest(s.inputCh, msg)
			}
		case wsTypeInkeyResponse:
			if s.pendingKind() == pendingInkey {
				sendLatest(s.inkeyCh, msg)
			}
		case wsTypeConfirmResponse:
			switch s.pendingKind() {
			case pendingConfirm, pendingDisplay:
				sendLatest(s.confirmCh, msg)
			}
		case wsTypeBack:
			if s.pendingKind() != pendingNone {
				sendLatest(s.backCh, msg)
			}
		case wsTypeTerminate:
			return
		}
	}
}

func sendLatest(ch chan wsClientMessage, msg wsClientMessage) {
	select {
	case ch <- msg:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- msg
	}
}

func (s *wsSession) send(msg wsServerMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.WriteJSON(msg); err != nil {
		s.disconnect()
		return fmt.Errorf("write websocket message: %w", err)
	}
	return nil
}

func (s *wsSession) sendIfConnected(msg wsServerMessage) {
	select {
	case <-s.disconnectCh:
		return
	default:
	}
	_ = s.send(msg)
}

func (s *wsSession) setProfileICCID(iccid string) {
	s.stateMu.Lock()
	s.profileICCID = strings.TrimSpace(iccid)
	s.stateMu.Unlock()
}

func (s *wsSession) currentProfileICCID() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.profileICCID
}

func (s *wsSession) setRootMenu(snapshot menuSnapshot) {
	s.stateMu.Lock()
	s.rootMenu = cloneMenu(snapshot.menu)
	s.rootMenuSource = snapshot.source
	s.rootMenuProbeItem = snapshot.probeItem
	s.hasRootProbeItem = snapshot.hasProbeItem
	s.stateMu.Unlock()
}

func (s *wsSession) storeRootMenu(snapshot menuSnapshot) {
	s.stateMu.Lock()
	s.rootMenu = cloneMenu(snapshot.menu)
	s.rootMenuSource = snapshot.source
	s.rootMenuProbeItem = snapshot.probeItem
	s.hasRootProbeItem = snapshot.hasProbeItem
	imei := s.imei
	iccid := s.profileICCID
	s.stateMu.Unlock()

	if s.menus != nil {
		s.menus.Set(imei, iccid, snapshot)
	}
}

func (s *wsSession) markSetupMenuSeen() {
	s.probe.clear()

	s.setupMenuMu.Lock()
	defer s.setupMenuMu.Unlock()
	if !s.setupMenuClosed {
		close(s.setupMenuCh)
		s.setupMenuClosed = true
	}
}

func (s *wsSession) resetSetupMenuSignal() {
	s.setupMenuMu.Lock()
	s.setupMenuCh = make(chan struct{})
	s.setupMenuClosed = false
	s.setupMenuMu.Unlock()
}

func (s *wsSession) setupMenuSeen() <-chan struct{} {
	s.setupMenuMu.Lock()
	defer s.setupMenuMu.Unlock()
	return s.setupMenuCh
}

func (s *wsSession) disconnected() bool {
	select {
	case <-s.disconnectCh:
		return true
	default:
		return false
	}
}

func (s *wsSession) probing() bool {
	return s.probe.scanning()
}

func (s *wsSession) rootMenuAction() (menuSource, byte, bool) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.rootMenuSource, s.rootMenuProbeItem, s.hasRootProbeItem
}

func (s *wsSession) awaitingProbeSelection() bool {
	return s.probe.awaitingSelection()
}

func (s *wsSession) pendingKind() pendingKind {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	return s.pending
}

func (s *wsSession) beginPending(kind pendingKind) func() {
	drainClientMessages(s.selectCh)
	drainClientMessages(s.inputCh)
	drainClientMessages(s.inkeyCh)
	drainClientMessages(s.confirmCh)
	drainClientMessages(s.backCh)

	s.pendingMu.Lock()
	s.pending = kind
	s.pendingMu.Unlock()
	return func() {
		s.pendingMu.Lock()
		if s.pending == kind {
			s.pending = pendingNone
		}
		s.pendingMu.Unlock()
	}
}

func drainClientMessages(ch chan wsClientMessage) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (s *wsSession) callbacks() usim.STKCallbacks {
	return usim.STKCallbacks{
		DisplayText:   s.displayText,
		GetInkey:      s.getInkey,
		GetInput:      s.getInput,
		SetupMenu:     s.setupMenu,
		SelectItem:    s.selectItem,
		SendSMS:       s.confirmSimple,
		SendSS:        s.confirmSimple,
		SendUSSD:      s.confirmSimple,
		SendDTMF:      s.confirmSimple,
		SetupCall:     s.confirmSimple,
		LaunchBrowser: s.confirmSimple,
	}
}

func (s *wsSession) setupMenu(_ context.Context, cmd stkpkg.SetupMenuCommand) (stkpkg.TerminalResponse, error) {
	s.markSetupMenuSeen()
	menu := menuFromCommand(menuKindRoot, cmd.MenuCommand)
	if len(menu.Items) == 0 {
		s.storeRootMenu(menuSnapshot{})
		return stkpkg.OK(), s.send(statusMessage(false, s.currentProfileICCID(), nil))
	}
	s.storeRootMenu(newMenuSnapshot(&menu, menuSourceSetup, 0))
	if err := s.send(statusMessage(true, s.currentProfileICCID(), &menu)); err != nil {
		return stkpkg.TerminalResponse{}, err
	}
	return stkpkg.OK(), s.send(menuMessage(menu))
}

func (s *wsSession) selectItem(ctx context.Context, cmd stkpkg.SelectItemCommand) (stkpkg.TerminalResponse, error) {
	if selection, ok := s.probe.takeSelection(); ok {
		response, err := s.answerProbeSelection(cmd, selection)
		if err != nil {
			return response, err
		}
		sendProbeSelectionHit(selection.hitCh, probeHitSelectItem)
		return response, nil
	}
	probeMenu := menuFromCommand(menuKindRoot, cmd.MenuCommand)
	if hit, ok := s.probe.takeScanHit(probeHitSelectItem); ok {
		return s.cacheProbeMenu(&probeMenu, hit.item)
	}

	menu := menuFromCommand(menuKindSelectItem, cmd.MenuCommand)
	done := s.beginPending(pendingSelect)
	defer done()
	if err := s.send(menuMessage(menu)); err != nil {
		return stkpkg.TerminalResponse{}, err
	}

	select {
	case msg := <-s.selectCh:
		item, ok := byteFromItemID(msg.ItemID)
		if !ok {
			return stkpkg.Result(stkpkg.ResultRequiredValuesMissing), nil
		}
		return stkpkg.TerminalResponse{
			Result:         stkpkg.ResultCommandPerformed,
			ItemIdentifier: &item,
		}, nil
	case <-s.backCh:
		return stkpkg.Result(stkpkg.ResultBackwardMove), nil
	case <-ctx.Done():
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-s.disconnectCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	}
}

func (s *wsSession) cacheProbeMenu(menu *wsMenu, probeItem byte) (stkpkg.TerminalResponse, error) {
	if menu == nil || len(menu.Items) == 0 {
		s.storeRootMenu(menuSnapshot{})
		return stkpkg.Result(stkpkg.ResultRequiredValuesMissing), s.send(statusMessage(false, s.currentProfileICCID(), nil))
	}

	s.storeRootMenu(newMenuSnapshot(menu, menuSourceProbe, probeItem))
	if err := s.send(statusMessage(true, s.currentProfileICCID(), menu)); err != nil {
		return stkpkg.TerminalResponse{}, err
	}
	return stkpkg.Result(stkpkg.ResultUserTermination), nil
}

func (s *wsSession) answerProbeSelection(cmd stkpkg.SelectItemCommand, selection probeRootSelection) (stkpkg.TerminalResponse, error) {
	menu := menuFromCommand(menuKindRoot, cmd.MenuCommand)
	if len(menu.Items) > 0 {
		s.storeRootMenu(newMenuSnapshot(&menu, menuSourceProbe, selection.probeItem))
	}
	result := stkpkg.ResultCommandPerformed
	if selection.helpRequested {
		result = stkpkg.ResultHelpInformationRequired
	}
	return stkpkg.TerminalResponse{
		Result:         result,
		ItemIdentifier: &selection.item,
	}, nil
}

func (s *wsSession) displayText(ctx context.Context, cmd stkpkg.DisplayTextCommand) (stkpkg.TerminalResponse, error) {
	if _, ok := s.probe.takeScanHit(probeHitDisplayText); ok {
		return stkpkg.OK(), nil
	}
	if s.awaitingProbeSelection() {
		return stkpkg.OK(), nil
	}

	var done func()
	if !cmd.ImmediateResponse {
		done = s.beginPending(pendingDisplay)
		defer done()
	}
	if err := s.send(wsServerMessage{
		Type:              wsTypeDisplayText,
		Text:              cmd.Text.String(),
		HighPriority:      cmd.HighPriority,
		UserClear:         cmd.UserClear,
		ImmediateResponse: cmd.ImmediateResponse,
	}); err != nil {
		return stkpkg.TerminalResponse{}, err
	}
	if cmd.ImmediateResponse {
		return stkpkg.OK(), nil
	}

	select {
	case msg := <-s.confirmCh:
		if !msg.Accepted {
			return stkpkg.Result(stkpkg.ResultUserTermination), nil
		}
		return stkpkg.OK(), nil
	case <-s.backCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-ctx.Done():
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-s.disconnectCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	}
}

func (s *wsSession) getInput(ctx context.Context, cmd stkpkg.GetInputCommand) (stkpkg.TerminalResponse, error) {
	if s.probing() || s.awaitingProbeSelection() {
		return stkpkg.Result(stkpkg.ResultTerminalUnableToProcess), nil
	}

	done := s.beginPending(pendingInput)
	defer done()
	message := wsServerMessage{
		Type:          wsTypeInput,
		Text:          cmd.Text.String(),
		MinLength:     int(cmd.Length.Min),
		MaxLength:     int(cmd.Length.Max),
		HideInput:     cmd.HideInput,
		HelpAvailable: cmd.HelpAvailable,
	}
	if cmd.DefaultText != nil {
		message.DefaultText = cmd.DefaultText.String()
	}
	if err := s.send(message); err != nil {
		return stkpkg.TerminalResponse{}, err
	}

	select {
	case msg := <-s.inputCh:
		return textResponse(msg.Text), nil
	case <-s.backCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-ctx.Done():
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-s.disconnectCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	}
}

func (s *wsSession) getInkey(ctx context.Context, cmd stkpkg.GetInkeyCommand) (stkpkg.TerminalResponse, error) {
	if s.probing() || s.awaitingProbeSelection() {
		return stkpkg.Result(stkpkg.ResultTerminalUnableToProcess), nil
	}

	done := s.beginPending(pendingInkey)
	defer done()
	if err := s.send(wsServerMessage{
		Type:          wsTypeInkey,
		Text:          cmd.Text.String(),
		YesNo:         cmd.YesNo,
		HelpAvailable: cmd.HelpAvailable,
	}); err != nil {
		return stkpkg.TerminalResponse{}, err
	}

	select {
	case msg := <-s.inkeyCh:
		return textResponse(msg.Text), nil
	case <-s.backCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-ctx.Done():
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-s.disconnectCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	}
}

func (s *wsSession) confirmSimple(ctx context.Context, cmd stkpkg.SimpleCommand) (stkpkg.TerminalResponse, error) {
	if s.probing() || s.awaitingProbeSelection() {
		return stkpkg.Result(stkpkg.ResultTerminalUnableToProcess), nil
	}

	done := s.beginPending(pendingConfirm)
	defer done()
	if err := s.send(wsServerMessage{
		Type:    wsTypeConfirm,
		Command: commandName(cmd.Details.Type),
		Text:    simpleCommandText(cmd),
	}); err != nil {
		return stkpkg.TerminalResponse{}, err
	}

	select {
	case msg := <-s.confirmCh:
		if msg.Accepted {
			return stkpkg.OK(), nil
		}
		return stkpkg.Result(stkpkg.ResultUserDidNotAccept), nil
	case <-s.backCh:
		return stkpkg.Result(stkpkg.ResultUserDidNotAccept), nil
	case <-ctx.Done():
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	case <-s.disconnectCh:
		return stkpkg.Result(stkpkg.ResultUserTermination), nil
	}
}

func menuFromCommand(kind string, cmd stkpkg.MenuCommand) wsMenu {
	menu := wsMenu{
		Kind:          kind,
		HelpAvailable: cmd.HelpAvailable,
		Items:         make([]wsMenuItem, 0, len(cmd.Items)),
	}
	if cmd.Title != nil {
		menu.Title = cmd.Title.String()
	}
	if cmd.DefaultItem != 0 {
		value := int(cmd.DefaultItem)
		menu.DefaultItemID = &value
	}
	for _, item := range cmd.Items {
		menu.Items = append(menu.Items, wsMenuItem{
			ID:    int(item.Identifier),
			Label: item.Text.String(),
		})
	}
	return menu
}

func cloneMenu(menu *wsMenu) *wsMenu {
	if menu == nil {
		return nil
	}
	cloned := *menu
	cloned.Items = append([]wsMenuItem(nil), menu.Items...)
	if menu.DefaultItemID != nil {
		value := *menu.DefaultItemID
		cloned.DefaultItemID = &value
	}
	return &cloned
}

func byteFromItemID(id int) (byte, bool) {
	if id < 0 || id > 255 {
		return 0, false
	}
	return byte(id), true
}

func textResponse(text string) stkpkg.TerminalResponse {
	return stkpkg.TerminalResponse{
		Result: stkpkg.ResultCommandPerformed,
		Text:   &stkpkg.TextString{DCS: stkpkg.DCSGSM8Unpacked, Value: text},
	}
}

func simpleCommandText(cmd stkpkg.SimpleCommand) string {
	if cmd.Alpha != nil && strings.TrimSpace(cmd.Alpha.String()) != "" {
		return cmd.Alpha.String()
	}
	if cmd.Text != nil && strings.TrimSpace(cmd.Text.String()) != "" {
		return cmd.Text.String()
	}
	if strings.TrimSpace(cmd.URL) != "" {
		return cmd.URL
	}
	return commandName(cmd.Details.Type)
}

func commandName(command stkpkg.CommandType) string {
	switch command {
	case stkpkg.CommandSendSMS:
		return "send_sms"
	case stkpkg.CommandSendSS:
		return "send_ss"
	case stkpkg.CommandSendUSSD:
		return "send_ussd"
	case stkpkg.CommandSendDTMF:
		return "send_dtmf"
	case stkpkg.CommandSetupCall:
		return "setup_call"
	case stkpkg.CommandLaunchBrowser:
		return "launch_browser"
	default:
		return fmt.Sprintf("command_0x%02x", byte(command))
	}
}
