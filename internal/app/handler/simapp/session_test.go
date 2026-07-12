package simapp

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	stkpkg "github.com/damonto/wwan-go/sim/stk"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mstk "github.com/damonto/sigmo/internal/pkg/modem/stk"
)

type fakeWSConn struct {
	mu     sync.Mutex
	writes []wsServerMessage
}

func (c *fakeWSConn) ReadJSON(any) error {
	select {}
}

func (c *fakeWSConn) WriteJSON(v any) error {
	msg, ok := v.(wsServerMessage)
	if !ok {
		panic("unexpected websocket message type")
	}
	c.mu.Lock()
	c.writes = append(c.writes, msg)
	c.mu.Unlock()
	return nil
}

func (c *fakeWSConn) Close() error { return nil }

func (c *fakeWSConn) lastWrite(t *testing.T) wsServerMessage {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writes) == 0 {
		t.Fatal("writes is empty")
	}
	return c.writes[len(c.writes)-1]
}

func (c *fakeWSConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writes)
}

func (c *fakeWSConn) writesCopy() []wsServerMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]wsServerMessage(nil), c.writes...)
}

type fakeEnvelopeSender struct {
	mu         sync.Mutex
	envelopes  []stkpkg.Envelope
	responses  []stkpkg.EnvelopeResponse
	onEnvelope func(context.Context, stkpkg.Envelope, int)
}

func (s *fakeEnvelopeSender) SendEnvelope(ctx context.Context, envelope stkpkg.Envelope) (stkpkg.EnvelopeResponse, error) {
	s.mu.Lock()
	s.envelopes = append(s.envelopes, envelope)
	sendNumber := len(s.envelopes)
	var response stkpkg.EnvelopeResponse
	if len(s.responses) > 0 {
		response = s.responses[0]
		s.responses = s.responses[1:]
	}
	onEnvelope := s.onEnvelope
	s.mu.Unlock()
	if onEnvelope != nil {
		onEnvelope(ctx, envelope, sendNumber)
	}
	if response.SW1 == 0 && response.SW2 == 0 {
		return stkpkg.EnvelopeResponse{SW1: 0x90, SW2: 0x00}, nil
	}
	return response, nil
}

func (s *fakeEnvelopeSender) firstEnvelope(t *testing.T) stkpkg.Envelope {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		if len(s.envelopes) > 0 {
			envelope := s.envelopes[0]
			s.mu.Unlock()
			return envelope
		}
		s.mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("timed out waiting for envelope")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (s *fakeEnvelopeSender) itemIDs(t *testing.T) []byte {
	t.Helper()
	s.mu.Lock()
	envelopes := append([]stkpkg.Envelope(nil), s.envelopes...)
	s.mu.Unlock()

	items := make([]byte, 0, len(envelopes))
	for _, envelope := range envelopes {
		data, err := envelope.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary() error = %v", err)
		}
		if len(data) == 0 {
			t.Fatal("envelope is empty")
		}
		items = append(items, data[len(data)-1])
	}
	return items
}

func newTestSession() (*wsSession, *fakeWSConn) {
	conn := &fakeWSConn{}
	return &wsSession{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		setupMenuCh:  make(chan struct{}),
		rootCh:       make(chan wsClientMessage, 1),
		selectCh:     make(chan wsClientMessage, 1),
		inputCh:      make(chan wsClientMessage, 1),
		inkeyCh:      make(chan wsClientMessage, 1),
		confirmCh:    make(chan wsClientMessage, 1),
		backCh:       make(chan wsClientMessage, 1),
	}, conn
}

func setProbeInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	old := setupMenuProbeInterval
	setupMenuProbeInterval = interval
	t.Cleanup(func() {
		setupMenuProbeInterval = old
	})
}

func setSessionRetryDelay(t *testing.T, delay time.Duration) {
	t.Helper()
	old := simAppSessionRetryDelay
	simAppSessionRetryDelay = delay
	t.Cleanup(func() {
		simAppSessionRetryDelay = old
	})
}

