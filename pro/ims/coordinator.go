//go:build ims

package ims

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	imsgo "github.com/damonto/ims-go"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/pro/websheet"
	"github.com/godbus/dbus/v5"
)

type Config struct {
	Store              *storage.Store
	OnIncoming         IncomingSMSFunc
	Websheets          *websheet.Broker
	Access             Access
	NetworkPreferences *mmodem.NetworkPreferences
	Internet           *pinternet.Connector
}

type coordinator struct {
	settings           *SettingsStore
	store              *storage.Store
	onIncoming         IncomingSMSFunc
	websheets          *websheet.Broker
	access             Access
	networkPreferences *mmodem.NetworkPreferences
	internet           internetRestorer
	voltePreferenceMu  sync.Mutex

	mu               sync.Mutex
	sessions         map[string]*sessionState
	nextSessionID    uint64
	smsSubmissions   map[smsSubmissionKey]*smsSubmissionTracker
	voiceSubscribers map[uint64]VoiceEventFunc
	nextVoiceSubID   uint64
}

type sessionState struct {
	id          uint64
	cancel      context.CancelFunc
	done        <-chan struct{}
	reconnect   chan struct{}
	phase       sessionPhase
	client      *imsgo.Client
	ussd        *imsgo.USSDSession
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
	access := cfg.Access
	if access == "" {
		access = AccessWiFiCalling
	}
	return &coordinator{
		settings:           NewSettingsStore(cfg.Store),
		store:              cfg.Store,
		onIncoming:         cfg.OnIncoming,
		websheets:          cfg.Websheets,
		access:             access,
		networkPreferences: cfg.NetworkPreferences,
		internet:           cfg.Internet,
		sessions:           make(map[string]*sessionState),
		smsSubmissions:     make(map[smsSubmissionKey]*smsSubmissionTracker),
		voiceSubscribers:   make(map[uint64]VoiceEventFunc),
	}
}

func (c *coordinator) Run(ctx context.Context, registry *mmodem.Registry) error {
	if err := c.startEnabled(ctx, registry); err != nil {
		slog.Warn("start configured IMS access", "access", c.routeName(), "error", err)
	}
	unsubscribeVoLTE := c.subscribeVoLTEPreferences(ctx, registry)
	defer unsubscribeVoLTE()
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
	if c.access == AccessVoLTE {
		if c.networkPreferences == nil {
			return Settings{}, nil
		}
		enabled, _, err := c.networkPreferences.SavedVoLTE(ctx, modem.EquipmentIdentifier)
		if err != nil {
			return Settings{}, fmt.Errorf("read volte preference: %w", err)
		}
		return Settings{Enabled: enabled}, nil
	}
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
	if c.access == AccessVoLTE {
		if c.networkPreferences == nil {
			return ErrUnavailable
		}
		if err := c.networkPreferences.SaveVoLTE(ctx, modem.EquipmentIdentifier, settings.Enabled); err != nil {
			return err
		}
	} else {
		if err := c.settings.Put(ctx, profileID, settings); err != nil {
			return err
		}
	}
	if settings.Enabled {
		c.restart(modem, profileID)
	} else {
		c.stopAsync(modem.EquipmentIdentifier)
	}
	return nil
}

func (c *coordinator) Reconnect(ctx context.Context, modem *mmodem.Modem) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	settings, err := c.Settings(ctx, modem)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return ErrNotConnected
	}
	c.restart(modem, profileID)
	return nil
}

func (c *coordinator) subscribeVoLTEPreferences(ctx context.Context, registry *mmodem.Registry) func() {
	if c.access != AccessVoLTE || c.networkPreferences == nil {
		return func() {}
	}
	return c.networkPreferences.SubscribeVoLTE(func(event mmodem.VoLTEPreferenceEvent) {
		c.applyVoLTEPreference(ctx, registry, event)
	})
}

func (c *coordinator) applyVoLTEPreference(ctx context.Context, registry *mmodem.Registry, event mmodem.VoLTEPreferenceEvent) {
	if event.ModemID == "" {
		return
	}
	c.voltePreferenceMu.Lock()
	defer c.voltePreferenceMu.Unlock()

	enabled, _, err := c.networkPreferences.SavedVoLTE(ctx, event.ModemID)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("read VoLTE preference", "imei", event.ModemID, "error", err)
		}
		return
	}
	if !enabled {
		// The managed IMS client must be fully closed before the modem's client
		// is restored, otherwise both clients can contend for the same IMS state.
		c.stop(event.ModemID)
		if registry == nil {
			return
		}
		modem, err := registry.Find(ctx, event.ModemID)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("find modem for VoLTE release", "imei", event.ModemID, "error", err)
			}
			return
		}
		if err := releaseManagedVoLTE(ctx, modem, c.internet); err != nil && ctx.Err() == nil {
			slog.Warn("restore modem VoLTE", "imei", event.ModemID, "error", err)
		}
		return
	}
	modem, err := registry.Find(ctx, event.ModemID)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("find modem for VoLTE preference", "imei", event.ModemID, "error", err)
		}
		return
	}
	c.startIfEnabled(ctx, modem)
}

func (c *coordinator) Disconnect(_ context.Context, modem *mmodem.Modem) error {
	if modem == nil || modem.EquipmentIdentifier == "" {
		return nil
	}
	c.stopAsync(modem.EquipmentIdentifier)
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

func (c *coordinator) routeName() string {
	if c.access == AccessVoLTE {
		return string(AccessVoLTE)
	}
	return string(AccessWiFiCalling)
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
