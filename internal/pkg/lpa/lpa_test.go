package lpa

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestLockedChannelDisconnectOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel *fakeSmartCardChannel
		wantErr error
	}{
		{
			name:    "disconnect succeeds once",
			channel: &fakeSmartCardChannel{},
		},
		{
			name:    "disconnect error is returned once",
			channel: &fakeSmartCardChannel{disconnectErr: errFakeDisconnect},
			wantErr: errFakeDisconnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "test:" + tt.name
			gmu.Lock(key)
			channel := &lockedChannel{SmartCardChannel: tt.channel, key: key}

			err := channel.Disconnect()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Disconnect() error = %v, want %v", err, tt.wantErr)
			}
			if err := channel.Disconnect(); err != nil {
				t.Fatalf("second Disconnect() error = %v", err)
			}
			if tt.channel.disconnects != 1 {
				t.Fatalf("disconnects = %d, want 1", tt.channel.disconnects)
			}
			assertLockReleased(t, key)
		})
	}
}

func TestContextSmartCardChannelStopsNewOperationsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	underlying := &fakeSmartCardChannel{}
	channel := &contextSmartCardChannel{ctx: ctx, SmartCardChannel: underlying}

	if err := channel.Connect(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect() error = %v, want %v", err, context.Canceled)
	}
	if _, err := channel.OpenLogicalChannel(nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenLogicalChannel() error = %v, want %v", err, context.Canceled)
	}
	if _, err := channel.Transmit(nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Transmit() error = %v, want %v", err, context.Canceled)
	}
	if err := channel.CloseLogicalChannel(1); err != nil {
		t.Fatalf("CloseLogicalChannel() error = %v", err)
	}
	if err := channel.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if underlying.disconnects != 1 {
		t.Fatalf("disconnects = %d, want cleanup after cancellation", underlying.disconnects)
	}
}

func TestNewWithChannelStopsWaitingForLockAfterCancellation(t *testing.T) {
	key := "test:new-with-channel-cancellation"
	gmu.Lock(key)
	t.Cleanup(func() { gmu.Unlock(key) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	channel := &fakeSmartCardChannel{}
	_, err := NewWithChannel(ctx, ChannelConfig{
		LockKey: key,
		Channel: channel,
		Logger:  slog.Default(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NewWithChannel() error = %v, want %v", err, context.Canceled)
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1 after canceled acquisition", channel.disconnects)
	}
}

func TestSIMSlotChannelDisconnectOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel *fakeSmartCardChannel
		wantErr error
	}{
		{
			name:    "disconnect succeeds once",
			channel: &fakeSmartCardChannel{},
		},
		{
			name:    "disconnect error is retained",
			channel: &fakeSmartCardChannel{disconnectErr: errFakeDisconnect},
			wantErr: errFakeDisconnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases := 0
			channel := &simSlotChannel{
				SmartCardChannel: tt.channel,
				release:          func() { releases++ },
			}

			for range 2 {
				if err := channel.Disconnect(); !errors.Is(err, tt.wantErr) {
					t.Fatalf("Disconnect() error = %v, want %v", err, tt.wantErr)
				}
			}
			if tt.channel.disconnects != 1 {
				t.Fatalf("disconnects = %d, want 1", tt.channel.disconnects)
			}
			if releases != 1 {
				t.Fatalf("slot releases = %d, want 1", releases)
			}
		})
	}
}

func TestSIMSlotChannelCloseLogicalChannelReleasesOnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		disconnectErr error
	}{
		{name: "close error"},
		{name: "close and disconnect errors", disconnectErr: errFakeDisconnect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases := 0
			underlying := &fakeSmartCardChannel{
				disconnectErr:          tt.disconnectErr,
				closeLogicalChannelErr: errFakeCloseLogicalChannel,
			}
			channel := &simSlotChannel{
				SmartCardChannel: underlying,
				release:          func() { releases++ },
			}

			err := channel.CloseLogicalChannel(1)
			if !errors.Is(err, errFakeCloseLogicalChannel) {
				t.Fatalf("CloseLogicalChannel() error = %v, want %v", err, errFakeCloseLogicalChannel)
			}
			if tt.disconnectErr != nil && !errors.Is(err, tt.disconnectErr) {
				t.Fatalf("CloseLogicalChannel() error = %v, want it to contain %v", err, tt.disconnectErr)
			}
			if underlying.disconnects != 1 {
				t.Fatalf("disconnects = %d, want 1", underlying.disconnects)
			}
			if releases != 1 {
				t.Fatalf("slot releases = %d, want 1", releases)
			}
		})
	}
}