func TestSessionAttemptKeepsWebSocketOpenForRetry(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "open card fails",
			err:  errors.New("claim rejected"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, conn := newTestSession()
			handler := &Handler{
				openCard: func(context.Context, *mmodem.Modem) (mstk.Card, error) {
					return mstk.Card{}, tt.err
				},
			}

			done, opened := handler.runSessionAttempt(context.Background(), &mmodem.Modem{
				EquipmentIdentifier: "866069053145502",
			}, session)
			if done {
				t.Fatal("runSessionAttempt() = done, want retry")
			}
			if opened {
				t.Fatal("runSessionAttempt() opened = true, want false")
			}
			if session.disconnected() {
				t.Fatal("session disconnected, want websocket kept open")
			}
			writes := conn.writesCopy()
			if len(writes) != 1 {
				t.Fatalf("writes = %d, want unavailable status", len(writes))
			}
			if writes[0].Type != wsTypeStatus || writes[0].Available == nil || *writes[0].Available {
				t.Fatalf("status = %+v, want unavailable", writes[0])
			}
		})
	}
}

func TestSessionLoopStopsAfterRetryLimit(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantTry int
	}{
		{
			name:    "open card keeps failing",
			err:     errors.New("claim rejected"),
			wantTry: simAppSessionMaxRetries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setSessionRetryDelay(t, 0)
			session, conn := newTestSession()
			tries := 0
			handler := &Handler{
				openCard: func(context.Context, *mmodem.Modem) (mstk.Card, error) {
					tries++
					return mstk.Card{}, tt.err
				},
			}

			handler.runSessionLoop(context.Background(), "866069053145502", &mmodem.Modem{
				EquipmentIdentifier: "866069053145502",
			}, session)

			if tries != tt.wantTry {
				t.Fatalf("openCard calls = %d, want %d", tries, tt.wantTry)
			}
			if !session.disconnected() {
				t.Fatal("session connected, want disconnected after retry limit")
			}
			writes := conn.writesCopy()
			if len(writes) != tt.wantTry {
				t.Fatalf("writes = %d, want %d unavailable statuses", len(writes), tt.wantTry)
			}
			for i, write := range writes {
				if write.Type == wsTypeError {
					t.Fatalf("write %d type = error, want silent retry exhaustion", i)
				}
				if write.Type != wsTypeStatus || write.Available == nil || *write.Available {
					t.Fatalf("write %d = %+v, want unavailable status", i, write)
				}
			}
		})
	}
}

func TestMenuCache(t *testing.T) {
	tests := []struct {
		name      string
		imei      string
		iccid     string
		getIMEI   string
		getICCID  string
		wantFound bool
	}{
		{
			name:      "same IMEI and ICCID",
			imei:      " 866069053145502 ",
			iccid:     " 8986000000000000000 ",
			getIMEI:   "866069053145502",
			getICCID:  "8986000000000000000",
			wantFound: true,
		},
		{
			name:     "different ICCID misses",
			imei:     "866069053145502",
			iccid:    "8986000000000000000",
			getIMEI:  "866069053145502",
			getICCID: "8986000000000000001",
		},
		{
			name:     "different IMEI misses",
			imei:     "866069053145502",
			iccid:    "8986000000000000000",
			getIMEI:  "866069053145503",
			getICCID: "8986000000000000000",
		},
		{
			name:     "empty key skips cache",
			imei:     "",
			iccid:    "8986000000000000000",
			getIMEI:  "",
			getICCID: "8986000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newMenuCache()
			menu := &wsMenu{
				Kind:  menuKindRoot,
				Title: "SIM",
				Items: []wsMenuItem{{ID: 1, Label: "Balance"}},
			}
			cache.Set(tt.imei, tt.iccid, newMenuSnapshot(menu, menuSourceProbe, 0xff))

			got := cache.Get(tt.getIMEI, tt.getICCID)
			if (got.menu != nil) != tt.wantFound {
				t.Fatalf("Get() found = %t, want %t", got.menu != nil, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			if got.source != menuSourceProbe || got.probeItem != 0xff {
				t.Fatalf("Get() source = %d probeItem = 0x%02X, want probe 0xFF", got.source, got.probeItem)
			}
			if !got.hasProbeItem {
				t.Fatal("Get() hasProbeItem = false, want true")
			}

			menu.Items[0].Label = "changed"
			if got.menu.Items[0].Label != "Balance" {
				t.Fatalf("cached menu label = %q, want Balance", got.menu.Items[0].Label)
			}
			got.menu.Items[0].Label = "mutated"
			again := cache.Get(tt.getIMEI, tt.getICCID)
			if again.menu.Items[0].Label != "Balance" {
				t.Fatalf("cached menu label after returned mutation = %q, want Balance", again.menu.Items[0].Label)
			}
		})
	}
}

func TestSetupMenuSignalResetsBetweenAttempts(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "probe runs after previous setup menu"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, _ := newTestSession()
			session.markSetupMenuSeen()
			select {
			case <-session.setupMenuSeen():
			default:
				t.Fatal("setupMenuSeen is open, want closed")
			}

			session.resetSetupMenuSignal()
			select {
			case <-session.setupMenuSeen():
				t.Fatal("setupMenuSeen is closed after reset")
			default:
			}

			sender := &fakeEnvelopeSender{}
			probed, err := session.sendSetupMenuProbes(context.Background(), sender)
			if err != nil {
				t.Fatalf("sendSetupMenuProbes() error = %v", err)
			}
			if probed {
				t.Fatal("sendSetupMenuProbes() = true, want false")
			}
			if got := sender.itemIDs(t); len(got) != 256 {
				t.Fatalf("probe items length = %d, want 256", len(got))
			}
		})
	}
}

