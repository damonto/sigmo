package modem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestMessagingListKeepsDecodableMessages(t *testing.T) {
	decodeErr := &wwanmodem.MessageDecodeError{
		Ref: wwanmodem.MessageRef{Storage: wwanmodem.MessageStorageDevice, ID: 2},
		Err: errors.New("truncated PDU"),
	}
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "complete list"},
		{name: "partial list", err: fmt.Errorf("read inbox: %w", &wwanmodem.MessageListError{Errors: []*wwanmodem.MessageDecodeError{decodeErr}})},
		{name: "transport failure", err: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeMessageSource{
				stored:  []wwanmodem.Message{{Text: "first"}, {Text: "third"}},
				listErr: tt.err,
			}
			messages, err := (&Modem{EquipmentIdentifier: "test-modem"}).Messaging().list(t.Context(), source)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("list() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(messages) != 0 {
					t.Error("list() returned messages after a transport failure")
				}
				return
			}
			if len(messages) != 2 || messages[0].Text != "first" || messages[1].Text != "third" {
				t.Fatalf("list() messages = %+v, want first and third", messages)
			}
		})
	}
}

func TestMessagingSubscribeIsolatesMalformedMessages(t *testing.T) {
	decodeErr := &wwanmodem.MessageDecodeError{Err: errors.New("truncated PDU")}
	listErr := &wwanmodem.MessageListError{Errors: []*wwanmodem.MessageDecodeError{decodeErr}}
	stop := errors.New("subscriber stopped")
	tests := []struct {
		name    string
		stored  []wwanmodem.Message
		listErr error
		events  []wwanmodem.Result[wwanmodem.Message]
		want    []string
		wantErr error
	}{
		{
			name:    "stored and live decode failures preserve one subscription",
			stored:  []wwanmodem.Message{{Text: "stored first"}, {Text: "stored third"}},
			listErr: listErr,
			events: []wwanmodem.Result[wwanmodem.Message]{
				{Err: fmt.Errorf("receive SMS: %w", decodeErr)},
				{Value: wwanmodem.Message{Text: "live message"}},
			},
			want: []string{"stored first", "stored third", "live message"}, wantErr: stop,
		},
		{
			name:    "all stored messages malformed",
			listErr: listErr,
			events:  []wwanmodem.Result[wwanmodem.Message]{{Value: wwanmodem.Message{Text: "live message"}}},
			want:    []string{"live message"}, wantErr: stop,
		},
		{name: "list transport error remains fatal", listErr: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
		{
			name:    "stream transport error remains fatal",
			events:  []wwanmodem.Result[wwanmodem.Message]{{Err: io.ErrUnexpectedEOF}},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name: "subscriber error remains fatal",
			events: []wwanmodem.Result[wwanmodem.Message]{
				{Value: wwanmodem.Message{Text: "first"}},
				{Value: wwanmodem.Message{Text: "second"}},
			},
			want: []string{"first"}, wantErr: stop,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeMessageSource{stored: tt.stored, listErr: tt.listErr, events: tt.events}
			t.Cleanup(source.wg.Wait)
			var got []string
			messaging := (&Modem{EquipmentIdentifier: "test-modem"}).Messaging()
			err := messaging.subscribe(t.Context(), source, func(message *SMS) error {
				select {
				case <-source.watchCanceled:
					t.Error("subscription was canceled before delivering the message")
				default:
				}
				got = append(got, message.Text)
				if len(got) >= len(tt.want) {
					return stop
				}
				return nil
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("subscribe() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("delivered messages = %v, want %v", got, tt.want)
			}
			if source.watchCalls != 1 || !source.watchedBeforeList {
				t.Errorf("watch calls = %d, watched before list = %t", source.watchCalls, source.watchedBeforeList)
			}
			select {
			case <-source.watchCanceled:
			default:
				t.Error("subscribe() did not cancel its watcher on return")
			}
		})
	}
}

type fakeMessageSource struct {
	stored            []wwanmodem.Message
	listErr           error
	events            []wwanmodem.Result[wwanmodem.Message]
	watchCalls        int
	watchedBeforeList bool
	watchCanceled     <-chan struct{}
	wg                sync.WaitGroup
}

func (s *fakeMessageSource) ListMessages(ctx context.Context) ([]wwanmodem.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.watchedBeforeList = s.watchCalls != 0
	if s.listErr != nil {
		if _, ok := errors.AsType[*wwanmodem.MessageListError](s.listErr); !ok {
			return nil, s.listErr
		}
	}
	return slices.Clone(s.stored), s.listErr
}

func (s *fakeMessageSource) WatchMessages(ctx context.Context) (<-chan wwanmodem.Result[wwanmodem.Message], error) {
	s.watchCalls++
	s.watchCanceled = ctx.Done()
	out := make(chan wwanmodem.Result[wwanmodem.Message])
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(out)
		for _, event := range s.events {
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
			if event.Err != nil {
				if _, ok := errors.AsType[*wwanmodem.MessageDecodeError](event.Err); !ok {
					return
				}
			}
		}
		<-ctx.Done()
	}()
	return out, nil
}
