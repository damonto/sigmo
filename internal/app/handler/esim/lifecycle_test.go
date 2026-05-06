package esim

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/config"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
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
		profileClients    []lifecycleClient
		ctxTimeout        time.Duration
		wantErr           error
		wantRestart       bool
		wantEnableClosed  bool
		wantNotifications bool
		wantFindCalls     int
	}{
		{
			name:              "enable succeeds",
			findResults:       []findResult{{modem: current}},
			profileClients:    []lifecycleClient{&fakeLifecycleClient{profiles: enabledProfiles(iccid)}},
			wantRestart:       true,
			wantEnableClosed:  true,
			wantNotifications: true,
			wantFindCalls:     1,
		},
		{
			name:              "restart error succeeds when enabled profile is confirmed",
			restartErr:        errors.New("qmicli power off failed"),
			findResults:       []findResult{{err: mmodem.ErrNotFound}, {modem: reloadedModem}},
			profileClients:    []lifecycleClient{&fakeLifecycleClient{profiles: enabledProfiles(iccid)}},
			wantRestart:       true,
			wantEnableClosed:  true,
			wantNotifications: true,
			wantFindCalls:     2,
		},
		{
			name:              "enable error succeeds without modem reload when profile becomes active",
			enableErr:         enableErr,
			findResults:       []findResult{{modem: current}},
			profileClients:    []lifecycleClient{&fakeLifecycleClient{profiles: enabledProfiles(iccid)}},
			wantEnableClosed:  true,
			wantNotifications: true,
			wantFindCalls:     1,
		},
		{
			name:              "enable error succeeds after slow modem re-enumeration",
			enableErr:         enableErr,
			findResults:       []findResult{{err: mmodem.ErrNotFound}, {err: mmodem.ErrNotFound}, {modem: reloadedModem}},
			profileClients:    []lifecycleClient{&fakeLifecycleClient{profiles: enabledProfiles(iccid)}},
			wantEnableClosed:  true,
			wantNotifications: true,
			wantFindCalls:     3,
		},
		{
			name:             "enable error returns original error when profile remains disabled and modem stays present",
			enableErr:        enableErr,
			findResults:      []findResult{{modem: current}},
			ctxTimeout:       time.Millisecond,
			wantErr:          enableErr,
			wantEnableClosed: true,
			wantFindCalls:    1,
		},
		{
			name:             "enable error returns timeout while modem remains unavailable",
			enableErr:        enableErr,
			findResults:      []findResult{{err: mmodem.ErrNotFound}},
			ctxTimeout:       time.Millisecond,
			wantErr:          context.DeadlineExceeded,
			wantEnableClosed: true,
			wantFindCalls:    1,
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
			factoryClients = append(append([]lifecycleClient(nil), tt.profileClients...), factoryClients...)

			var (
				restartCalled bool
				findCalls     int
			)
			l := &lifecycle{
				cfg:             &config.Config{},
				confirmInterval: time.Microsecond,
				newClient: func(*mmodem.Modem, *config.Config) (lifecycleClient, error) {
					if len(factoryClients) == 0 {
						return &fakeLifecycleClient{profiles: disabledProfiles(iccid)}, nil
					}
					client := factoryClients[0]
					factoryClients = factoryClients[1:]
					return client, nil
				},
				findModem: func(string) (*mmodem.Modem, error) {
					findCalls++
					if len(tt.findResults) == 0 {
						return current, nil
					}
					index := min(findCalls-1, len(tt.findResults)-1)
					result := tt.findResults[index]
					return result.modem, result.err
				},
				restartModem: func(*mmodem.Modem, bool) error {
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

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			err := session.Enable(ctx)
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
			if tt.wantNotifications && notificationClient.sentNotifications != 1 {
				t.Fatalf("sent notifications = %d, want 1", notificationClient.sentNotifications)
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
	sentNotifications   int
}

func (f *fakeLifecycleClient) ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error) {
	return f.profiles, f.listProfileErr
}

func (f *fakeLifecycleClient) ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error) {
	return f.notifications, f.listNotificationErr
}

func (f *fakeLifecycleClient) EnableProfile(any, bool) error {
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