func TestSetupMenuProbe(t *testing.T) {
	tests := []struct {
		name      string
		setupSeen bool
		onSend    func(t *testing.T, session *wsSession) func(context.Context, stkpkg.Envelope, int)
		wantProbe bool
		wantItems []byte
		wantLen   int
	}{
		{
			name:      "setup menu already seen skips probe",
			setupSeen: true,
		},
		{
			name: "stops when select item appears",
			onSend: func(t *testing.T, session *wsSession) func(context.Context, stkpkg.Envelope, int) {
				t.Helper()
				return func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 3 {
						return
					}
					resp, err := session.selectItem(ctx, stkpkg.SelectItemCommand{
						MenuCommand: stkpkg.MenuCommand{
							Title: &stkpkg.AlphaIdentifier{Value: "SIM"},
							Items: []stkpkg.Item{
								{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "Balance"}},
							},
						},
					})
					if err != nil {
						t.Fatalf("selectItem() error = %v", err)
					}
					if resp.Result != stkpkg.ResultUserTermination {
						t.Fatalf("selectItem() result = %v, want user termination", resp.Result)
					}
				}
			},
			wantProbe: true,
			wantItems: []byte{0x00, 0x01, 0x02},
		},
		{
			name: "display text is acknowledged and scan continues",
			onSend: func(t *testing.T, session *wsSession) func(context.Context, stkpkg.Envelope, int) {
				t.Helper()
				return func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					switch sendNumber {
					case 2:
						resp, err := session.displayText(ctx, stkpkg.DisplayTextCommand{
							Text: stkpkg.TextString{Value: "Card info"},
						})
						if err != nil {
							t.Fatalf("displayText() error = %v", err)
						}
						if resp.Result != stkpkg.ResultCommandPerformed {
							t.Fatalf("displayText() result = %v, want command performed", resp.Result)
						}
					case 4:
						resp, err := session.selectItem(ctx, stkpkg.SelectItemCommand{
							MenuCommand: stkpkg.MenuCommand{
								Items: []stkpkg.Item{
									{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "ESIMLIST"}},
								},
							},
						})
						if err != nil {
							t.Fatalf("selectItem() error = %v", err)
						}
						if resp.Result != stkpkg.ResultUserTermination {
							t.Fatalf("selectItem() result = %v, want user termination", resp.Result)
						}
					}
				}
			},
			wantProbe: true,
			wantItems: []byte{0x00, 0x01, 0x02, 0x03},
		},
		{
			name:    "scans every item when no proactive command appears",
			wantLen: 256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, _ := newTestSession()
			if tt.setupSeen {
				session.markSetupMenuSeen()
			}
			sender := &fakeEnvelopeSender{}
			if tt.onSend != nil {
				sender.onEnvelope = tt.onSend(t, session)
			}
			probed, err := session.sendSetupMenuProbes(context.Background(), sender)
			if err != nil {
				t.Fatalf("sendSetupMenuProbes() error = %v", err)
			}
			if probed != tt.wantProbe {
				t.Fatalf("sendSetupMenuProbes() = %t, want %t", probed, tt.wantProbe)
			}
			got := sender.itemIDs(t)
			if tt.wantLen > 0 {
				if len(got) != tt.wantLen {
					t.Fatalf("probe items length = %d, want %d", len(got), tt.wantLen)
				}
				if got[0] != 0x00 || got[len(got)-1] != 0xff {
					t.Fatalf("probe item bounds = 0x%02X..0x%02X, want 0x00..0xFF", got[0], got[len(got)-1])
				}
				return
			}
			if !bytes.Equal(got, tt.wantItems) {
				t.Fatalf("probe items = % X, want % X", got, tt.wantItems)
			}
		})
	}
}

