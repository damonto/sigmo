//go:build wifi_calling

package wificalling

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	vowifi "github.com/damonto/vowifi-go"
	"github.com/godbus/dbus/v5"
)

type Config struct {
	Store      *storage.Store
	OnIncoming IncomingSMSFunc
	Websheets  *websheet.Broker
}

type coordinator struct {
	settings   *SettingsStore
	store      *storage.Store
	onIncoming IncomingSMSFunc
	websheets  *websheet.Broker

	mu                sync.Mutex
	sessions          map[string]*sessionState
	smsReports        map[smsReportKey]*smsReportTracker
	pendingSMSReports map[smsReportKey]pendingSMSReport
	voiceSubscribers  map[uint64]VoiceEventFunc
	nextVoiceSubID    uint64
}

type sessionState struct {
	cancel      context.CancelFunc
	done        <-chan struct{}
	phase       sessionPhase
	client      *vowifi.Client
	ussd        *vowifi.Session
	calls       map[string]*voiceCallState
	pendingDial *pendingVoiceDial
	modemPath   dbus.ObjectPath
	profileID   string
	connected   bool
	connectedAt time.Time
	websheet    *websheet.Session
}

type sessionPhase string

const (
	sessionPhaseConnecting       sessionPhase = "connecting"
	sessionPhaseConnected        sessionPhase = "connected"
	sessionPhaseWebsheetRequired sessionPhase = "websheet_required"
	sessionPhaseDisconnected     sessionPhase = "disconnected"
)

func New(cfg Config) Coordinator {
	return &coordinator{
		settings:          NewSettingsStore(cfg.Store),
		store:             cfg.Store,
		onIncoming:        cfg.OnIncoming,
		websheets:         cfg.Websheets,
		sessions:          make(map[string]*sessionState),
		smsReports:        make(map[smsReportKey]*smsReportTracker),
		pendingSMSReports: make(map[smsReportKey]pendingSMSReport),
		voiceSubscribers:  make(map[uint64]VoiceEventFunc),
	}
}

func (c *coordinator) Run(ctx context.Context, registry *mmodem.Registry) error {
	if err := c.startEnabled(ctx, registry); err != nil {
		slog.Warn("start configured Wi-Fi Calling profiles", "error", err)
	}
	unsubscribe, err := registry.Subscribe(func(event mmodem.ModemEvent) error {
		switch event.Type {
		case mmodem.ModemEventAdded:
			if event.Modem != nil {
				c.startIfEnabled(ctx, event.Modem)
			}
		case mmodem.ModemEventRemoved:
			c.stopByPath(event.Path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe modem registry: %w", err)
	}
	defer unsubscribe()
	<-ctx.Done()
	c.stopAll()
	return nil
}

func (c *coordinator) Settings(ctx context.Context, modem *mmodem.Modem) (Settings, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return Settings{}, err
	}
	return c.settings.Get(ctx, profileID)
}

func (c *coordinator) UpdateSettings(ctx context.Context, modem *mmodem.Modem, settings Settings) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	if err := c.settings.Put(ctx, profileID, settings); err != nil {
		return err
	}
	c.stop(modem.EquipmentIdentifier)
	if settings.Enabled {
		c.start(modem, profileID)
	}
	return nil
}

func (c *coordinator) Status(ctx context.Context, modem *mmodem.Modem) (Status, error) {
	settings, err := c.Settings(ctx, modem)
	if err != nil {
		return Status{}, err
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return Status{}, err
	}
	c.mu.Lock()
	session := c.sessions[modem.EquipmentIdentifier]
	status := statusFromSession(settings, session, profileID, time.Now())
	c.mu.Unlock()
	return status, nil
}

func statusFromSession(settings Settings, session *sessionState, profileID string, now time.Time) Status {
	status := Status{
		Settings: settings,
		State:    StateIdle,
	}
	if session == nil || session.profileID != profileID {
		if settings.Enabled {
			status.State = StateDisconnected
		}
		return status
	}
	switch session.phase {
	case sessionPhaseConnected:
		status.Connected = session.client != nil
		if status.Connected {
			status.State = StateConnected
			if !session.connectedAt.IsZero() {
				status.DurationSeconds = max(0, int64(now.Sub(session.connectedAt).Seconds()))
			}
			return status
		}
		status.State = StateDisconnected
	case sessionPhaseWebsheetRequired:
		status.State = StateWebsheetRequired
		if session.websheet != nil {
			info := session.websheet.Info()
			status.Websheet = &info
		}
	case sessionPhaseDisconnected:
		status.State = StateDisconnected
	default:
		status.State = StateConnecting
	}
	return status
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
