package lpa

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/damonto/euicc-go/driver"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestCreateChannelForSlotRejectsATPrimaryPort(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/ttyUSB0",
		Ports: []mmodem.ModemPort{
			{PortType: wwanmodem.PortAT, Device: "/dev/ttyUSB0"},
		},
	}

	_, err := createChannelForSlot(t.Context(), modem, 1)
	if !errors.Is(err, wwanmodem.ErrNotSupported) {
		t.Fatalf("createChannelForSlot() error = %v, want %v", err, wwanmodem.ErrNotSupported)
	}
}

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

func TestClientCloseReleasesSIMSlotOnce(t *testing.T) {
	m := new(mmodem.Modem)
	releaseSIMSlot, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatalf("ReserveSIMSlot() error = %v", err)
	}
	client := &Client{releaseSIMSlot: releaseSIMSlot}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	release, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatalf("ReserveSIMSlot() after Close error = %v", err)
	}
	release()
}

func TestClientDiscardSkipsInvalidatedLogicalChannel(t *testing.T) {
	channel := &fakeSmartCardChannel{logicalChannel: 3}
	client, err := NewWithChannelFactory(t.Context(), ChannelConfig{AID: AIDs[0]}, func(context.Context) (driver.SmartCardChannel, error) {
		return channel, nil
	})
	if err != nil {
		t.Fatalf("NewWithChannelFactory() error = %v", err)
	}
	if err := client.discard(); err != nil {
		t.Fatalf("discard() error = %v", err)
	}
	if channel.closeLogicalChannels != 0 {
		t.Fatalf("logical channel close calls = %d, want 0", channel.closeLogicalChannels)
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnect calls = %d, want 1", channel.disconnects)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() after discard error = %v", err)
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnect calls after Close = %d, want 1", channel.disconnects)
	}
}

func TestContextSmartCardChannelStopsNewOperationsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	underlying := &fakeSmartCardChannel{}
	channel := &contextSmartCardChannel{operation: newOperationContext(ctx), SmartCardChannel: underlying}

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

func TestNoSupportedAIDCacheability(t *testing.T) {
	tests := []struct {
		name      string
		openErr   error
		cacheable bool
	}{
		{name: "transport error is retryable", openErr: errors.New("transport unavailable")},
		{name: "protocol rejection is cacheable", openErr: errAIDNotSupported, cacheable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var channels []*fakeSmartCardChannel
			_, err := NewWithChannelFactory(t.Context(), ChannelConfig{
				Logger: slog.Default(),
			}, func(context.Context) (driver.SmartCardChannel, error) {
				channel := &fakeSmartCardChannel{openLogicalChannelErr: tt.openErr}
				channels = append(channels, channel)
				return channel, nil
			})
			if !errors.Is(err, ErrNoSupportedAID) {
				t.Fatalf("NewWithChannelFactory() error = %v, want %v", err, ErrNoSupportedAID)
			}
			if got := errors.Is(err, errCacheableNoSupportedAID); got != tt.cacheable {
				t.Fatalf("cacheable error = %v, want %v", got, tt.cacheable)
			}
			if len(channels) != len(AIDs) {
				t.Fatalf("channels created = %d, want %d", len(channels), len(AIDs))
			}
			for i, channel := range channels {
				if channel.disconnects != 1 {
					t.Fatalf("channel %d disconnects = %d, want 1", i, channel.disconnects)
				}
			}
		})
	}
}