func TestSetupMenuProbeIgnoresDisplayTextOnly(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "display text is acked but not cached as menu"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, conn := newTestSession()
			session.imei = "866069053145502"
			session.menus = newMenuCache()
			session.setProfileICCID("8986000000000000000")
			sender := &fakeEnvelopeSender{
				onEnvelope: func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 2 {
						return
					}
					resp, err := session.displayText(ctx, stkpkg.DisplayTextCommand{
						Text: stkpkg.TextString{Value: "Card info"},
					})
					if err != nil {
						t.Fatalf("displayText() error = %v", err)
					}
					if resp.Result != stkpkg.ResultCommandPerformed {
						t.Fatalf("displayText() result = %v, want command performed", resp.Result)
					}
				},
			}

			probed, err := session.sendSetupMenuProbes(context.Background(), sender)
			if err != nil {
				t.Fatalf("sendSetupMenuProbes() error = %v", err)
			}
			if probed {
				t.Fatal("sendSetupMenuProbes() = true, want false")
			}
			if got := sender.itemIDs(t); len(got) != 256 {
				t.Fatalf("probe items length = %d, want 256", len(got))
			}
			if conn.writeCount() != 0 {
				t.Fatalf("writes = %d, want no websocket messages", conn.writeCount())
			}
			if cached := session.menus.Get(session.imei, session.currentProfileICCID()); cached.menu != nil {
				t.Fatalf("cached menu = %+v, want nil", cached.menu)
			}
		})
	}
}

func TestSetupMenuAvailability(t *testing.T) {
	tests := []struct {
		name      string
		items     []stkpkg.Item
		available bool
	}{
		{
			name:      "empty menu unavailable",
			items:     nil,
			available: false,
		},
		{
			name: "valid menu available",
			items: []stkpkg.Item{
				{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "Balance"}},
			},
			available: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, conn := newTestSession()
			session.imei = "866069053145502"
			session.menus = newMenuCache()
			session.setProfileICCID("8986000000000000000")
			resp, err := session.setupMenu(context.Background(), stkpkg.SetupMenuCommand{
				MenuCommand: stkpkg.MenuCommand{
					Title: &stkpkg.AlphaIdentifier{Value: "SIM"},
					Items: tt.items,
				},
			})
			if err != nil {
				t.Fatalf("setupMenu() error = %v", err)
			}
			if resp.Result != stkpkg.ResultCommandPerformed {
				t.Fatalf("setupMenu() result = %v, want command performed", resp.Result)
			}
			status := conn.writes[0]
			if status.Available == nil || *status.Available != tt.available {
				t.Fatalf("status.available = %v, want %t", status.Available, tt.available)
			}
			if status.ProfileICCID != "8986000000000000000" {
				t.Fatalf("status.profileIccid = %q, want ICCID", status.ProfileICCID)
			}
			if tt.available && conn.writeCount() != 2 {
				t.Fatalf("writes = %d, want status and menu", conn.writeCount())
			}
			cached := session.menus.Get(session.imei, session.currentProfileICCID())
			if (cached.menu != nil) != tt.available {
				t.Fatalf("cached menu found = %t, want %t", cached.menu != nil, tt.available)
			}
			if tt.available && cached.source != menuSourceSetup {
				t.Fatalf("cached menu source = %d, want setup", cached.source)
			}
		})
	}
}

