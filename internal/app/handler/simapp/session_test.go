package simapp

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	usim "github.com/damonto/wwan-go/sim"
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
	mu        sync.Mutex
	envelopes []stkpkg.Envelope
}

type fakeSTKRunner struct {
	fakeEnvelopeSender
	runErr error
}

func (r *fakeSTKRunner) Run(context.Context, usim.STKCallbacks) error {
	return r.runErr
}

func (s *fakeEnvelopeSender) SendEnvelope(_ context.Context, envelope stkpkg.Envelope) (stkpkg.EnvelopeResponse, error) {
	s.mu.Lock()
	s.envelopes = append(s.envelopes, envelope)
	s.mu.Unlock()
	return stkpkg.EnvelopeResponse{SW1: 0x90, SW2: 0x00}, nil
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

func (s *fakeEnvelopeSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.envelopes)
}

func newTestSession() (*wsSession, *fakeWSConn) {
	conn := &fakeWSConn{}
	return &wsSession{
		conn:         conn,
		disconnectCh: make(chan struct{}),
		rootCh:       make(chan wsClientMessage, 1),
		selectCh:     make(chan wsClientMessage, 1),
		inputCh:      make(chan wsClientMessage, 1),
		inkeyCh:      make(chan wsClientMessage, 1),
		confirmCh:    make(chan wsClientMessage, 1),
		backCh:       make(chan wsClientMessage, 1),
	}, conn
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

			done := handler.runSessionAttempt(context.Background(), &mmodem.Modem{
				EquipmentIdentifier: "866069053145502",
			}, session)
			if done {
				t.Fatal("runSessionAttempt() = done, want retry")
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
		name       string
		openErr    error
		runErr     error
		wantTry    int
		wantWrites int
	}{
		{
			name:       "open card keeps failing",
			openErr:    errors.New("open rejected"),
			wantTry:    simAppSessionMaxRetries,
			wantWrites: simAppSessionMaxRetries,
		},
		{
			name:       "STK run keeps failing after card opens",
			runErr:     errors.New("claim rejected"),
			wantTry:    simAppSessionMaxRetries,
			wantWrites: 2 * simAppSessionMaxRetries,
		},
		{
			name:       "STK run ends after card opens",
			wantTry:    simAppSessionMaxRetries,
			wantWrites: 2 * simAppSessionMaxRetries,
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
					if tt.openErr != nil {
						return mstk.Card{}, tt.openErr
					}
					return mstk.Card{
						ICCID: "8986000000000000000",
						STK:   &fakeSTKRunner{runErr: tt.runErr},
					}, nil
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
			if len(writes) != tt.wantWrites {
				t.Fatalf("writes = %d, want %d unavailable statuses", len(writes), tt.wantWrites)
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

func TestSessionAttemptCachedMenuVisibility(t *testing.T) {
	tests := []struct {
		name          string
		catHandshake  bool
		wantAvailable bool
	}{
		{
			name:          "transport without CAT handshake uses client cache",
			wantAvailable: true,
		},
		{
			name:         "QMI CAT handshake hides client cache",
			catHandshake: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				imei  = "866069053145502"
				iccid = "8986000000000000000"
			)
			handler := &Handler{}
			handler.menus.Set(imei, iccid, &wsMenu{
				Kind:  menuKindRoot,
				Title: "9eSIM",
				Items: []wsMenuItem{{ID: 1, Label: "eSIM List"}},
			})
			var ready <-chan struct{}
			if tt.catHandshake {
				ready = make(chan struct{})
			}
			handler.openCard = func(context.Context, *mmodem.Modem) (mstk.Card, error) {
				return mstk.Card{
					ICCID: iccid,
					STK:   &fakeSTKRunner{},
					Ready: ready,
				}, nil
			}
			session, conn := newTestSession()

			done := handler.runSessionAttempt(context.Background(), &mmodem.Modem{
				EquipmentIdentifier: imei,
			}, session)
			if done {
				t.Fatal("runSessionAttempt() = done, want retry")
			}
			writes := conn.writesCopy()
			if len(writes) != 2 {
				t.Fatalf("writes = %d, want initial and stopped statuses", len(writes))
			}
			status := writes[0]
			if status.Available == nil || *status.Available != tt.wantAvailable {
				t.Fatalf("status.available = %v, want %t", status.Available, tt.wantAvailable)
			}
			if (status.Menu != nil) != tt.wantAvailable {
				t.Fatalf("status.menu present = %t, want %t", status.Menu != nil, tt.wantAvailable)
			}
			stopped := writes[1]
			if stopped.Available == nil || *stopped.Available || stopped.Menu != nil {
				t.Fatalf("stopped status = %+v, want unavailable without menu", stopped)
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
			cache.Set(tt.imei, tt.iccid, menu)

			got := cache.Get(tt.getIMEI, tt.getICCID)
			if (got != nil) != tt.wantFound {
				t.Fatalf("Get() found = %t, want %t", got != nil, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			menu.Items[0].Label = "changed"
			if got.Items[0].Label != "Balance" {
				t.Fatalf("cached menu label = %q, want Balance", got.Items[0].Label)
			}
			got.Items[0].Label = "mutated"
			again := cache.Get(tt.getIMEI, tt.getICCID)
			if again.Items[0].Label != "Balance" {
				t.Fatalf("cached menu label after returned mutation = %q, want Balance", again.Items[0].Label)
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
			if (cached != nil) != tt.available {
				t.Fatalf("cached menu found = %t, want %t", cached != nil, tt.available)
			}
		})
	}
}

func TestRootMenuSelectionSendsEnvelope(t *testing.T) {
	session, _ := newTestSession()
	session.setRootMenu(&wsMenu{
		Kind:  menuKindRoot,
		Items: []wsMenuItem{{ID: 2, Label: "eSIM List"}},
	})
	sender := &fakeEnvelopeSender{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go session.rootSelectionLoop(ctx, envelopeRootSelector{sender: sender}, nil)

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

func TestRootSelectionLoopReadiness(t *testing.T) {
	tests := []struct {
		name       string
		waitForCAT bool
	}{
		{name: "transport without handshake is ready"},
		{name: "CAT claim gates selection", waitForCAT: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, _ := newTestSession()
			session.setRootMenu(&wsMenu{
				Kind:  menuKindRoot,
				Items: []wsMenuItem{{ID: 1, Label: "eSIM List"}},
			})
			sender := &fakeEnvelopeSender{}
			var ready chan struct{}
			var readySignal <-chan struct{}
			if tt.waitForCAT {
				ready = make(chan struct{})
				readySignal = ready
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go session.rootSelectionLoop(ctx, envelopeRootSelector{sender: sender}, readySignal)

			session.rootCh <- wsClientMessage{Type: wsTypeMenuSelection, ItemID: 1}
			if tt.waitForCAT {
				timer := time.NewTimer(20 * time.Millisecond)
				<-timer.C
				if got := sender.count(); got != 0 {
					t.Fatalf("envelopes before CAT claim = %d, want 0", got)
				}
				close(ready)
			}
			_ = sender.firstEnvelope(t)
		})
	}
}

func TestRootMenuSelectionValidatesCurrentMenu(t *testing.T) {
	tests := []struct {
		name      string
		menu      *wsMenu
		itemID    int
		wantErr   bool
		wantCalls int
	}{
		{
			name:      "current item",
			menu:      &wsMenu{Kind: menuKindRoot, Items: []wsMenuItem{{ID: 2, Label: "eSIM List"}}},
			itemID:    2,
			wantCalls: 1,
		},
		{
			name:    "stale item",
			menu:    &wsMenu{Kind: menuKindRoot, Items: []wsMenuItem{{ID: 2, Label: "eSIM List"}}},
			itemID:  3,
			wantErr: true,
		},
		{
			name:    "cleared menu",
			itemID:  2,
			wantErr: true,
		},
		{
			name:    "item outside byte range",
			menu:    &wsMenu{Kind: menuKindRoot, Items: []wsMenuItem{{ID: 2, Label: "eSIM List"}}},
			itemID:  256,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, _ := newTestSession()
			session.setRootMenu(tt.menu)
			sender := &fakeEnvelopeSender{}
			err := session.selectRootMenu(context.Background(), envelopeRootSelector{sender: sender}, wsClientMessage{
				Type:   wsTypeMenuSelection,
				ItemID: tt.itemID,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectRootMenu() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got := sender.count(); got != tt.wantCalls {
				t.Fatalf("envelopes = %d, want %d", got, tt.wantCalls)
			}
		})
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
