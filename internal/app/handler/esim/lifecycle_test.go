package esim

import (
	"context"
	"errors"
	"slices"
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

func TestLifecycleClientCreationUsesOperationContext(t *testing.T) {
	iccid, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	l := &lifecycle{
		newClient: func(ctx context.Context, _ *mmodem.Modem, _ *settings.Settings, _ string) (lifecycleClient, error) {
			return nil, ctx.Err()
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "354015820228039"}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "prepare enable",
			run: func() error {
				_, err := l.PrepareEnable(ctx, modem, "default", iccid)
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				return l.Delete(ctx, modem, "default", iccid)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error = %v, want %v", err, context.Canceled)
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
	notificationErr := errors.New("notification endpoint unavailable")
	current := &mmodem.Modem{
		EquipmentIdentifier: "354015820228039",
		Sim:                 &mmodem.SIM{Identifier: "8985200099999999999"},
	}
	visibleModem := &mmodem.Modem{EquipmentIdentifier: current.EquipmentIdentifier}

	tests := []struct {
		name              string
		enableErr         error
		ensureErr         error
		notificationErr   error
		wantErr           error
		wantEnsure        bool
		wantReleased      bool
		wantInvalidated   bool
		wantNotifications bool
		wantRefreshes     []bool
	}{
		{
			name:              "enable succeeds",
			wantEnsure:        true,
			wantReleased:      true,
			wantInvalidated:   true,
			wantNotifications: true,
			wantRefreshes:     []bool{true},
		},
		{
			name:          "CAT busy is returned",
			enableErr:     sgp22.ErrCatBusy,
			wantErr:       sgp22.ErrCatBusy,
			wantReleased:  true,
			wantRefreshes: []bool{true},
		},
		{
			name:              "notification failure is best effort",
			notificationErr:   notificationErr,
			wantEnsure:        true,
			wantReleased:      true,
			wantInvalidated:   true,
			wantNotifications: true,
			wantRefreshes:     []bool{true},
		},
		{
			name:            "ensure SIM visible error is returned",
			ensureErr:       ensureErr,
			wantErr:         ensureErr,
			wantEnsure:      true,
			wantReleased:    true,
			wantInvalidated: true,
			wantRefreshes:   []bool{true},
		},
		{
			name:          "enable error returns original error immediately",
			enableErr:     enableErr,
			wantErr:       enableErr,
			wantReleased:  true,
			wantRefreshes: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enableClient := &fakeLifecycleClient{enableErr: tt.enableErr}
			notificationClient := &fakeLifecycleClient{
				notifications: []*sgp22.NotificationMetadata{
					{SequenceNumber: 2},
				},
				sendErr: tt.notificationErr,
			}
			factoryClients := []lifecycleClient{notificationClient}

			var ensureCalled bool
			l := &lifecycle{
				settings: &settings.Settings{},
				newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
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
					if target.PreviousICCID != "8985200099999999999" {
						t.Fatalf("previous ICCID = %q, want %q", target.PreviousICCID, "8985200099999999999")
					}
					if !target.RequireEUICC {
						t.Fatal("RequireEUICC = false, want true")
					}
					if tt.ensureErr != nil {
						return nil, tt.ensureErr
					}
					return visibleModem, nil
				},
			}
			session := &enableSession{
				l:             l,
				modem:         current,
				iccid:         iccid,
				previousICCID: activeModemICCID(current),
				client:        enableClient,
				lastSeq:       1,
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
			if enableClient.released != tt.wantReleased {
				t.Fatalf("enable lease released = %v, want %v", enableClient.released, tt.wantReleased)
			}
			if enableClient.invalidated != tt.wantInvalidated {
				t.Fatalf("enable lease invalidated = %v, want %v", enableClient.invalidated, tt.wantInvalidated)
			}
			if !slices.Equal(enableClient.enableRefreshes, tt.wantRefreshes) {
				t.Fatalf("enable refreshes = %v, want %v", enableClient.enableRefreshes, tt.wantRefreshes)
			}
			if tt.wantNotifications && notificationClient.sentNotifications != 1 {
				t.Fatalf("sent notifications = %d, want 1", notificationClient.sentNotifications)
			}
		})
	}
}

func TestSendPendingNotificationsKeepsFailedNotification(t *testing.T) {
	wantErr := errors.New("notification endpoint unavailable")
	client := &fakeLifecycleClient{
		notifications: []*sgp22.NotificationMetadata{{SequenceNumber: 2}},
		sendErrors:    []error{wantErr, nil},
	}
	l := &lifecycle{
		newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
			return client, nil
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "354015820228039"}

	err := l.sendPendingNotifications(t.Context(), modem, "default", 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("first sendPendingNotifications() error = %v, want %v", err, wantErr)
	}
	if client.removedNotifications != 0 || len(client.notifications) != 1 {
		t.Fatalf("failed notification was removed: removed = %d, pending = %d", client.removedNotifications, len(client.notifications))
	}
	if err := l.sendPendingNotifications(t.Context(), modem, "default", 1); err != nil {
		t.Fatalf("second sendPendingNotifications() error = %v", err)
	}
	if client.sentNotifications != 2 || client.removedNotifications != 1 || len(client.notifications) != 0 {
		t.Fatalf("notification attempts/removals/pending = %d/%d/%d, want 2/1/0", client.sentNotifications, client.removedNotifications, len(client.notifications))
	}
}

type fakeLifecycleClient struct {
	profiles             []*sgp22.ProfileInfo
	notifications        []*sgp22.NotificationMetadata
	enableErr            error
	listProfileErr       error
	listNotificationErr  error
	deleteErr            error
	sendErr              error
	sendErrors           []error
	removeErr            error
	released             bool
	invalidated          bool
	enableRefreshes      []bool
	sentNotifications    int
	removedNotifications int
}

func (f *fakeLifecycleClient) ListProfile(any, []bertlv.Tag) ([]*sgp22.ProfileInfo, error) {
	return f.profiles, f.listProfileErr
}

func (f *fakeLifecycleClient) ListNotification(...sgp22.NotificationEvent) ([]*sgp22.NotificationMetadata, error) {
	return f.notifications, f.listNotificationErr
}

func (f *fakeLifecycleClient) EnableProfile(_ any, refresh bool) error {
	f.enableRefreshes = append(f.enableRefreshes, refresh)
	return f.enableErr
}

func (f *fakeLifecycleClient) Delete(sgp22.ICCID) error {
	return f.deleteErr
}

func (f *fakeLifecycleClient) RetrieveNotificationList(sequence any) ([]*sgp22.PendingNotification, error) {
	seq, ok := sequence.(sgp22.SequenceNumber)
	if !ok {
		return nil, errors.New("notification sequence is required")
	}
	for _, notification := range f.notifications {
		if notification != nil && notification.SequenceNumber == seq {
			return []*sgp22.PendingNotification{{Notification: notification}}, nil
		}
	}
	return nil, nil
}

func (f *fakeLifecycleClient) HandleNotification(*sgp22.PendingNotification) error {
	f.sentNotifications++
	if len(f.sendErrors) > 0 {
		err := f.sendErrors[0]
		f.sendErrors = f.sendErrors[1:]
		return err
	}
	return f.sendErr
}

func (f *fakeLifecycleClient) RemoveNotificationFromList(sequence sgp22.SequenceNumber) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removedNotifications++
	f.notifications = slices.DeleteFunc(f.notifications, func(notification *sgp22.NotificationMetadata) bool {
		return notification != nil && notification.SequenceNumber == sequence
	})
	return nil
}

func (f *fakeLifecycleClient) Close() error {
	f.released = true
	return nil
}

func (f *fakeLifecycleClient) Invalidate() error {
	f.released = true
	f.invalidated = true
	return nil
}

func disabledProfiles(iccid sgp22.ICCID) []*sgp22.ProfileInfo {
	return []*sgp22.ProfileInfo{
		{ICCID: iccid, ProfileState: sgp22.ProfileDisabled},
	}
}