func TestProbeSelectItemCachesRootMenuWithoutPopup(t *testing.T) {
	tests := []struct {
		name string
		cmd  stkpkg.SelectItemCommand
	}{
		{
			name: "probed select item becomes cached root menu",
			cmd: stkpkg.SelectItemCommand{
				MenuCommand: stkpkg.MenuCommand{
					Title: &stkpkg.AlphaIdentifier{Value: "SIM"},
					Items: []stkpkg.Item{
						{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "Balance"}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, conn := newTestSession()
			session.imei = "866069053145502"
			session.menus = newMenuCache()
			session.setProfileICCID("8986000000000000000")

			sender := &fakeEnvelopeSender{
				onEnvelope: func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 4 {
						return
					}
					resp, err := session.selectItem(ctx, tt.cmd)
					if err != nil {
						t.Fatalf("selectItem() error = %v", err)
					}
					if resp.Result != stkpkg.ResultUserTermination {
						t.Fatalf("selectItem() result = %v, want user termination", resp.Result)
					}
				},
			}
			probed, err := session.sendSetupMenuProbes(context.Background(), sender)
			if err != nil {
				t.Fatalf("sendSetupMenuProbes() error = %v", err)
			}
			if !probed {
				t.Fatal("sendSetupMenuProbes() = false, want true")
			}
			if got := sender.itemIDs(t); !bytes.Equal(got, []byte{0x00, 0x01, 0x02, 0x03}) {
				t.Fatalf("probe items = % X, want 00 01 02 03", got)
			}

			writes := conn.writesCopy()
			if len(writes) != 1 {
				t.Fatalf("writes = %d, want only status", len(writes))
			}
			status := writes[0]
			if status.Type != wsTypeStatus {
				t.Fatalf("message type = %q, want status", status.Type)
			}
			if status.Available == nil || !*status.Available {
				t.Fatalf("status.available = %v, want true", status.Available)
			}
			if status.Menu == nil || status.Menu.Kind != menuKindRoot {
				t.Fatalf("status.menu = %+v, want root menu", status.Menu)
			}
			cached := session.menus.Get(session.imei, session.currentProfileICCID())
			if cached.menu == nil || cached.menu.Kind != menuKindRoot || cached.menu.Items[0].Label != "Balance" {
				t.Fatalf("cached menu = %+v, want root Balance menu", cached)
			}
			if cached.source != menuSourceProbe || cached.probeItem != 0x03 || !cached.hasProbeItem {
				t.Fatalf("cached source = %d probeItem = 0x%02X hasProbeItem=%t, want probe 0x03", cached.source, cached.probeItem, cached.hasProbeItem)
			}
		})
	}
}

func TestProbeRootMenuSelectionReactivatesAndAnswersSelectItem(t *testing.T) {
	tests := []struct {
		name          string
		cached        menuSnapshot
		itemID        int
		wantProbeItem byte
	}{
		{
			name:          "uses cached probe item",
			cached:        newMenuSnapshot(nil, menuSourceProbe, 0xff),
			itemID:        1,
			wantProbeItem: 0xff,
		},
		{
			name:          "zero is a valid cached probe item",
			cached:        newMenuSnapshot(nil, menuSourceProbe, 0x00),
			itemID:        2,
			wantProbeItem: 0x00,
		},
		{
			name:          "falls back to zero without cached probe item",
			cached:        menuSnapshot{source: menuSourceProbe},
			itemID:        1,
			wantProbeItem: 0x00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, conn := newTestSession()
			session.imei = "866069053145502"
			session.menus = newMenuCache()
			session.setProfileICCID("8986000000000000000")
			root := &wsMenu{
				Kind: menuKindRoot,
				Items: []wsMenuItem{
					{ID: 1, Label: "ESIMLIST"},
					{ID: 2, Label: "CARDINFO"},
				},
			}
			cached := tt.cached
			cached.menu = root
			session.setRootMenu(cached)

			var resp stkpkg.TerminalResponse
			sender := &fakeEnvelopeSender{
				onEnvelope: func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 1 {
						return
					}
					var err error
					resp, err = session.selectItem(ctx, stkpkg.SelectItemCommand{
						MenuCommand: stkpkg.MenuCommand{
							Title: &stkpkg.AlphaIdentifier{Value: "9eSIM"},
							Items: []stkpkg.Item{
								{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "ESIMLIST"}},
								{Identifier: 2, Text: stkpkg.AlphaIdentifier{Value: "CARDINFO"}},
							},
						},
					})
					if err != nil {
						t.Fatalf("selectItem() error = %v", err)
					}
				},
			}
			err := session.selectRootMenu(context.Background(), envelopeRootSelector{sender: sender}, wsClientMessage{
				Type:   wsTypeMenuSelection,
				ItemID: tt.itemID,
			})
			if err != nil {
				t.Fatalf("selectRootMenu() error = %v", err)
			}
			if got := sender.itemIDs(t); !bytes.Equal(got, []byte{tt.wantProbeItem}) {
				t.Fatalf("probe items = % X, want % X", got, []byte{tt.wantProbeItem})
			}

			if resp.Result != stkpkg.ResultCommandPerformed {
				t.Fatalf("selectItem() result = %v, want command performed", resp.Result)
			}
			if resp.ItemIdentifier == nil || *resp.ItemIdentifier != byte(tt.itemID) {
				t.Fatalf("ItemIdentifier = %v, want %d", resp.ItemIdentifier, tt.itemID)
			}
			if conn.writeCount() != 0 {
				t.Fatalf("writes = %d, want no popup messages", conn.writeCount())
			}
		})
	}
}

