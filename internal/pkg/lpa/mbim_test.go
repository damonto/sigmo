package lpa

import (
	"context"
	"errors"
	"testing"
)

type fakeMBIMLPAReader struct {
	transmit func(context.Context) error
}

func (f *fakeMBIMLPAReader) OpenChannel(context.Context, []byte) (uint32, error) {
	return 1, nil
}

func (f *fakeMBIMLPAReader) TransmitAPDU(ctx context.Context, _ uint32, _ []byte) ([]byte, uint32, error) {
	if f.transmit != nil {
		if err := f.transmit(ctx); err != nil {
			return nil, 0, err
		}
	}
	return nil, 0x0090, nil
}

func (f *fakeMBIMLPAReader) CloseChannel(context.Context, uint32) error { return nil }
func (f *fakeMBIMLPAReader) Close() error                               { return nil }

func TestMBIMLPAChannelUsesLeaseContext(t *testing.T) {
	reader := &fakeMBIMLPAReader{transmit: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	channel, err := newMBIMLPAChannel(reader)
	if err != nil {
		t.Fatalf("newMBIMLPAChannel() error = %v", err)
	}
	operation := newOperationContext(t.Context())
	wrapped := &contextSmartCardChannel{operation: operation, SmartCardChannel: channel}
	ctx, cancel := context.WithCancel(t.Context())
	operation.use(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := wrapped.Transmit([]byte{0x00})
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Transmit() error = %v, want %v", err, context.Canceled)
	}
}
