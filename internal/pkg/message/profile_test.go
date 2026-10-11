package message

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestUnconfirmedSendDoesNotChangeRoutes(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"cancellation", context.Canceled},
		{"closed connection", net.ErrClosed},
		{"unexpected EOF", io.ErrUnexpectedEOF},
		{"wrapped timeout", fmt.Errorf("submit message: %w", &net.OpError{Op: "write", Net: "tcp", Err: context.DeadlineExceeded})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := &fakeRoute{status: RouteStatus{Connected: true}}
			device := &fakeModemDevice{id: "modem", profile: "sim-a", sendErr: tt.err}
			service := New(testStore(t), route)
			_, err := service.send(t.Context(), device, "10086", "test message")
			if !errors.Is(err, ErrSendUnconfirmed) || !errors.Is(err, tt.err) {
				t.Fatalf("send() error = %v, want unconfirmed result retaining %v", err, tt.err)
			}
			if device.sendCalls != 1 || route.sendSMSCalls != 0 {
				t.Fatalf("sends = modem %d, route %d, want 1 and 0", device.sendCalls, route.sendSMSCalls)
			}
		})
	}
}

func TestRoutedTimeoutPreservesUnconfirmedOutcome(t *testing.T) {
	for _, preferred := range []bool{true, false} {
		t.Run(fmt.Sprintf("preferred=%t", preferred), func(t *testing.T) {
			route := &fakeRoute{status: RouteStatus{Preferred: preferred, Connected: true}, sendErr: context.DeadlineExceeded}
			device := &fakeModemDevice{id: "modem", profile: "sim-a", sendErr: errors.New("modem rejected message")}
			_, err := New(testStore(t), route).send(t.Context(), device, "10086", "test message")
			if !errors.Is(err, ErrSendUnconfirmed) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("send() error = %v, want unconfirmed route timeout", err)
			}
			if route.sendSMSCalls != 1 {
				t.Fatalf("route sends = %d, want 1", route.sendSMSCalls)
			}
		})
	}
}

type reservedRoute struct {
	entered chan struct{}
	release chan struct{}
}

func (r *reservedRoute) Status(context.Context, *mmodem.Modem) (RouteStatus, error) {
	return RouteStatus{Preferred: true, Connected: true}, nil
}

func (r *reservedRoute) SendSMS(ctx context.Context, modem *mmodem.Modem, to, text string) (storage.Message, error) {
	close(r.entered)
	select {
	case <-ctx.Done():
		return storage.Message{}, ctx.Err()
	case <-r.release:
	}
	return storage.Message{ModemID: modem.EquipmentIdentifier, ProfileID: "sim-a", Source: storage.MessageSourceRouted, ExternalKey: "sent", Sender: "own", Recipient: to, Text: text, Timestamp: time.Now(), Status: "sent"}, nil
}

func (r *reservedRoute) ApplyPendingSMSStatus(context.Context, storage.Message) error { return nil }

func TestExpectedSIMIsCheckedInsideReservation(t *testing.T) {
	store := testStore(t)
	device := &mmodem.Modem{EquipmentIdentifier: "modem", SIM: &mmodem.SIM{Identifier: "sim-a"}}
	route := &reservedRoute{entered: make(chan struct{}), release: make(chan struct{})}
	messenger := New(store, route)
	if _, err := messenger.SendWithProfile(t.Context(), device, SendRequest{To: "10086", Text: "hello", ExpectedProfileID: "sim-b"}); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("mismatched profile = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	result := make(chan error, 1)
	wg.Go(func() {
		_, err := messenger.SendWithProfile(ctx, device, SendRequest{To: "10086", Text: "hello", ExpectedProfileID: "sim-a"})
		result <- err
	})
	select {
	case <-route.entered:
	case <-time.After(time.Second):
		t.Fatal("send did not reach its route")
	}
	switchCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	if release, err := device.ReserveSIMSlot(switchCtx); err == nil {
		release()
		t.Fatal("SIM switch entered while an SMS was being submitted")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(route.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	release, err := device.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