func TestProbeRootMenuSelectionRequiresSelectItem(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "accepted envelope without menu tries next item"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, _ := newTestSession()
			root := &wsMenu{
				Kind:  menuKindRoot,
				Items: []wsMenuItem{{ID: 1, Label: "ESIMLIST"}},
			}
			session.setRootMenu(newMenuSnapshot(root, menuSourceProbe, 0xff))

			var resp stkpkg.TerminalResponse
			sender := &fakeEnvelopeSender{
				onEnvelope: func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 2 {
						return
					}
					var err error
					resp, err = session.selectItem(ctx, stkpkg.SelectItemCommand{
						MenuCommand: stkpkg.MenuCommand{
							Items: []stkpkg.Item{{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "ESIMLIST"}}},
						},
					})
					if err != nil {
						t.Fatalf("selectItem() error = %v", err)
					}
				},
			}

			if err := session.selectRootMenu(context.Background(), envelopeRootSelector{sender: sender}, wsClientMessage{
				Type:   wsTypeMenuSelection,
				ItemID: 1,
			}); err != nil {
				t.Fatalf("selectRootMenu() error = %v", err)
			}
			if got := sender.itemIDs(t); !bytes.Equal(got, []byte{0xff, 0x00}) {
				t.Fatalf("probe items = % X, want FF 00", got)
			}
			if resp.ItemIdentifier == nil || *resp.ItemIdentifier != 1 {
				t.Fatalf("ItemIdentifier = %v, want 1", resp.ItemIdentifier)
			}
		})
	}
}

