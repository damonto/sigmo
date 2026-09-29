package internet

import (
	"context"
	"errors"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/wwan-go/qcom"
)

type blockedInternetModem struct {
	fakeInternetModem
	started chan struct{}
}

func (m blockedInternetModem) connectBearer(ctx context.Context, _ mmodem.BearerProperties) (*mmodem.Bearer, error) {
	close(m.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestConnectDoesNotHoldGlobalRoutesWhileWaitingForModem(t *testing.T) {
	c, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	modem := blockedInternetModem{fakeInternetModem: fakeInternetModem{modemID: "slow"}, started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		defer c.lockModem("slow")()
		_, err := c.connect(ctx, modem, Preferences{APN: "internet"}, false)
		done <- err
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-modem.started:
	case <-time.After(time.Second):
		t.Fatal("connection did not start")
	}
	if !c.routeMu.TryLock() {
		t.Fatal("PDN setup holds global route lock")
	}
	c.routeMu.Unlock()
	readCtx, stopRead := context.WithTimeout(t.Context(), time.Second)
	defer stopRead()
	if _, err := c.current(readCtx, fakeInternetModem{modemID: "other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Current(readCtx, &mmodem.Modem{EquipmentIdentifier: "slow"}); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("busy Current() error = %v", err)
	}
}

func TestRejectedConnectionKeepsSuccessfulPreference(t *testing.T) {
	c, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := Preferences{APN: "good", IPType: "ipv4v6"}
	c.preferences["modem"] = want
	calls := 0
	rejected := &qcom.WDSStartNetworkError{Err: qcom.QMIErrorCallFailed}
	modem := fakeInternetModem{modemID: "modem", connectCalls: &calls, connectErr: rejected}
	_, err = c.connect(t.Context(), modem, Preferences{APN: "bad", APNUsername: "user", APNPassword: "secret"}, false)
	if !errors.Is(err, rejected) {
		t.Fatalf("connect() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("connection attempted %d times after network rejection", calls)
	}
	if got := c.preference("modem"); got != want {
		t.Fatalf("preference = %+v, want %+v", got, want)
	}
}

func TestConnectionPreferenceDoesNotCrossSIMProfiles(t *testing.T) {
	for _, profile := range []string{"new-card", ""} {
		t.Run(profile, func(t *testing.T) {
			c, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatal(err)
			}
			c.setConnectionAndPreference("modem", trackedConnection{profileID: "old-card"}, Preferences{APN: "old-apn", APNPassword: "old-password"})
			got := c.preferenceWithAlwaysOn(t.Context(), fakeInternetModem{modemID: "modem", iccidValue: profile})
			if got.APN != "" || got.APNPassword != "" {
				t.Fatalf("new profile inherits previous APN: %+v", got)
			}
		})
	}
}

func TestCanceledOperationDoesNotWaitForModem(t *testing.T) {
	c, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	unlock := c.lockModem("busy")
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Disconnect(ctx, &mmodem.Modem{EquipmentIdentifier: "busy"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Disconnect() error = %v, want context.Canceled", err)
	}
}
