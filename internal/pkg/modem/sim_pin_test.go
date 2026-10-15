package modem

import (
	"context"
	"errors"
	"strings"
	"testing"

	wwan "github.com/damonto/wwan-go/modem"
)

type pinDeviceFake struct {
	info                        wwan.SIMInfo
	status                      wwan.SIMPINStatus
	reads, writes, powers       int
	pin, replacement            string
	writeErr, readErr, powerErr error
	onWrite                     func()
}

func (d *pinDeviceFake) SIMInfo(context.Context) (wwan.SIMInfo, error) {
	d.reads++
	return d.info, d.readErr
}
func (d *pinDeviceFake) SIMPINStatus(context.Context) (wwan.SIMPINStatus, error) {
	return d.status, d.readErr
}
func (d *pinDeviceFake) mutate(pin, newPIN string) error {
	d.writes++
	d.pin, d.replacement = pin, newPIN
	if d.onWrite != nil {
		d.onWrite()
	}
	return d.writeErr
}
func (d *pinDeviceFake) SendPIN(_ context.Context, pin string) error      { return d.mutate(pin, "") }
func (d *pinDeviceFake) SendPUK(_ context.Context, puk, pin string) error { return d.mutate(puk, pin) }
func (d *pinDeviceFake) EnablePIN(_ context.Context, pin string, enabled bool) error {
	err := d.mutate(pin, "")
	if err == nil {
		d.status.Enabled = enabled
	}
	return err
}
func (d *pinDeviceFake) ChangePIN(_ context.Context, oldPIN, newPIN string) error {
	return d.mutate(oldPIN, newPIN)
}
func (d *pinDeviceFake) SetPowerState(context.Context, wwan.PowerState) error {
	d.powers++
	return d.powerErr
}
func pinFixture(t *testing.T, lock wwan.SIMLock) (*Modem, *pinDeviceFake, SIMPINStatus) {
	t.Helper()
	d := &pinDeviceFake{info: wwan.SIMInfo{Slot: 1, ICCID: "SIM-A", State: wwan.SIMStateReady}, status: wwan.SIMPINStatus{Supported: true, Enabled: true, EnabledKnown: true, Lock: lock, PINRetries: 3, PINRetriesKnown: true, PUKRetries: 10, PUKRetriesKnown: true}}
	m := &Modem{simPINDevice: d}
	s, err := m.SIMPINStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return m, d, s
}

func TestSIMPINValidationNeverTouchesDevice(t *testing.T) {
	for _, pin := range []string{"", "123", "123456789", " 1234", "１２３４", "12a4", "12\n4"} {
		t.Run(pin, func(t *testing.T) {
			m, d, s := pinFixture(t, wwan.SIMLockNone)
			reads := d.reads
			if err := m.SetSIMPINProtection(t.Context(), SIMPINProtection{PIN: pin, SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMPINInvalid) {
				t.Fatal(err)
			}
			if d.reads != reads || d.writes != 0 {
				t.Fatal("invalid PIN reached device")
			}
		})
	}
	m, d, s := pinFixture(t, wwan.SIMLockPUK)
	reads := d.reads
	for _, req := range []SIMUnlock{{PUK: "1234567", NewPIN: "1234", SIMToken: s.SIMToken}, {PUK: "１２３４５６７８", NewPIN: "1234", SIMToken: s.SIMToken}, {PIN: "1234", PUK: "12345678", NewPIN: "1234", SIMToken: s.SIMToken}, {PUK: "12345678", NewPIN: "1234"}} {
		if err := m.UnlockSIM(t.Context(), req); !errors.Is(err, ErrSIMPINInvalid) {
			t.Fatal(err)
		}
	}
	if d.reads != reads || d.writes != 0 {
		t.Fatal("invalid PUK reached device")
	}
}

func TestSIMPINProtectionNoOpAndLeadingZeros(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	if err := m.SetSIMPINProtection(t.Context(), SIMPINProtection{PIN: "0001", Enabled: true, SIMToken: s.SIMToken}); err != nil {
		t.Fatal(err)
	}
	if d.writes != 0 {
		t.Fatal("already enabled verified PIN")
	}
	for range 2 {
		if err := m.SetSIMPINProtection(t.Context(), SIMPINProtection{PIN: "0001", Enabled: false, SIMToken: s.SIMToken}); err != nil {
			t.Fatal(err)
		}
	}
	if d.writes != 1 || d.pin != "0001" || d.powers != 0 {
		t.Fatalf("writes=%d pin=%s powers=%d", d.writes, d.pin, d.powers)
	}
}

func TestSIMPINStaleTokenAndUnknownState(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	d.info.ICCID = "SIM-B"
	if err := m.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "1234", NewPIN: "0000", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMChanged) {
		t.Fatal(err)
	}
	if d.writes != 0 {
		t.Fatal("wrote to a different SIM")
	}
	d.status.EnabledKnown = false
	d.status.PINRetriesKnown = false
	d.status.PUKRetriesKnown = false
	s, err := m.SIMPINStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled != nil || s.PINRetries != nil || s.PUKRetries != nil {
		t.Fatal("unknown values became zero")
	}
	if err := m.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "1234", NewPIN: "0000", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMPINState) {
		t.Fatal(err)
	}
}

