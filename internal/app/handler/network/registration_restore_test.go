package network

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// registrarProbe records registration calls and answers selection reads.
type registrarProbe struct {
	mu           sync.Mutex
	calls        []string
	selection    wwan.NetworkSelection
	selectionErr error
	operatorCode string
	registerErr  error
	autoErr      error
}

func (p *registrarProbe) record(call string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
}

func (p *registrarProbe) Selection(context.Context, *mmodem.Modem) (wwan.NetworkSelection, error) {
	p.record("selection")
	return p.selection, p.selectionErr
}

func (p *registrarProbe) OperatorCode(context.Context, *mmodem.Modem) (string, error) {
	p.record("operator")
	return p.operatorCode, nil
}

func (p *registrarProbe) Register(_ context.Context, _ *mmodem.Modem, operatorCode string) error {
	p.record("register:" + operatorCode)
	return p.registerErr
}

func (p *registrarProbe) RegisterAutomatically(context.Context, *mmodem.Modem) error {
	p.record("auto")
	return p.autoErr
}

func (p *registrarProbe) recorded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func newRegistrationTestNetwork(t *testing.T, probe *registrarProbe) (*network, *storage.Store) {
	t.Helper()
	store := openNetworkTestStore(t)
	preferences, err := networkprefs.New(store)
	if err != nil {
		t.Fatalf("networkprefs.New() error = %v", err)
	}
	n, err := newNetwork(preferences, store, nil)
	if err != nil {
		t.Fatalf("newNetwork() error = %v", err)
	}
	n.registrar = probe
	return n, store
}

func TestSetRegistrationSavesProfilePreference(t *testing.T) {
	t.Parallel()

	errRegister := errors.New("register rejected")
	tests := []struct {
		name        string
		req         SetRegistrationRequest
		registerErr error
		wantCalls   []string
		wantPref    *networkRegistrationPreference
		wantErr     error
	}{
		{
			name:      "manual",
			req:       SetRegistrationRequest{Mode: RegistrationModeManual, OperatorCode: " 46001 "},
			wantCalls: []string{"register:46001"},
			wantPref:  &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
		},
		{
			name:      "automatic",
			req:       SetRegistrationRequest{Mode: RegistrationModeAutomatic, OperatorCode: "46001"},
			wantCalls: []string{"auto"},
			wantPref:  &networkRegistrationPreference{Mode: RegistrationModeAutomatic},
		},
		{
			name:    "manual without operator",
			req:     SetRegistrationRequest{Mode: RegistrationModeManual},
			wantErr: errOperatorCodeRequired,
		},
		{
			name:    "unknown mode",
			req:     SetRegistrationRequest{Mode: "roaming"},
			wantErr: errRegistrationModeInvalid,
		},
		{
			name:        "modem rejects manual registration",
			req:         SetRegistrationRequest{Mode: RegistrationModeManual, OperatorCode: "46001"},
			registerErr: errRegister,
			wantCalls:   []string{"register:46001"},
			wantErr:     errRegister,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := &registrarProbe{registerErr: tt.registerErr}
			n, store := newRegistrationTestNetwork(t, probe)
			modem := &mmodem.Modem{EquipmentIdentifier: "imei-1", SIM: &mmodem.SIM{Identifier: "iccid-1"}}

			err := n.SetRegistration(t.Context(), modem, tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetRegistration() error = %v, want %v", err, tt.wantErr)
			}
			if got := probe.recorded(); !slices.Equal(got, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", got, tt.wantCalls)
			}
			var pref networkRegistrationPreference
			getErr := store.Get(t.Context(), "profile:iccid-1", networkRegistrationKey, &pref)
			if tt.wantPref == nil {
				if !errors.Is(getErr, storage.ErrNotFound) {
					t.Fatalf("stored preference = %+v, %v; want not found", pref, getErr)
				}
				return
			}
			if getErr != nil {
				t.Fatalf("Get() error = %v", getErr)
			}
			if pref != *tt.wantPref {
				t.Fatalf("stored preference = %+v, want %+v", pref, *tt.wantPref)
			}
		})
	}
}

func TestRegistrationPrefersModemSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		saved        *networkRegistrationPreference
		selection    wwan.NetworkSelection
		selectionErr error
		want         RegistrationResponse
		wantErr      bool
	}{
		{
			name:      "modem automatic overrides stale manual preference",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionAutomatic},
			want:      RegistrationResponse{Mode: RegistrationModeAutomatic},
		},
		{
			name:      "modem manual with operator",
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46000"},
			want:      RegistrationResponse{Mode: RegistrationModeManual, OperatorCode: "46000"},
		},
		{
			name:      "modem manual without operator falls back to saved code",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual},
			want:      RegistrationResponse{Mode: RegistrationModeManual, OperatorCode: "46001"},
		},
		{
			name:         "unsupported selection read uses saved preference",
			saved:        &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selectionErr: wwan.ErrUnsupported,
			want:         RegistrationResponse{Mode: RegistrationModeManual, OperatorCode: "46001"},
		},
		{
			name:         "unsupported selection read without preference is automatic",
			selectionErr: wwan.ErrUnsupported,
			want:         RegistrationResponse{Mode: RegistrationModeAutomatic},
		},
		{
			name:      "unknown selection uses saved preference",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeAutomatic},
			selection: wwan.NetworkSelection{},
			want:      RegistrationResponse{Mode: RegistrationModeAutomatic},
		},
		{
			name:         "selection read failure is reported",
			selectionErr: errors.New("QMI timeout"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := &registrarProbe{selection: tt.selection, selectionErr: tt.selectionErr}
			n, store := newRegistrationTestNetwork(t, probe)
			modem := &mmodem.Modem{EquipmentIdentifier: "imei-1", SIM: &mmodem.SIM{Identifier: "iccid-1"}}
			if tt.saved != nil {
				if err := store.Put(t.Context(), "profile:iccid-1", networkRegistrationKey, *tt.saved); err != nil {
					t.Fatalf("Put() error = %v", err)
				}
			}

			got, err := n.Registration(t.Context(), modem)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Registration() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Registration() error = %v", err)
			}
			if *got != tt.want {
				t.Fatalf("Registration() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestRegistrationRestoreDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		saved        *networkRegistrationPreference
		selection    wwan.NetworkSelection
		selectionErr error
		operatorCode string
		wantCalls    []string
	}{
		{
			name:      "no preference releases inherited manual selection",
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46000"},
			wantCalls: []string{"selection", "auto"},
		},
		{
			name:      "automatic preference releases manual selection",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeAutomatic},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46000"},
			wantCalls: []string{"selection", "auto"},
		},
		{
			name:      "no preference leaves automatic modem alone",
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionAutomatic},
			wantCalls: []string{"selection"},
		},
		{
			name:         "no preference leaves unreadable selection alone",
			selectionErr: wwan.ErrUnsupported,
			wantCalls:    []string{"selection"},
		},
		{
			name:      "manual preference already applied",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46001"},
			wantCalls: []string{"selection"},
		},
		{
			name:      "manual preference reapplied over automatic modem",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionAutomatic},
			wantCalls: []string{"selection", "register:46001"},
		},
		{
			name:      "manual preference reapplied over other manual operator",
			saved:     &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46000"},
			wantCalls: []string{"selection", "register:46001"},
		},
		{
			name:         "manual preference with unreadable selection trusts registered operator",
			saved:        &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selectionErr: wwan.ErrUnsupported,
			operatorCode: "46001",
			wantCalls:    []string{"selection", "operator"},
		},
		{
			name:         "manual preference with unreadable selection registers when operator differs",
			saved:        &networkRegistrationPreference{Mode: RegistrationModeManual, OperatorCode: "46001"},
			selectionErr: wwan.ErrUnsupported,
			operatorCode: "46000",
			wantCalls:    []string{"selection", "operator", "register:46001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := openNetworkTestStore(t)
			modem := &mmodem.Modem{EquipmentIdentifier: "imei-1", SIM: &mmodem.SIM{Identifier: "iccid-1"}}
			if tt.saved != nil {
				if err := store.Put(t.Context(), "profile:iccid-1", networkRegistrationKey, *tt.saved); err != nil {
					t.Fatalf("Put() error = %v", err)
				}
			}
			probe := &registrarProbe{selection: tt.selection, selectionErr: tt.selectionErr, operatorCode: tt.operatorCode}
			restorer := newRegistrationRestorer(store)
			restorer.registrar = probe

			if err := restorer.restoreModem(t.Context(), modem); err != nil {
				t.Fatalf("restoreModem() error = %v", err)
			}
			if got := probe.recorded(); !slices.Equal(got, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", got, tt.wantCalls)
			}
		})
	}
}

func TestRegistrationRestoreRerunsOnSIMChange(t *testing.T) {
	store := openNetworkTestStore(t)
	modem := &mmodem.Modem{EquipmentIdentifier: "imei-1", SIM: &mmodem.SIM{Identifier: "iccid-1"}}
	probe := &registrarProbe{selection: wwan.NetworkSelection{Mode: wwan.NetworkSelectionManual, OperatorID: "46000"}}
	restorer := newRegistrationRestorer(store)
	restorer.registrar = probe

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		restorer.run(ctx, modem)
	}()
	waitForCalls(t, probe, 2)

	restorer.notify(modem)
	waitForCalls(t, probe, 4)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run() did not stop after cancellation")
	}
	want := []string{"selection", "auto", "selection", "auto"}
	if got := probe.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func waitForCalls(t *testing.T, probe *registrarProbe, count int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(probe.recorded()) >= count {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("calls = %v, want at least %d", probe.recorded(), count)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
