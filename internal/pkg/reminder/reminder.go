package reminder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/webpush"
)

type ProfileType string

const (
	ProfileTypePSIM ProfileType = "psim"
	ProfileTypeESIM ProfileType = "esim"
)

type Scheduler struct {
	store    *storage.Store
	settings *settings.Store
	webPush  *webpush.Client
	wake     chan struct{}
	now      func() time.Time
	deliver  func(context.Context, notifyevent.ReminderEvent) error
}

func New(store *storage.Store, settingsStore *settings.Store, webPush *webpush.Client) (*Scheduler, error) {
	if store == nil {
		return nil, errors.New("reminder storage is required")
	}
	if settingsStore == nil {
		return nil, errors.New("reminder settings store is required")
	}
	scheduler := &Scheduler{
		store:    store,
		settings: settingsStore,
		webPush:  webPush,
		wake:     make(chan struct{}, 1),
		now:      func() time.Time { return time.Now().UTC() },
	}
	scheduler.deliver = scheduler.deliverNotification
	return scheduler, nil
}

func (s *Scheduler) Get(ctx context.Context, profileType ProfileType, profileID string) (storage.Reminder, bool, error) {
	return s.store.GetReminder(ctx, string(profileType), strings.TrimSpace(profileID))
}

func (s *Scheduler) Save(ctx context.Context, value storage.Reminder) error {
	if !ProfileType(value.ProfileType).Valid() {
		return fmt.Errorf("unsupported reminder profile type %q", value.ProfileType)
	}
	if err := s.store.UpsertReminder(ctx, value); err != nil {
		return err
	}
	s.signal()
	return nil
}

func (s *Scheduler) Delete(ctx context.Context, profileType ProfileType, profileID string) error {
	if err := s.store.DeleteReminder(ctx, string(profileType), strings.TrimSpace(profileID)); err != nil {
		return err
	}
	s.signal()
	return nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		if err := s.processDue(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		nextAt, ok, err := s.store.NextReminderAt(ctx)
		if err != nil {
			return err
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-s.wake:
				continue
			}
		}

		delay := time.Until(nextAt)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return nil
		case <-s.wake:
			stopTimer(timer)
		case <-timer.C:
		}
	}
}

func (s *Scheduler) processDue(ctx context.Context) error {
	due, err := s.store.DueReminders(ctx, s.now())
	if err != nil {
		return err
	}
	for _, item := range due {
		claimed, ok, err := s.store.ClaimReminder(ctx, item)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := s.processOne(ctx, claimed); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) processOne(ctx context.Context, item storage.Reminder) error {
	currentSettings := s.settings.Snapshot()
	modemLabel := item.ModemID
	if alias := strings.TrimSpace(currentSettings.FindModem(item.ModemID).Alias); alias != "" {
		modemLabel = alias
	}
	event := notifyevent.ReminderEvent{
		ProfileType: item.ProfileType,
		ProfileID:   item.ProfileID,
		ProfileName: item.ProfileName,
		ModemID:     item.ModemID,
		SEID:        item.SEID,
		Modem:       modemLabel,
		ScheduledAt: item.NextAt,
		Content:     item.Content,
	}
	if err := s.deliver(ctx, event); err != nil {
		slog.Error(
			"reminder delivery completed with errors",
			"profile_type", item.ProfileType,
			"profile_id", item.ProfileID,
			"scheduled_at", item.NextAt,
			"error", err,
		)
	} else {
		slog.Info(
			"reminder delivery completed",
			"profile_type", item.ProfileType,
			"profile_id", item.ProfileID,
			"scheduled_at", item.NextAt,
		)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if item.RepeatDays == nil {
		deleted, err := s.store.DeleteClaimedReminder(ctx, item)
		if err != nil {
			return err
		}
		if deleted {
			slog.Info("one-shot reminder completed", "profile_type", item.ProfileType, "profile_id", item.ProfileID)
		} else {
			slog.Info(
				"one-shot reminder completion skipped",
				"profile_type", item.ProfileType,
				"profile_id", item.ProfileID,
				"reason", "newer revision exists",
			)
		}
		return nil
	}
	if *item.RepeatDays > maxRepeatDays {
		return fmt.Errorf("reminder repeat days %d exceed maximum %d", *item.RepeatDays, maxRepeatDays)
	}
	nextAt := s.now().Add(time.Duration(*item.RepeatDays) * 24 * time.Hour)
	advanced, err := s.store.AdvanceClaimedReminder(ctx, item, nextAt)
	if err != nil {
		return err
	}
	if advanced {
		slog.Info(
			"repeating reminder rescheduled",
			"profile_type", item.ProfileType,
			"profile_id", item.ProfileID,
			"repeat_days", *item.RepeatDays,
			"next_at", nextAt,
		)
	} else {
		slog.Info(
			"repeating reminder reschedule skipped",
			"profile_type", item.ProfileType,
			"profile_id", item.ProfileID,
			"reason", "newer revision exists",
		)
	}
	return nil
}

func (s *Scheduler) deliverNotification(ctx context.Context, event notifyevent.ReminderEvent) error {
	currentSettings := s.settings.Snapshot()
	notifier, err := notify.New(&currentSettings)
	if err != nil {
		return fmt.Errorf("create notifier: %w", err)
	}
	notifierErr := notifier.Send(ctx, event)
	if s.webPush != nil {
		if err := s.webPush.Send(ctx, event); err != nil {
			slog.Warn("send reminder web push", "profile_type", event.ProfileType, "profile_id", event.ProfileID, "error", err)
		}
	}
	return notifierErr
}

func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (p ProfileType) Valid() bool {
	return p == ProfileTypePSIM || p == ProfileTypeESIM
}

func (p ProfileType) String() string {
	return string(p)
}

func stopTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}
