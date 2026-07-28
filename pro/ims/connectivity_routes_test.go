//go:build ims

package ims

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type routedAccessProbe struct {
	connectedValue bool
	err            error
	statusCalls    int
}

func (p *routedAccessProbe) connected(context.Context, *mmodem.Modem) (bool, error) {
	p.statusCalls++
	return p.connectedValue, p.err
}

func (*routedAccessProbe) SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error) {
	return storage.Message{}, nil
}

func (*routedAccessProbe) ApplyPendingSMSStatus(context.Context, storage.Message) error {
	return nil
}

func (*routedAccessProbe) ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error) {
	return "", nil
}

func TestSelectPreferredAccessPrefersWiFiCalling(t *testing.T) {
	tests := []struct {
		name           string
		wifiConnected  bool
		volteConnected bool
		wantWiFi       bool
		wantVoLTE      bool
	}{
		{name: "wifi calling wins when both are connected", wifiConnected: true, volteConnected: true, wantWiFi: true},
		{name: "volte is the fallback", volteConnected: true, wantVoLTE: true},
		{name: "no connected route"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wifiCalling := &routedAccessProbe{connectedValue: tt.wifiConnected}
			volte := &routedAccessProbe{connectedValue: tt.volteConnected}
			got, err := selectPreferredAccess(context.Background(), &mmodem.Modem{}, wifiCalling, volte)
			if err != nil {
				t.Fatalf("selectPreferredAccess() error = %v", err)
			}
			switch {
			case tt.wantWiFi && got != wifiCalling:
				t.Fatalf("selectPreferredAccess() = %T, want Wi-Fi Calling", got)
			case tt.wantVoLTE && got != volte:
				t.Fatalf("selectPreferredAccess() = %T, want VoLTE", got)
			case !tt.wantWiFi && !tt.wantVoLTE && got != nil:
				t.Fatalf("selectPreferredAccess() = %T, want nil", got)
			}
		})
	}
}

func TestSelectPreferredAccessSkipsVoLTEWhenWiFiCallingConnected(t *testing.T) {
	statusErr := errors.New("VoLTE status")
	wifiCalling := &routedAccessProbe{connectedValue: true}
	volte := &routedAccessProbe{err: statusErr}

	got, err := selectPreferredAccess(context.Background(), &mmodem.Modem{}, wifiCalling, volte)
	if err != nil {
		t.Fatalf("selectPreferredAccess() error = %v", err)
	}
	if got != wifiCalling {
		t.Fatalf("selectPreferredAccess() = %T, want Wi-Fi Calling", got)
	}
	if volte.statusCalls != 0 {
		t.Fatalf("VoLTE Status() calls = %d, want 0", volte.statusCalls)
	}
}

func TestConnectivityRoutesRejectIncompleteFacade(t *testing.T) {
	connectivity := &Connectivity{}

	if _, err := connectivity.preferredAccess(context.Background(), &mmodem.Modem{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("preferredAccess() error = %v, want %v", err, ErrUnavailable)
	}
	if err := connectivity.MessageRoute().ApplyPendingSMSStatus(context.Background(), storage.Message{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ApplyPendingSMSStatus() error = %v, want %v", err, ErrUnavailable)
	}
}