func TestProbeRootMenuSelectionAutoAcknowledgesDisplayText(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "display text does not clear pending select item"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProbeInterval(t, 0)
			session, conn := newTestSession()
			root := &wsMenu{
				Kind:  menuKindRoot,
				Items: []wsMenuItem{{ID: 1, Label: "ESIMLIST"}},
			}
			session.setRootMenu(newMenuSnapshot(root, menuSourceProbe, 0xff))

			var displayResp stkpkg.TerminalResponse
			var selectResp stkpkg.TerminalResponse
			sender := &fakeEnvelopeSender{
				onEnvelope: func(ctx context.Context, _ stkpkg.Envelope, sendNumber int) {
					if sendNumber != 1 {
						return
					}
					var err error
					displayResp, err = session.displayText(ctx, stkpkg.DisplayTextCommand{
						Text: stkpkg.TextString{Value: "Loading"},
					})
					if err != nil {
						t.Fatalf("displayText() error = %v", err)
					}
					selectResp, err = session.selectItem(ctx, stkpkg.SelectItemCommand{
						MenuCommand: stkpkg.MenuCommand{
							Items: []stkpkg.Item{{Identifier: 1, Text: stkpkg.AlphaIdentifier{Value: "ESIMLIST"}}},
						},
					})
					if err != nil {
						t.Fatalf("selectItem() error = %v", err)
					}
				},
			}
			if err := session.selectRootMenu(context.Background(), envelopeRootSelector{sender: sender}, wsClientMessage{
				Type:   wsTypeMenuSelection,
				ItemID: 1,
			}); err != nil {
				t.Fatalf("selectRootMenu() error = %v", err)
			}

			if displayResp.Result != stkpkg.ResultCommandPerformed {
				t.Fatalf("displayText() result = %v, want command performed", displayResp.Result)
			}
			if selectResp.ItemIdentifier == nil || *selectResp.ItemIdentifier != 1 {
				t.Fatalf("ItemIdentifier = %v, want 1", selectResp.ItemIdentifier)
			}
			if conn.writeCount() != 0 {
				t.Fatalf("writes = %d, want no popup messages", conn.writeCount())
			}
		})
	}
}

