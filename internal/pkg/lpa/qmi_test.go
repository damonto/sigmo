package lpa

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeQMILPAReader struct {
	activated       int
	openedAID       []byte
	openedChannel   uint8
	transmitChannel uint8
	command         []byte
	closedChannel   uint8
	closeChannelErr error
	transmit        func(context.Context) error
	closes          int
}

func (f *fakeQMILPAReader) ActivateSlot(context.Context) error {
	f.activated++
	return nil
}

func (f *fakeQMILPAReader) OpenLogicalChannel(_ context.Context, aid []byte) (uint8, error) {
	f.openedAID = slices.Clone(aid)
	return f.openedChannel, nil
}

func (f *fakeQMILPAReader) SendAPDU(ctx context.Context, channel uint8, command []byte) ([]byte, error) {
	f.transmitChannel = channel
	f.command = slices.Clone(command)
	if f.transmit != nil {
		if err := f.transmit(ctx); err != nil {
			return nil, err
		}
	}
	return []byte{0x90, 0x00}, nil
}

func TestQMILPAChannelUsesLeaseContext(t *testing.T) {
	reader := &fakeQMILPAReader{transmit: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	channel, err := newQMILPAChannel(reader)
	if err != nil {
		t.Fatalf("newQMILPAChannel() error = %v", err)
	}
	operation := newOperationContext(context.Background())
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

func (f *fakeQMILPAReader) CloseLogicalChannel(_ context.Context, channel uint8) error {
	f.closedChannel = channel
	return f.closeChannelErr
}

func (f *fakeQMILPAReader) Close() error {
	f.closes++
	return nil
}

func TestQMILPAChannelClosesReaderAfterLogicalChannelCloseError(t *testing.T) {
	closeErr := errors.New("close logical channel")
	reader := &fakeQMILPAReader{closeChannelErr: closeErr}
	channel, err := newQMILPAChannel(reader)
	if err != nil {
		t.Fatalf("newQMILPAChannel() error = %v", err)
	}
	if err := channel.CloseLogicalChannel(3); !errors.Is(err, closeErr) {
		t.Fatalf("CloseLogicalChannel() error = %v, want %v", err, closeErr)
	}
	if reader.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", reader.closes)
	}
	if err := channel.Disconnect(); err != nil {
		t.Fatalf("Disconnect() after close error = %v", err)
	}
	if reader.closes != 1 {
		t.Fatalf("Close() calls after Disconnect() = %d, want 1", reader.closes)
	}
}

func TestQMILPAChannelDoesNotActivateSlot(t *testing.T) {
	reader := &fakeQMILPAReader{openedChannel: 3}
	channel, err := newQMILPAChannel(reader)
	if err != nil {
		t.Fatalf("newQMILPAChannel() error = %v", err)
	}
	if err := channel.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if reader.activated != 0 {
		t.Fatalf("ActivateSlot() calls = %d, want 0", reader.activated)
	}

	aid := []byte{0xA0, 0x00, 0x00, 0x05, 0x59}
	logicalChannel, err := channel.OpenLogicalChannel(aid)
	if err != nil {
		t.Fatalf("OpenLogicalChannel() error = %v", err)
	}
	if logicalChannel != 3 {
		t.Fatalf("OpenLogicalChannel() = %d, want 3", logicalChannel)
	}
	if !slices.Equal(reader.openedAID, aid) {
		t.Fatalf("opened AID = % X, want % X", reader.openedAID, aid)
	}

	command := []byte{0x82, 0xE2, 0x91, 0x00}
	response, err := channel.Transmit(command)
	if err != nil {
		t.Fatalf("Transmit() error = %v", err)
	}
	if reader.transmitChannel != logicalChannel || !slices.Equal(reader.command, command) {
		t.Fatalf("Transmit() used channel %d command % X, want channel %d command % X", reader.transmitChannel, reader.command, logicalChannel, command)
	}
	if !slices.Equal(response, []byte{0x90, 0x00}) {
		t.Fatalf("Transmit() = % X, want 90 00", response)
	}

	if err := channel.CloseLogicalChannel(logicalChannel); err != nil {
		t.Fatalf("CloseLogicalChannel() error = %v", err)
	}
	if reader.closedChannel != logicalChannel {
		t.Fatalf("closed channel = %d, want %d", reader.closedChannel, logicalChannel)
	}
	if err := channel.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if err := channel.Disconnect(); err != nil {
		t.Fatalf("second Disconnect() error = %v", err)
	}
	if reader.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", reader.closes)
	}
	if _, err := channel.Transmit(command); !errors.Is(err, errQMILPAChannelClosed) {
		t.Fatalf("Transmit() after close error = %v, want %v", err, errQMILPAChannelClosed)
	}
}