func TestNewWithChannelFactoryUsesFreshChannelForAIDFallback(t *testing.T) {
	channels := []*fakeSmartCardChannel{
		{openLogicalChannelErr: errAIDNotSupported},
		{logicalChannel: 2},
	}
	next := 0
	client, err := NewWithChannelFactory(t.Context(), ChannelConfig{}, func(context.Context) (driver.SmartCardChannel, error) {
		if next >= len(channels) {
			t.Fatal("channel factory called after successful AID attempt")
		}
		channel := channels[next]
		next++
		return channel, nil
	})
	if err != nil {
		t.Fatalf("NewWithChannelFactory() error = %v", err)
	}
	if channels[0].disconnects != 1 {
		t.Fatalf("failed channel disconnects = %d, want 1", channels[0].disconnects)
	}
	if channels[1].disconnects != 0 {
		t.Fatalf("active channel disconnects = %d, want 0", channels[1].disconnects)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if channels[1].disconnects != 1 {
		t.Fatalf("closed active channel disconnects = %d, want 1", channels[1].disconnects)
	}
}

func TestNewWithChannelFactoryDisconnectsWhenOptionsAreInvalid(t *testing.T) {
	channel := &fakeSmartCardChannel{}
	_, err := NewWithChannelFactory(t.Context(), ChannelConfig{
		AID:      AIDs[0],
		ConfigID: "invalid-mss",
		Settings: &settings.Settings{Modems: map[string]settings.Modem{"invalid-mss": {MSS: 255}}},
	}, func(context.Context) (driver.SmartCardChannel, error) {
		return channel, nil
	})
	if err == nil {
		t.Fatal("NewWithChannelFactory() error = nil, want invalid MSS error")
	}
	if channel.disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1", channel.disconnects)
	}
}

func TestNewWithChannelFactoryReleasesLockAfterFactoryFailure(t *testing.T) {
	key := "test:channel-factory-failure"
	factoryErr := errors.New("channel unavailable")
	first := &fakeSmartCardChannel{openLogicalChannelErr: errAIDNotSupported}
	calls := 0
	_, err := NewWithChannelFactory(t.Context(), ChannelConfig{LockKey: key}, func(context.Context) (driver.SmartCardChannel, error) {
		calls++
		if calls == 1 {
			return first, nil
		}
		return nil, factoryErr
	})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("NewWithChannelFactory() error = %v, want %v", err, factoryErr)
	}
	if first.disconnects != 1 {
		t.Fatalf("first channel disconnects = %d, want 1", first.disconnects)
	}
	assertLockReleased(t, key)
}

func TestContextRoundTripperCancelsInFlightRequest(t *testing.T) {
	operation := newOperationContext(context.Background())
	transport := &contextRoundTripper{
		operation: operation,
		next: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	operation.use(ctx)
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(req)
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, context.Canceled)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewWithChannelFactoryStopsWaitingForLockAfterCancellation(t *testing.T) {
	key := "test:new-with-channel-factory-cancellation"
	gmu.Lock(key)
	t.Cleanup(func() { gmu.Unlock(key) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	_, err := NewWithChannelFactory(ctx, ChannelConfig{
		LockKey: key,
		Logger:  slog.Default(),
	}, func(context.Context) (driver.SmartCardChannel, error) {
		called = true
		return &fakeSmartCardChannel{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NewWithChannelFactory() error = %v, want %v", err, context.Canceled)
	}
	if called {
		t.Fatal("channel factory called before lock acquisition")
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

func TestNewWithChannelFactoryLogger(t *testing.T) {
	tests := []struct {
		name    string
		channel *fakeSmartCardChannel
		run     func(t *testing.T, client *Client)
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
			run: func(t *testing.T, client *Client) {
				t.Helper()
				if _, err := client.rawClient().APDU.TransmitRaw([]byte{0x01}); err != nil {
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

			client, err := NewWithChannelFactory(context.Background(), ChannelConfig{
				LockKey: "test:" + tt.name,
				AID:     AIDs[0],
				Logger:  mmodem.LoggerForIMEI("860588043408833"),
			}, func(context.Context) (driver.SmartCardChannel, error) {
				return tt.channel, nil
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewWithChannelFactory() error = %v, want %v", err, tt.wantErr)
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
	closeLogicalChannels   int
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
	f.closeLogicalChannels++
	return f.closeLogicalChannelErr
}