func TestRootMenuSelectionSendsEnvelope(t *testing.T) {
	session, _ := newTestSession()
	sender := &fakeEnvelopeSender{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go session.rootSelectionLoop(ctx, envelopeRootSelector{sender: sender})

	session.rootCh <- wsClientMessage{Type: wsTypeMenuSelection, ItemID: 2, HelpRequested: true}

	envelope := sender.firstEnvelope(t)
	got, err := envelope.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := []byte{0xD3, 0x09, 0x82, 0x02, 0x01, 0x81, 0x90, 0x01, 0x02, 0x95, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("envelope = % X, want % X", got, want)
	}
}

func TestCommandResponses(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse
		want func(t *testing.T, resp stkpkg.TerminalResponse, conn *fakeWSConn)
	}{
		{
			name: "select item returns selected identifier",
			run: func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse {
				sendErr := sendAfterWrite(conn, func() {
					session.selectCh <- wsClientMessage{Type: wsTypeMenuSelection, ItemID: 7}
				})
				resp, err := session.selectItem(context.Background(), stkpkg.SelectItemCommand{
					MenuCommand: stkpkg.MenuCommand{
						Items: []stkpkg.Item{{Identifier: 7, Text: stkpkg.AlphaIdentifier{Value: "Start"}}},
					},
				})
				if err != nil {
					t.Fatalf("selectItem() error = %v", err)
				}
				if err := <-sendErr; err != nil {
					t.Fatalf("send response: %v", err)
				}
				return resp
			},
			want: func(t *testing.T, resp stkpkg.TerminalResponse, conn *fakeWSConn) {
				if resp.ItemIdentifier == nil || *resp.ItemIdentifier != 7 {
					t.Fatalf("ItemIdentifier = %v, want 7", resp.ItemIdentifier)
				}
				if conn.lastWrite(t).Kind != menuKindSelectItem {
					t.Fatalf("message kind = %q, want select-item", conn.lastWrite(t).Kind)
				}
			},
		},
		{
			name: "display text accepts confirmation",
			run: func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse {
				sendErr := sendAfterWrite(conn, func() {
					session.confirmCh <- wsClientMessage{Type: wsTypeConfirmResponse, Accepted: true}
				})
				resp, err := session.displayText(context.Background(), stkpkg.DisplayTextCommand{
					Text: stkpkg.TextString{Value: "Hello"},
				})
				if err != nil {
					t.Fatalf("displayText() error = %v", err)
				}
				if err := <-sendErr; err != nil {
					t.Fatalf("send response: %v", err)
				}
				return resp
			},
			want: func(t *testing.T, resp stkpkg.TerminalResponse, conn *fakeWSConn) {
				if resp.Result != stkpkg.ResultCommandPerformed {
					t.Fatalf("result = %v, want command performed", resp.Result)
				}
				if conn.lastWrite(t).Type != wsTypeDisplayText {
					t.Fatalf("message type = %q, want display_text", conn.lastWrite(t).Type)
				}
			},
		},
		{
			name: "get input returns text",
			run: func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse {
				sendErr := sendAfterWrite(conn, func() {
					session.inputCh <- wsClientMessage{Type: wsTypeInputResponse, Text: "1234"}
				})
				resp, err := session.getInput(context.Background(), stkpkg.GetInputCommand{
					Text:   stkpkg.TextString{Value: "PIN"},
					Length: stkpkg.ResponseLength{Min: 1, Max: 8},
				})
				if err != nil {
					t.Fatalf("getInput() error = %v", err)
				}
				if err := <-sendErr; err != nil {
					t.Fatalf("send response: %v", err)
				}
				return resp
			},
			want: func(t *testing.T, resp stkpkg.TerminalResponse, _ *fakeWSConn) {
				if resp.Text == nil || resp.Text.String() != "1234" {
					t.Fatalf("Text = %+v, want 1234", resp.Text)
				}
			},
		},
		{
			name: "get inkey returns text",
			run: func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse {
				sendErr := sendAfterWrite(conn, func() {
					session.inkeyCh <- wsClientMessage{Type: wsTypeInkeyResponse, Text: "Y"}
				})
				resp, err := session.getInkey(context.Background(), stkpkg.GetInkeyCommand{
					Text:  stkpkg.TextString{Value: "Continue?"},
					YesNo: true,
				})
				if err != nil {
					t.Fatalf("getInkey() error = %v", err)
				}
				if err := <-sendErr; err != nil {
					t.Fatalf("send response: %v", err)
				}
				return resp
			},
			want: func(t *testing.T, resp stkpkg.TerminalResponse, _ *fakeWSConn) {
				if resp.Text == nil || resp.Text.String() != "Y" {
					t.Fatalf("Text = %+v, want Y", resp.Text)
				}
			},
		},
		{
			name: "confirm command rejects user decline",
			run: func(t *testing.T, session *wsSession, conn *fakeWSConn) stkpkg.TerminalResponse {
				sendErr := sendAfterWrite(conn, func() {
					session.confirmCh <- wsClientMessage{Type: wsTypeConfirmResponse, Accepted: false}
				})
				resp, err := session.confirmSimple(context.Background(), stkpkg.SimpleCommand{
					CommandFrame: stkpkg.CommandFrame{
						Details: stkpkg.CommandDetails{Type: stkpkg.CommandSendUSSD},
					},
					Text: &stkpkg.TextString{Value: "*123#"},
				})
				if err != nil {
					t.Fatalf("confirmSimple() error = %v", err)
				}
				if err := <-sendErr; err != nil {
					t.Fatalf("send response: %v", err)
				}
				return resp
			},
			want: func(t *testing.T, resp stkpkg.TerminalResponse, conn *fakeWSConn) {
				if resp.Result != stkpkg.ResultUserDidNotAccept {
					t.Fatalf("result = %v, want user did not accept", resp.Result)
				}
				if conn.lastWrite(t).Command != "send_ussd" {
					t.Fatalf("command = %q, want send_ussd", conn.lastWrite(t).Command)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, conn := newTestSession()
			resp := tt.run(t, session, conn)
			tt.want(t, resp, conn)
		})
	}
}

func sendAfterWrite(conn *fakeWSConn, send func()) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline.C:
				errCh <- errors.New("timed out waiting for websocket write")
				return
			case <-ticker.C:
				if conn.writeCount() == 0 {
					continue
				}
				send()
				errCh <- nil
				return
			}
		}
	}()
	return errCh
}