func TestLockedChannelCloseLogicalChannelReleasesOnError(t *testing.T) {
	t.Parallel()

	key := "test:close-logical-channel-error"
	gmu.Lock(key)
	channel := &fakeSmartCardChannel{closeLogicalChannelErr: errFakeCloseLogicalChannel}
	locked := &lockedChannel{SmartCardChannel: channel, key: key}

	err := locked.CloseLogicalChannel(1)
	if !errors.Is(err, errFakeCloseLogicalChannel) {
		t.Fatalf("CloseLogicalChannel() error = %v, want %v", err, errFakeCloseLogicalChannel)
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1", channel.disconnects)
	}
	if err := locked.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnects after second release = %d, want 1", channel.disconnects)
	}
	assertLockReleased(t, key)
}

func TestNewWithChannelLogger(t *testing.T) {
	tests := []struct {
		name    string
		channel *fakeSmartCardChannel
		run     func(t *testing.T, client *LPA)
		want    string
		wantErr error
	}{
		{
			name:    "LPA creation logs IMEI",
			channel: &fakeSmartCardChannel{openLogicalChannelErr: errFakeOpenLogicalChannel},
			want:    "msg=\"failed to create LPA client\"",
			wantErr: ErrNoSupportedAID,
		},
		{
			name:    "euicc APDU logs IMEI",
			channel: &fakeSmartCardChannel{logicalChannel: 1},
			run: func(t *testing.T, client *LPA) {
				t.Helper()
				if _, err := client.APDU.TransmitRaw([]byte{0x01}); err != nil {
					t.Fatalf("TransmitRaw() error = %v", err)
				}
			},
			want: "msg=\"[APDU] sending\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(previous)

			client, err := NewWithChannel(context.Background(), ChannelConfig{
				LockKey: "test:" + tt.name,
				Channel: tt.channel,
				Logger:  mmodem.LoggerForIMEI("860588043408833"),
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewWithChannel() error = %v, want %v", err, tt.wantErr)
			}
			if err == nil {
				defer func() {
					if cerr := client.Close(); cerr != nil {
						t.Fatalf("Close() error = %v", cerr)
					}
				}()
				tt.run(t, client)
			}

			got := logs.String()
			for _, want := range []string{tt.want, "imei=860588043408833"} {
				if !strings.Contains(got, want) {
					t.Fatalf("logs = %s, want it to contain %q", got, want)
				}
			}
		})
	}
}

func assertLockReleased(t *testing.T, key string) {
	t.Helper()

	acquired := make(chan struct{})
	go func() {
		gmu.Lock(key)
		defer gmu.Unlock(key)
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lock was not released")
	}
}

var errFakeDisconnect = errors.New("disconnect")
var errFakeCloseLogicalChannel = errors.New("close logical channel")
var errFakeOpenLogicalChannel = errors.New("open logical channel")

type fakeSmartCardChannel struct {
	disconnectErr          error
	closeLogicalChannelErr error
	openLogicalChannelErr  error
	transmitResponse       []byte
	logicalChannel         byte
	disconnects            int
}

func (f *fakeSmartCardChannel) Connect() error {
	return nil
}

func (f *fakeSmartCardChannel) Disconnect() error {
	f.disconnects++
	return f.disconnectErr
}

func (f *fakeSmartCardChannel) OpenLogicalChannel([]byte) (byte, error) {
	if f.openLogicalChannelErr != nil {
		return 0, f.openLogicalChannelErr
	}
	if f.logicalChannel != 0 {
		return f.logicalChannel, nil
	}
	return 1, nil
}

func (f *fakeSmartCardChannel) Transmit([]byte) ([]byte, error) {
	if f.transmitResponse != nil {
		return f.transmitResponse, nil
	}
	return []byte{0x90, 0x00}, nil
}

func (f *fakeSmartCardChannel) CloseLogicalChannel(byte) error {
	return f.closeLogicalChannelErr
}
