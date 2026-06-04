package esim

import (
	"context"
	"errors"
	"testing"
	"time"

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
	current := &mmodem.Modem{EquipmentIdentifier: "354015820228039"}
	reloadedModem := &mmodem.Modem{EquipmentIdentifier: current.EquipmentIdentifier}

	tests := []struct {
		name              string
		enableErr         error
		restartErr        error
		findResults       []findResult
		wantErr           error
		wantRestart       bool
		wantEnableClosed  bool
		wantNotifications bool
		wantWaitForModem  bool
		wantFindCalls     int
	}{
		{
			name:              "enable succeeds",
			findResults:       []findResult{{modem: current}},
			wantRestart:       true,
			wantEnableClosed:  true,
			wantNotifications: true,
			wantFindCalls:     1,
		},
		{
			name: "enable succeeds after final modem availability wait",
			findResults: []findResult{
				{err: mmodem.ErrNotFound},
				{modem: reloadedModem},
			},
			wantRestart:       true,
			wantEnableClosed:  true,
			wantNotifications: true,
			wantWaitForModem:  true,
			wantFindCalls:     2,
		},
		{
			name:              "restart error succeeds when modem is ready",
			restartErr:        errors.New("QMI UIM power off failed"),
			findResults:       []findResult{{err: mmodem.ErrNotFound}, {modem: reloadedModem}},
			wantRestart:       true,
			wantEnableClosed:  true,
			wantNotifications: true,
			wantWaitForModem:  true,
			wantFindCalls:     2,
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

			var (
				restartCalled      bool
				findCalls          int
				waitForModemCalled bool
			)
			l := &lifecycle{
				settings:     &settings.Settings{},
				readyTimeout: time.Millisecond,
				newClient: func(*mmodem.Modem, *settings.Settings) (lifecycleClient, error) {
					if len(factoryClients) == 0 {
						return &fakeLifecycleClient{profiles: disabledProfiles(iccid)}, nil
					}
					client := factoryClients[0]
					factoryClients = factoryClients[1:]
					return client, nil
				},
				findModem: func(ctx context.Context, id string) (*mmodem.Modem, error) {
					_ = ctx.Err()
					if id == "" {
						t.Fatal("id is empty")
					}
					findCalls++
					if len(tt.findResults) == 0 {
						return current, nil
					}
					index := min(findCalls-1, len(tt.findResults)-1)
					result := tt.findResults[index]
					return result.modem, result.err
				},
				waitForModemReload: func(ctx context.Context, modem *mmodem.Modem) (*mmodem.Modem, error) {
					_ = ctx.Err()
					if modem == nil {
						t.Fatal("modem is nil")
					}
					waitForModemCalled = true
					return reloadedModem, nil
				},
				restartModem: func(ctx context.Context, modem *mmodem.Modem, compatible bool) error {
					_ = ctx.Err()
					if modem == nil {
						t.Fatal("modem is nil")
					}
					_ = compatible
					restartCalled = true
					return tt.restartErr
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
			if restartCalled != tt.wantRestart {
				t.Fatalf("restart called = %v, want %v", restartCalled, tt.wantRestart)
			}
			if findCalls < tt.wantFindCalls {
				t.Fatalf("find calls = %d, want at least %d", findCalls, tt.wantFindCalls)
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
			if waitForModemCalled != tt.wantWaitForModem {
				t.Fatalf("wait for modem called = %v, want %v", waitForModemCalled, tt.wantWaitForModem)
			}
		})
	}
}

type findResult struct {
	modem *mmodem.Modem
	err   error
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

func enabledProfiles(iccid sgp22.ICCID) []*sgp22.ProfileInfo {
	return []*sgp22.ProfileInfo{
		{ICCID: iccid, ProfileState: sgp22.ProfileEnabled},
	}
}

func disabledProfiles(iccid sgp22.ICCID) []*sgp22.ProfileInfo {
	return []*sgp22.ProfileInfo{
		{ICCID: iccid, ProfileState: sgp22.ProfileDisabled},
	}
}
