package esim

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/euicc-go/bertlv"
	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestActiveProfile(t *testing.T) {
	t.Parallel()

	target, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	other, err := sgp22.NewICCID("8985200099999999999")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}

	tests := []struct {
		name     string
		profiles []*sgp22.ProfileInfo
		want     bool
	}{
		{
			name: "target enabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: target, ProfileState: sgp22.ProfileEnabled},
			},
			want: true,
		},
		{
			name: "target disabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: target, ProfileState: sgp22.ProfileDisabled},
			},
			want: false,
		},
		{
			name: "other enabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: other, ProfileState: sgp22.ProfileEnabled},
			},
			want: false,
		},
		{
			name: "nil profile",
			profiles: []*sgp22.ProfileInfo{
				nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := activeProfile(tt.profiles, target); got != tt.want {
				t.Fatalf("activeProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnableSessionEnable(t *testing.T) {
	iccid, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	enableErr := errors.New("qmi enable returned unknown")
	ensureErr := errors.New("SIM not visible")
	current := &mmodem.Modem{EquipmentIdentifier: "354015820228039"}
	visibleModem := &mmodem.Modem{EquipmentIdentifier: current.EquipmentIdentifier}

	tests := []struct {
		name              string
		enableErr         error
		ensureErr         error
		wantErr           error
		wantEnsure        bool
		wantEnableClosed  bool
		wantNotifications bool
	}{
		{
			name:              "enable succeeds",
			wantEnsure:        true,
			wantEnableClosed:  true,
			wantNotifications: true,
		},
		{
			name:             "ensure SIM visible error is returned",
			ensureErr:        ensureErr,
			wantErr:          ensureErr,
			wantEnsure:       true,
			wantEnableClosed: true,
		},
		{
			name:             "enable error returns original error immediately",
			enableErr:        enableErr,
			wantErr:          enableErr,
			wantEnableClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enableClient := &fakeLifecycleClient{enableErr: tt.enableErr}
			notificationClient := &fakeLifecycleClient{
				notifications: []*sgp22.NotificationMetadata{
					{SequenceNumber: 2},
				},
			}
			factoryClients := []lifecycleClient{notificationClient}

			var ensureCalled bool
			l := &lifecycle{
				settings: &settings.Settings{},
				newClient: func(*mmodem.Modem, *settings.Settings) (lifecycleClient, error) {
					if len(factoryClients) == 0 {
						return &fakeLifecycleClient{profiles: disabledProfiles(iccid)}, nil
					}
					client := factoryClients[0]
					factoryClients = factoryClients[1:]
					return client, nil
				},
				ensureSIMVisible: func(ctx context.Context, modem *mmodem.Modem, target mmodem.SIMTarget) (*mmodem.Modem, error) {
					_ = ctx.Err()
					ensureCalled = true
					if modem != current {
						t.Fatalf("modem = %p, want %p", modem, current)
					}
					if target.ICCID != iccid.String() {
						t.Fatalf("target ICCID = %q, want %q", target.ICCID, iccid.String())
					}
					if tt.ensureErr != nil {
						return nil, tt.ensureErr
					}
					return visibleModem, nil
				},
			}
			session := &enableSession{
				l:       l,
				modem:   current,
				iccid:   iccid,
				client:  enableClient,
				lastSeq: 1,
			}

			err := session.Enable(context.Background())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Enable() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Enable() error = %v", err)
			}
			if ensureCalled != tt.wantEnsure {
				t.Fatalf("ensure called = %v, want %v", ensureCalled, tt.wantEnsure)
			}
			if enableClient.closed != tt.wantEnableClosed {
				t.Fatalf("enable client closed = %v, want %v", enableClient.closed, tt.wantEnableClosed)
			}
			if !enableClient.enableRefresh {
				t.Fatal("enable refresh = false, want true")
			}
			if tt.wantNotifications && notificationClient.sentNotifications != 1 {
				t.Fatalf("sent notifications = %d, want 1", notificationClient.sentNotifications)
			}
		})
	}
}

type fakeLifecycleClient struct {
	profiles            []*sgp22.ProfileInfo
	notifications       []*sgp22.NotificationMetadata
	enableErr           error
	listProfileErr      error
	listNotificationErr error
	deleteErr           error
	sendErr             error
	closed              bool
	enableRefresh       bool
	sentNotifications   int
}

func (f *fakeLifecycleClient) ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error) {
	return f.profiles, f.listProfileErr
}

func (f *fakeLifecycleClient) ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error) {
	return f.notifications, f.listNotificationErr
}

func (f *fakeLifecycleClient) EnableProfile(_ any, refresh bool) error {
	f.enableRefresh = refresh
	return f.enableErr
}

func (f *fakeLifecycleClient) Delete(sgp22.ICCID) error {
	return f.deleteErr
}

func (f *fakeLifecycleClient) SendNotification(any, bool) error {
	f.sentNotifications++
	return f.sendErr
}

func (f *fakeLifecycleClient) Close() error {
	f.closed = true
	return nil
}

func disabledProfiles(iccid sgp22.ICCID) []*sgp22.ProfileInfo {
	return []*sgp22.ProfileInfo{
		{ICCID: iccid, ProfileState: sgp22.ProfileDisabled},
	}
}
