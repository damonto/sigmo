package modem

import (
	"context"
	"errors"
	"testing"
	"time"

	devicewwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

func TestConsumeSIMRefreshStreamTracksResetLifecycle(t *testing.T) {
	reload := make(chan error, 1)
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		generation:          1,
		onFailure: func(err error) {
			reload <- err
		},
	}
	stream := make(chan devicewwan.SIMRefreshEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- modem.consumeSIMRefreshStream(ctx, stream) }()

	_, _, startSignal := modem.currentSIMRefresh()
	stream <- devicewwan.SIMRefreshEvent{Stage: devicewwan.SIMRefreshStart, Mode: devicewwan.SIMRefreshReset}
	waitForRefreshSignal(t, startSignal)
	startVersion, inProgress, endSignal := modem.currentSIMRefresh()
	if startVersion == 0 || !inProgress {
		t.Fatalf("refresh after START = version %d, in progress %t", startVersion, inProgress)
	}

	stream <- devicewwan.SIMRefreshEvent{Stage: devicewwan.SIMRefreshEndWithFailure, Mode: devicewwan.SIMRefreshReset}
	waitForRefreshSignal(t, endSignal)
	endVersion, inProgress, _ := modem.currentSIMRefresh()
	if endVersion <= startVersion || inProgress {
		t.Fatalf("refresh after END = version %d, in progress %t; start version %d", endVersion, inProgress, startVersion)
	}
	select {
	case err := <-reload:
		if !errors.Is(err, errSIMRefreshReload) {
			t.Fatalf("reload error = %v, want %v", err, errSIMRefreshReload)
		}
	case <-time.After(time.Second):
		t.Fatal("reset refresh did not request a modem reload")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consumeSIMRefreshStream() error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("consumeSIMRefreshStream() did not stop")
	}
}

func waitForRefreshSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SIM refresh signal")
	}
}
