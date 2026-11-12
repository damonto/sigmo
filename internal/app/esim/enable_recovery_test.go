package esim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	sgp22 "github.com/damonto/euicc-go/v2"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestEnableProfileConfirmsFinalState(t *testing.T) {
	for _, tt := range []struct {
		name            string
		enableErr       error
		ensureErr       error
		confirmationErr error
		wrongEUICC      bool
		disabled        bool
		retiredOnce     bool
		wantFailure     bool
		wantEnsure      bool
		wantMessage     string
	}{
		{name: "successful reply still requires state confirmation", wantEnsure: true},
		{name: "successful reply with disabled target", disabled: true, wantFailure: true, wantEnsure: true, wantMessage: "target eSIM profile is not enabled"},
		{name: "successful reply with replaced eUICC", wrongEUICC: true, wantFailure: true, wantEnsure: true, wantMessage: "eUICC changed"},
		{name: "QMI internal error after successful switch", enableErr: qcom.QMIErrorInternal, wantEnsure: true},
		{name: "QMI invalid argument after successful switch", enableErr: qcom.QMIErrorInvalidArgument, wantEnsure: true},
		{name: "QMI aborted after successful switch", enableErr: qcom.QMIErrorAborted, wantEnsure: true},
		{name: "retired QMI client after successful switch", enableErr: qcom.QMIErrorInvalidClientID, wantEnsure: true},
		{name: "replacement retires during confirmation", enableErr: qcom.QMIErrorInternal, retiredOnce: true, wantEnsure: true},
		{name: "target never becomes ready", enableErr: qcom.QMIErrorInternal, ensureErr: context.DeadlineExceeded, wantFailure: true, wantEnsure: true},
		{name: "target remains disabled", enableErr: qcom.QMIErrorInternal, disabled: true, wantFailure: true, wantEnsure: true, wantMessage: "target eSIM profile is not enabled"},
		{name: "eUICC replaced", enableErr: qcom.QMIErrorInternal, wrongEUICC: true, wantFailure: true, wantEnsure: true, wantMessage: "eUICC changed"},
		{name: "confirmation keeps failing", enableErr: qcom.QMIErrorInternal, confirmationErr: qcom.QMIErrorInternal, wantFailure: true, wantEnsure: true},
		{name: "CAT busy is a card rejection", enableErr: sgp22.ErrCatBusy, wantFailure: true},
		{name: "missing ICCID is a card rejection", enableErr: sgp22.ErrICCIDNotFound, wantFailure: true},
		{name: "unknown error is preserved", enableErr: errors.New("invalid command"), wantFailure: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				iccid := mustTestICCID(t, "8985200012345678901")
				current := &mmodem.Modem{EquipmentIdentifier: "modem"}
				replacement := &mmodem.Modem{EquipmentIdentifier: "modem"}
				enableClient := &fakeLifecycleClient{}
				if tt.enableErr != nil {
					enableClient.enableErr = fmt.Errorf("send APDU: %w", tt.enableErr)
				}
				confirmation := &fakeLifecycleClient{
					eid: []byte{1}, eidErr: tt.confirmationErr,
					profiles: []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileEnabled}},
				}
				if tt.wrongEUICC {
					confirmation.eid = []byte{2}
				}
				if tt.disabled {
					confirmation.profiles = disabledProfiles(iccid)
				}
				notifications := &fakeLifecycleClient{eid: []byte{1}, notifications: []*sgp22.NotificationMetadata{{SequenceNumber: 2}}}
				clients := []lifecycleClient{confirmation}
				if tt.confirmationErr != nil {
					for range profileConfirmAttempts - 1 {
						clients = append(clients, &fakeLifecycleClient{eidErr: tt.confirmationErr})
					}
				}
				clients = append(clients, notifications)
				ensures, confirmations := 0, 0
				l := &lifecycle{
					ensureSIMVisible: func(ctx context.Context, _ *mmodem.Modem, target mmodem.SIMTarget) (*mmodem.Modem, error) {
						ensures++
						if !enableClient.invalidated {
							t.Error("waiting for recovery while retaining the old LPA lease")
						}
						if target.ICCID != iccid.String() || !target.RequireEUICC {
							t.Fatalf("unexpected readiness target: %+v", target)
						}
						return replacement, tt.ensureErr
					},
					newClient: func(_ context.Context, active *mmodem.Modem, _ *settings.Settings, seID string) (lifecycleClient, error) {
						if active != replacement || seID != "se0" {
							t.Fatal("confirmation did not use the replacement and original secure element")
						}
						confirmations++
						if tt.retiredOnce && confirmations == 1 {
							return nil, lpa.ErrModemRetired
						}
						if len(clients) == 0 {
							t.Fatal("unexpected extra LPA acquisition")
						}
						client := clients[0]
						clients = clients[1:]
						return client, nil
					},
				}
				session := &enableSession{
					l: l, modem: current, seID: "se0", iccid: iccid,
					eid: []byte{1}, client: enableClient, lastSeq: 1,
				}
				active, err := session.Enable(t.Context())
				if active != nil {
					session.notify(t.Context(), active, testModemFinder{replacement})
				}
				if tt.wantFailure {
					if err == nil || (tt.enableErr != nil && !errors.Is(err, tt.enableErr)) || !strings.Contains(err.Error(), tt.wantMessage) {
						t.Fatalf("Enable() = %v, want original error %v and %q", err, tt.enableErr, tt.wantMessage)
					}
					if tt.ensureErr != nil && !errors.Is(err, tt.ensureErr) {
						t.Fatalf("Enable() lost recovery error: %v", err)
					}
					if notifications.sentNotifications != 0 {
						t.Error("sent notifications before confirming a successful switch")
					}
				} else if err != nil || notifications.sentNotifications != 1 {
					t.Fatalf("Enable() = %v, notification count = %d", err, notifications.sentNotifications)
				}
				if (ensures != 0) != tt.wantEnsure {
					t.Fatalf("readiness checks = %d, want checked %v", ensures, tt.wantEnsure)
				}
				if len(enableClient.enableRefreshes) != 1 || !enableClient.released {
					t.Fatal("enable command was retried or its lease was retained")
				}
			})
		})
	}
}