func TestSIMPINFailedAttemptsAndNoReplay(t *testing.T) {
	tests := []struct {
		name        string
		lock, after wwan.SIMLock
		want        error
		puk         bool
	}{
		{"wrong PIN", wwan.SIMLockPIN, wwan.SIMLockPIN, ErrSIMIncorrectPIN, false},
		{"PIN to PUK", wwan.SIMLockPIN, wwan.SIMLockPUK, ErrSIMPUKRequired, false},
		{"wrong PUK", wwan.SIMLockPUK, wwan.SIMLockPUK, ErrSIMIncorrectPUK, true},
		{"PUK blocked", wwan.SIMLockPUK, wwan.SIMLockBlocked, ErrSIMPermanentlyBlocked, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, d, s := pinFixture(t, tt.lock)
			d.writeErr = errors.New("device rejected secret 12345678")
			d.onWrite = func() { d.status.Lock = tt.after; d.status.PINRetries--; d.status.PUKRetries-- }
			req := SIMUnlock{PIN: "0001", SIMToken: s.SIMToken}
			if tt.puk {
				req = SIMUnlock{PUK: "00123456", NewPIN: "0000", SIMToken: s.SIMToken}
			}
			err := m.UnlockSIM(t.Context(), req)
			if !errors.Is(err, tt.want) || strings.Contains(err.Error(), "12345678") {
				t.Fatal(err)
			}
			if d.writes != 1 || d.powers != 0 {
				t.Fatalf("writes=%d powers=%d", d.writes, d.powers)
			}
			current, err := m.SIMPINStatus(t.Context())
			if err != nil || current.Lock != tt.after {
				t.Fatalf("status=%+v err=%v", current, err)
			}
		})
	}
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	d.writeErr = context.DeadlineExceeded
	if err := m.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "1234", NewPIN: "4321", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMPINResultUnknown) {
		t.Fatal(err)
	}
	if d.writes != 1 {
		t.Fatal("timeout replayed credential")
	}
}

func TestSIMPINAcknowledgementAndPartialSuccess(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	d.onWrite = func() { d.readErr = errors.New("refresh unavailable") }
	if err := m.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "0001", NewPIN: "0002", SIMToken: s.SIMToken}); err != nil {
		t.Fatal(err)
	}
	if d.pin != "0001" || d.replacement != "0002" || d.writes != 1 || d.powers != 0 {
		t.Fatal("wrong change operation")
	}
	m, d, _ = pinFixture(t, wwan.SIMLockPIN)
	d.info.ICCID = "" // Locked cards need not expose an ICCID.
	d.powerErr = errors.New("radio unavailable")
	if err := m.UnlockSIM(t.Context(), SIMUnlock{PIN: "0001"}); !errors.Is(err, ErrEnableAfterSIMUnlock) {
		t.Fatal(err)
	}
	if d.writes != 1 || d.powers != 1 {
		t.Fatal("unlock was not acknowledged separately")
	}
}

func TestSIMPINReservationCancellation(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	release, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.ChangeSIMPIN(ctx, SIMPINChange{OldPIN: "1234", NewPIN: "4321", SIMToken: s.SIMToken}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if d.writes != 0 {
		t.Fatal("canceled request reached device")
	}
}

func TestSIMPINActivationInvalidatesTokenBeforeIdentityChanges(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	m.InvalidateSIMActivation()
	if err := m.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "1234", NewPIN: "0001", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMChanged) {
		t.Fatal(err)
	}
	if d.writes != 0 {
		t.Fatal("stale activation reached SIM")
	}
	// A replacement modem with the same ICCID has a different instance token.
	replacement, _, next := pinFixture(t, wwan.SIMLockNone)
	if next.SIMToken == s.SIMToken {
		t.Fatal("token reused across modem instances")
	}
	if err := replacement.ChangeSIMPIN(t.Context(), SIMPINChange{OldPIN: "1234", NewPIN: "0001", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMChanged) {
		t.Fatal(err)
	}
}

func TestSIMPINProtocolRetriesSurviveRefreshFailure(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockPUK)
	d.writeErr = &wwan.PINOperationError{Status: wwan.SIMPINStatus{Supported: true, Lock: wwan.SIMLockPUK, PUKRetries: 9, PUKRetriesKnown: true}, Cause: errors.New("rejected")}
	d.onWrite = func() { d.readErr = errors.New("disconnected") }
	if err := m.UnlockSIM(t.Context(), SIMUnlock{PUK: "00123456", NewPIN: "0001", SIMToken: s.SIMToken}); !errors.Is(err, ErrSIMIncorrectPUK) {
		t.Fatal(err)
	}
	if d.writes != 1 {
		t.Fatal("replayed credential")
	}
}

func TestSIMPINUnlockDoesNotEnableAnotherSIM(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockPIN)
	d.onWrite = func() { d.info.ICCID = "SIM-B" }
	if err := m.UnlockSIM(t.Context(), SIMUnlock{PIN: "1234", SIMToken: s.SIMToken}); !errors.Is(err, ErrEnableAfterSIMUnlock) {
		t.Fatal(err)
	}
	if d.writes != 1 || d.powers != 0 {
		t.Fatal("enabled a replacement SIM after unlocking the previous one")
	}
}

func TestSIMPINConcurrentProtectionSubmissions(t *testing.T) {
	m, d, s := pinFixture(t, wwan.SIMLockNone)
	entered, release := make(chan struct{}), make(chan struct{})
	d.onWrite = func() { close(entered); <-release }
	result := make(chan error, 2)
	submit := func() {
		result <- m.SetSIMPINProtection(t.Context(), SIMPINProtection{PIN: "1234", Enabled: false, SIMToken: s.SIMToken})
	}
	go submit()
	<-entered
	go submit()
	close(release)
	for range 2 {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	if d.writes != 1 {
		t.Fatalf("wrote %d credentials", d.writes)
	}
}
