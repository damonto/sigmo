package wwan

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/damonto/wwan-go/qcom"
	usim "github.com/damonto/wwan-go/sim"
	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

var testQMISetupMenu = qcom.CATCommand{
	Ref:  7,
	Data: []byte{0xD0, 0x09, 0x81, 0x03, 0x01, 0x25, 0x00, 0x82, 0x02, 0x81, 0x82},
}

func TestQMICATReaderCommands(t *testing.T) {
	cacheErr := qcom.QMIErrorNoEntry
	tests := []struct {
		name     string
		cached   qcom.CATCommand
		cacheErr error
		live     []qcom.CATCommand
		wantRef  uint32
	}{
		{
			name:    "restores cached setup menu",
			cached:  testQMISetupMenu,
			wantRef: qmiCachedSetupMenuRef,
		},
		{
			name:    "coalesces cached setup menu and force claim replay",
			cached:  testQMISetupMenu,
			live:    []qcom.CATCommand{testQMISetupMenu},
			wantRef: qmiCachedSetupMenuRef,
		},
		{
			name:     "uses live command after cache miss",
			cacheErr: cacheErr,
			live:     []qcom.CATCommand{testQMISetupMenu},
			wantRef:  testQMISetupMenu.Ref,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := &fakeQMICATCommandSource{
				cached:         tt.cached,
				cacheErr:       tt.cacheErr,
				commandBatches: [][]qcom.CATCommand{tt.live},
			}
			reader := newQMICATReader(qmiCATReaderConfig{
				CAT: cat,
			})
			reader.responder = &fakeQMICATResponder{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			out, err := reader.Commands(ctx, stkpkg.NewProfile(stkpkg.CapabilitySetupMenu))
			if err != nil {
				t.Fatalf("Commands() error = %v", err)
			}
			select {
			case <-reader.CATReady():
			default:
				t.Fatal("CATReady() is not closed after command claim")
			}
			select {
			case session := <-out:
				if session.Err != nil {
					t.Fatalf("session error = %v", session.Err)
				}
				if session.Ref != tt.wantRef {
					t.Fatalf("session ref = %d, want %d", session.Ref, tt.wantRef)
				}
				if _, ok := session.Command.(stkpkg.SetupMenuCommand); !ok {
					t.Fatalf("session command = %T, want SetupMenuCommand", session.Command)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for setup menu")
			}
			if got := cat.callsCopy(); !slices.Equal(got, []string{"cache", "claim"}) {
				t.Fatalf("CAT calls = %v, want [cache claim]", got)
			}
		})
	}
}

func TestQMICATReaderCommandErrors(t *testing.T) {
	claimErr := errors.New("claim rejected")
	reader := &qmiCATReader{
		cat:       &fakeQMICATCommandSource{cacheErr: qcom.QMIErrorNoEntry, claimErr: claimErr},
		responder: &fakeQMICATResponder{},
	}
	if _, err := reader.Commands(t.Context(), stkpkg.NewProfile(stkpkg.CapabilitySetupMenu)); !errors.Is(err, claimErr) {
		t.Fatalf("Commands() error = %v, want %v", err, claimErr)
	}
}

func TestQMICATReaderRespond(t *testing.T) {
	tests := []struct {
		name          string
		ref           uint32
		cachedPending bool
		wantCalls     int
	}{
		{
			name:          "cached command suppresses terminal response",
			ref:           qmiCachedSetupMenuRef,
			cachedPending: true,
		},
		{
			name:      "live command sends terminal response",
			ref:       7,
			wantCalls: 1,
		},
		{
			name:      "consumed cached ref sends later response",
			ref:       qmiCachedSetupMenuRef,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responder := &fakeQMICATResponder{}
			reader := &qmiCATReader{
				responder:             responder,
				cachedResponsePending: tt.cachedPending,
			}
			err := reader.Respond(context.Background(), usim.STKSession{Ref: tt.ref}, stkpkg.Result(stkpkg.ResultCommandPerformed))
			if err != nil {
				t.Fatalf("Respond() error = %v", err)
			}
			if got := responder.callCount(); got != tt.wantCalls {
				t.Fatalf("responder calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestQMISTKSessionSelectItemEncoding(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantItems int
		wantFirst string
		wantLast  string
		wantMenu  bool
	}{
		{
			name:      "accepts 9eSIM UTF-8 profile names",
			raw:       "D02B81030124008202818285084553494D4C4953548F0E01F09F87AAF09F87AA2044656D6F8F0602456C697361",
			wantItems: 2,
			wantFirst: "🇪🇪 Demo",
			wantLast:  "Elisa",
			wantMenu:  true,
		},
		{
			name:      "keeps standard alpha identifiers",
			raw:       "D0158103012400820281828505396553494D8F03014F4B",
			wantItems: 1,
			wantFirst: "OK",
			wantLast:  "OK",
			wantMenu:  true,
		},
		{
			name: "rejects invalid non-UTF-8 fallback",
			raw:  "D00E8103012400820281828F03018000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tt.raw)
			if err != nil {
				t.Fatalf("DecodeString() error = %v", err)
			}
			session, err := qmiSTKSession(qcom.CATCommand{Ref: 23, Data: raw}, 23)
			if err != nil {
				t.Fatalf("qmiSTKSession() error = %v", err)
			}
			menu, ok := session.Command.(stkpkg.SelectItemCommand)
			if !tt.wantMenu {
				if ok {
					t.Fatalf("command = %T, want malformed command", session.Command)
				}
				if _, ok := session.Command.(stkpkg.MalformedCommand); !ok {
					t.Fatalf("command = %T, want MalformedCommand", session.Command)
				}
				return
			}
			if !ok {
				t.Fatalf("command = %T, want SelectItemCommand", session.Command)
			}
			if len(menu.Items) != tt.wantItems {
				t.Fatalf("items = %d, want %d", len(menu.Items), tt.wantItems)
			}
			if got := menu.Items[0].Text.String(); got != tt.wantFirst {
				t.Fatalf("first item = %q, want %q", got, tt.wantFirst)
			}
			if got := menu.Items[len(menu.Items)-1].Text.String(); got != tt.wantLast {
				t.Fatalf("last item = %q, want %q", got, tt.wantLast)
			}
		})
	}
}

type fakeQMICATCommandSource struct {
	mu             sync.Mutex
	calls          []string
	claimCalls     int
	cached         qcom.CATCommand
	cacheErr       error
	commandBatches [][]qcom.CATCommand
	claim          qcom.CATEventClaim
	claimErr       error
}

func (c *fakeQMICATCommandSource) CachedProactiveCommand(context.Context, qcom.CATCachedCommandID) (qcom.CATCommand, error) {
	c.mu.Lock()
	c.calls = append(c.calls, "cache")
	c.mu.Unlock()
	return c.cached, c.cacheErr
}

func (c *fakeQMICATCommandSource) ForceClaimCommands(ctx context.Context, _ qcom.CATEventClaimConfig) (<-chan qcom.CATCommand, qcom.CATEventClaim, error) {
	c.mu.Lock()
	c.calls = append(c.calls, "claim")
	claimCall := c.claimCalls
	c.claimCalls++
	var commands []qcom.CATCommand
	if claimCall < len(c.commandBatches) {
		commands = slices.Clone(c.commandBatches[claimCall])
	}
	c.mu.Unlock()
	if c.claimErr != nil {
		return nil, qcom.CATEventClaim{}, c.claimErr
	}

	out := make(chan qcom.CATCommand, 8)
	go func() {
		defer close(out)
		for _, command := range commands {
			select {
			case <-ctx.Done():
				return
			case out <- command:
			}
		}
		<-ctx.Done()
	}()
	return out, c.claim, nil
}

func (c *fakeQMICATCommandSource) callsCopy() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

type fakeQMICATResponder struct {
	mu    sync.Mutex
	calls int
}

func (r *fakeQMICATResponder) Respond(context.Context, usim.STKSession, stkpkg.TerminalResponse) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return nil
}

func (r *fakeQMICATResponder) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