func TestEnableProfileReleasesCoordinationBeforeNotifications(t *testing.T) {
	iccid := mustTestICCID(t, "8985200012345678901")
	old := &mmodem.Modem{EquipmentIdentifier: "modem"}
	current := &mmodem.Modem{EquipmentIdentifier: "modem", Status: wwanmodem.Status{SIM: wwanmodem.SIMStateLocked}}
	registry := &simChangeTestRegistry{current: current}
	clients := []lifecycleClient{
		&fakeLifecycleClient{eid: []byte{1}, profiles: disabledProfiles(iccid)},
		&fakeLifecycleClient{eid: []byte{1}, profiles: []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileEnabled}}},
		&fakeLifecycleClient{eid: []byte{1}, notifications: []*sgp22.NotificationMetadata{{SequenceNumber: 2}}},
	}
	calls := 0
	service := &Service{registry: registry, lifecycle: &lifecycle{
		newClient: func(_ context.Context, device *mmodem.Modem, _ *settings.Settings, _ string) (lifecycleClient, error) {
			if device != current || calls >= len(clients) {
				t.Fatal("unexpected LPA acquisition")
			}
			if registry.changing != (calls < 2) {
				t.Fatal("preparation and confirmation must be coordinated; notification delivery must not hold coordination")
			}
			client := clients[calls]
			calls++
			return client, nil
		},
		ensureSIMVisible: func(context.Context, *mmodem.Modem, mmodem.SIMTarget) (*mmodem.Modem, error) {
			if !registry.changing {
				t.Fatal("readiness checked outside the coordinated operation")
			}
			return current, nil
		},
	}}
	if err := service.EnableProfile(t.Context(), old, "se0", iccid); err != nil {
		t.Fatal(err)
	}
	if calls != len(clients) || registry.changing {
		t.Fatal("profile activation did not complete")
	}
}

type simChangeTestRegistry struct {
	current  *mmodem.Modem
	changing bool
}

func (r *simChangeTestRegistry) Find(ctx context.Context, id string) (*mmodem.Modem, error) {
	return (testModemFinder{r.current}).Find(ctx, id)
}

func (r *simChangeTestRegistry) WithSIMChange(ctx context.Context, current *mmodem.Modem, run func(context.Context, *mmodem.Modem) error) error {
	active, err := r.Find(ctx, current.EquipmentIdentifier)
	if err != nil {
		return err
	}
	r.changing = true
	defer func() { r.changing = false }()
	return run(ctx, active)
}

func TestEnableNotificationDoesNotSendAfterEUICCReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := &mmodem.Modem{EquipmentIdentifier: "modem"}
		replacement := &mmodem.Modem{EquipmentIdentifier: "modem"}
		client := &fakeLifecycleClient{
			eid: []byte{2}, notifications: []*sgp22.NotificationMetadata{{SequenceNumber: 2}},
		}
		l := &lifecycle{newClient: func(_ context.Context, modem *mmodem.Modem, _ *settings.Settings, _ string) (lifecycleClient, error) {
			if modem == old {
				return nil, lpa.ErrModemRetired
			}
			return client, nil
		}}
		session := &enableSession{l: l, modem: old, eid: []byte{1}, lastSeq: 1}
		session.notify(t.Context(), old, testModemFinder{replacement})
		if client.sentNotifications != 0 || client.removedNotifications != 0 || !client.released {
			t.Fatal("notification retry used a different eUICC or retained its client")
		}
	})
}
