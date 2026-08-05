//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	imsgo "github.com/damonto/ims-go"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/pro/websheet"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type coordinatorConfig struct {
	Store              *storage.Store
	Registry           *mmodem.Registry
	OnIncoming         IncomingSMSFunc
	Websheets          *websheet.Broker
	Access             Access
	Internet           *pinternet.Connector
	RegistrationGroups *RegistrationGroups
}

type volteSettingsPersistence interface {
	Get(context.Context, string) (VoLTESettings, error)
	Put(context.Context, string, VoLTESettings) error
	SuspendedInternet(context.Context, string) (pinternet.Preferences, bool, error)
	PutSuspendedInternet(context.Context, string, pinternet.Preferences) error
	DeleteSuspendedInternet(context.Context, string) error
}

type coordinator struct {
	wifiCallingSettings *wifiCallingSettingsStore
	volteSettings       volteSettingsPersistence
	store               *storage.Store
	onIncoming          IncomingSMSFunc
	websheets           *websheet.Broker
	access              Access
	internet            internetRestorer
	registry            *mmodem.Registry
	registrationGroups  *RegistrationGroups

	mu               sync.Mutex
	sessions         map[string]*sessionState
	nextSessionID    uint64
	smsSubmissions   map[smsSubmissionKey]*smsSubmissionTracker
	voiceSubscribers map[uint64]VoiceEventFunc
	nextVoiceSubID   uint64
}

type sessionState struct {
	id          uint64
	modem       *mmodem.Modem
	cancel      context.CancelFunc
	done        <-chan struct{}
	reconnect   chan struct{}
	phase       sessionPhase
	client      *imsgo.Client
	ussd        *imsgo.USSDSession
	calls       map[string]*voiceCallState
	pendingDial *pendingVoiceDial
	deviceKey   string
	generation  uint64
	profileID   string
	connected   bool
	connectedAt time.Time
	websheet    *websheet.Session
}

type sessionPhase string

const (
	sessionPhaseConnecting       sessionPhase = "connecting"
	sessionPhaseWaitingForUplink sessionPhase = "waiting_for_uplink"
	sessionPhaseConnected        sessionPhase = "connected"
	sessionPhaseWebsheetRequired sessionPhase = "websheet_required"
	sessionPhaseDisconnected     sessionPhase = "disconnected"
)

func newCoordinator(cfg coordinatorConfig) *coordinator {
	var internet internetRestorer
	if cfg.Internet != nil {
		internet = cfg.Internet
	}
	return &coordinator{
		wifiCallingSettings: newWiFiCallingSettingsStore(cfg.Store),
		volteSettings:       newVoLTESettingsStore(cfg.Store),
		store:               cfg.Store,
		registry:            cfg.Registry,
		onIncoming:          cfg.OnIncoming,
		websheets:           cfg.Websheets,
		access:              cfg.Access,
		internet:            internet,
		registrationGroups:  cfg.RegistrationGroups,
		sessions:            make(map[string]*sessionState),
		smsSubmissions:      make(map[smsSubmissionKey]*smsSubmissionTracker),
		voiceSubscribers:    make(map[uint64]VoiceEventFunc),
	}
}

func (c *coordinator) volteStore() volteSettingsPersistence {
	if c.volteSettings == nil {
		return (*voLTESettingsStore)(nil)
	}
	return c.volteSettings
}

func (c *coordinator) Run(ctx context.Context, registry *mmodem.Registry) (runErr error) {
	if c.access == AccessVoLTE {
		ownership, err := acquireIMSPolicyRoutingOwnership(imsPolicyRoutingOwnershipAddress)
		if err != nil {
			return err
		}
		defer func() {
			runErr = errors.Join(runErr, ownership.Close())
		}()
		if err := cleanupStaleIMSPolicyRouting(systemPDNLinks{}); err != nil {
			return fmt.Errorf("clean stale VoLTE policy routing: %w", err)
		}
	}
	unsubscribe, err := registry.Subscribe(func(event mmodem.ModemEvent) error {
		c.processModemEvent(ctx, event)
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe modem registry: %w", err)
	}
	defer unsubscribe()
	if err := c.startEnabled(ctx, registry); err != nil {
		slog.Warn("start configured IMS access", "access", c.routeName(), "error", err)
	}
	<-ctx.Done()
	modems := c.stopAll()
	if err := c.releaseManagedVoLTEOnShutdown(ctx, modems); err != nil {
		slog.Warn("restore modem VoLTE on shutdown", "error", err)
	}
	return nil
}

func (c *coordinator) processModemEvent(ctx context.Context, event mmodem.ModemEvent) {
	switch event.Type {
	case mmodem.ModemEventAdded:
		if event.Modem != nil {
			c.startIfEnabled(ctx, event.Modem)
		}
	case mmodem.ModemEventChanged:
		if event.Previous != nil {
			if c.internet != nil {
				if err := c.internet.InvalidateModem(ctx, event.Previous); err != nil {
					slog.Warn("invalidate Internet after modem replacement", "imei", event.Previous.EquipmentIdentifier, "generation", event.Previous.Generation(), "error", err)
				}
			}
			previousPath := event.PreviousPath
			if previousPath == "" {
				previousPath = event.Previous.Path()
			}
			c.stopByDevice(previousPath, event.Previous.Generation())
		}
		if event.Modem != nil {
			c.startIfEnabled(ctx, event.Modem)
		}
	case mmodem.ModemEventRemoved:
		c.stopByDevice(event.Path, event.Generation)
		if c.internet != nil && event.Modem != nil {
			if err := c.internet.InvalidateModem(ctx, event.Modem); err != nil {
				slog.Warn("invalidate Internet after modem removal", "imei", event.Modem.EquipmentIdentifier, "generation", event.Generation, "error", err)
			}
		}
	case mmodem.ModemEventSIMChanged:
		if event.Modem == nil {
			return
		}
		if c.internet != nil {
			if err := c.internet.InvalidateModem(ctx, event.Modem); err != nil {
				slog.Warn("invalidate Internet after SIM profile change", "imei", event.Modem.EquipmentIdentifier, "generation", event.Generation, "error", err)
			}
		}
		c.stop(event.Modem.EquipmentIdentifier)
		c.startIfEnabled(ctx, event.Modem)
	}
}

func (c *coordinator) releaseManagedVoLTEOnShutdown(ctx context.Context, modems []*mmodem.Modem) error {
	if c.access != AccessVoLTE {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	var result error
	for _, modem := range modems {
		if modem == nil {
			continue
		}
		if err := releaseManagedVoLTE(cleanupCtx, modem, c.internet); err != nil {
			result = errors.Join(result, fmt.Errorf("restore modem %s VoLTE: %w", modem.EquipmentIdentifier, err))
		}
		settings, err := c.VoLTESettings(cleanupCtx, modem)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("read modem %s VoLTE data path: %w", modem.EquipmentIdentifier, err))
			continue
		}
		switch settings.DataPath {
		case DataPathMBIM:
			continue
		case DataPathQMAP:
			if c.internet != nil {
				if err := c.internet.SetQMAPEnabled(cleanupCtx, modem, false); err != nil {
					result = errors.Join(result, fmt.Errorf("restore modem %s normal Internet bearer: %w", modem.EquipmentIdentifier, err))
				}
			}
		case DataPathLegacyBAMDMUX:
			if err := c.restoreLegacyInternet(cleanupCtx, modem); err != nil {
				result = errors.Join(result, err)
			}
		case DataPathQualcomm410:
			if c.internet != nil {
				if err := c.internet.SetQualcomm410Enabled(cleanupCtx, modem, false); err != nil {
					result = errors.Join(result, fmt.Errorf("restore modem %s Qualcomm 410 Internet: %w", modem.EquipmentIdentifier, err))
				}
			}
		default:
			result = errors.Join(result, fmt.Errorf("modem %s has unsupported VoLTE data path %q", modem.EquipmentIdentifier, settings.DataPath))
		}
	}
	return result
}

func (c *coordinator) WiFiCallingSettings(ctx context.Context, modem *mmodem.Modem) (WiFiCallingSettings, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return WiFiCallingSettings{}, err
	}
	settings, err := c.wifiCallingSettings.Get(ctx, profileID)
	if err != nil {
		return WiFiCallingSettings{}, err
	}
	return ResolveWiFiCallingSettings(modem, settings)
}

func (c *coordinator) VoLTESettings(ctx context.Context, modem *mmodem.Modem) (VoLTESettings, error) {
	settings, err := c.volteStore().Get(ctx, modem.EquipmentIdentifier)
	if err != nil {
		return VoLTESettings{}, err
	}
	port, err := voLTEPort(modem)
	if err != nil {
		return VoLTESettings{}, err
	}
	switch port.PortType {
	case wwanmodem.PortMBIM:
		settings.DataPath = DataPathMBIM
	case wwanmodem.PortQMI:
		if settings.DataPath == DataPathMBIM {
			settings.DataPath = DataPathQMAP
		}
	}
	return settings, nil
}

func (c *coordinator) UpdateVoLTESettings(ctx context.Context, modem *mmodem.Modem, settings VoLTESettings) error {
	port, err := voLTEPort(modem)
	if err != nil {
		return err
	}
	switch port.PortType {
	case wwanmodem.PortMBIM:
		settings.DataPath = DataPathMBIM
	case wwanmodem.PortQMI:
		switch settings.DataPath {
		case DataPathQMAP, DataPathLegacyBAMDMUX, DataPathQualcomm410:
		default:
			return fmt.Errorf("unsupported QMI VoLTE data path %q", settings.DataPath)
		}
	default:
		return ErrUnavailable
	}
	current, err := c.VoLTESettings(ctx, modem)
	if err != nil {
		return err
	}
	if !settings.Enabled && !current.Enabled {
		return c.updateDisabledVoLTEDataPath(ctx, modem, current, settings)
	}
	if settings.Enabled {
		profileID, err := modem.ProfileID(ctx)
		if err != nil {
			return err
		}
		recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancelRecovery()
		switching := current.Enabled && current.DataPath != settings.DataPath
		rollbackCurrent := func() error {
			if !switching {
				return nil
			}
			if err := c.configureVoLTEDataPath(recoveryCtx, modem, current.DataPath); err != nil {
				return err
			}
			c.restart(modem, profileID)
			return nil
		}
		if switching {
			c.stop(modem.EquipmentIdentifier)
			if err := c.restoreVoLTEDataPath(ctx, modem, current.DataPath); err != nil {
				return errors.Join(err, rollbackCurrent())
			}
		}
		configuredNewPath := !current.Enabled || switching
		if err := c.configureVoLTEDataPath(ctx, modem, settings.DataPath); err != nil {
			var cleanupErr error
			if configuredNewPath {
				cleanupErr = c.restoreVoLTEDataPath(recoveryCtx, modem, settings.DataPath)
			}
			return errors.Join(err, cleanupErr, rollbackCurrent())
		}
		if err := c.volteStore().Put(ctx, modem.EquipmentIdentifier, settings); err != nil {
			var cleanupErr error
			if configuredNewPath {
				cleanupErr = c.restoreVoLTEDataPath(recoveryCtx, modem, settings.DataPath)
			}
			return errors.Join(err, cleanupErr, rollbackCurrent())
		}
		c.restart(modem, profileID)
		return nil
	}
	// The managed client must be fully closed before restoring the modem's
	// internal IMS client, otherwise both clients can contend for IMS state.
	c.stop(modem.EquipmentIdentifier)
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancelCleanup()
	result := error(nil)
	if err := releaseManagedVoLTE(cleanupCtx, modem, c.internet); err != nil {
		result = errors.Join(result, fmt.Errorf("restore modem VoLTE: %w", err))
	}
	// DataPath is only an activation preference while VoLTE is enabled. Once
	// VoLTE is disabled, always release the previously active special path;
	// the next saved DataPath must not change Internet routing by itself.
	result = errors.Join(result, c.restoreVoLTEDataPath(cleanupCtx, modem, current.DataPath))
	result = errors.Join(result, c.volteStore().Put(cleanupCtx, modem.EquipmentIdentifier, settings))
	return result
}

func (c *coordinator) UpdateWiFiCallingSettings(ctx context.Context, modem *mmodem.Modem, settings WiFiCallingSettings) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	settings, err = ResolveWiFiCallingSettings(modem, settings)
	if err != nil {
		return err
	}
	if err := c.wifiCallingSettings.Put(ctx, profileID, settings); err != nil {
		return err
	}
	if settings.Enabled {
		c.restart(modem, profileID)
	} else {
		c.stopAsync(modem.EquipmentIdentifier)
	}
	return nil
}

func (c *coordinator) updateDisabledVoLTEDataPath(ctx context.Context, modem *mmodem.Modem, current, next VoLTESettings) error {
	// A disabled setting may retain Qualcomm 410 as the preferred path for a
	// future enable, but that preference must not affect the current Internet
	// bearer. This cleanup also handles state left by older builds.
	if current.DataPath == DataPathQualcomm410 {
		if err := c.restoreVoLTEDataPath(ctx, modem, current.DataPath); err != nil {
			return err
		}
	}
	return c.volteStore().Put(ctx, modem.EquipmentIdentifier, next)
}

func (c *coordinator) configureVoLTEDataPath(ctx context.Context, modem *mmodem.Modem, dataPath DataPath) error {
	switch dataPath {
	case DataPathMBIM:
		return nil
	case DataPathQMAP:
		if c.internet == nil {
			return nil
		}
		if err := c.internet.SetQMAPEnabled(ctx, modem, true); err != nil {
			return fmt.Errorf("enable QMAP Internet for VoLTE: %w", err)
		}
	case DataPathLegacyBAMDMUX:
		if c.internet == nil {
			return nil
		}
		if err := c.internet.SetQMAPEnabled(ctx, modem, false); err != nil {
			return fmt.Errorf("restore non-QMAP data format for legacy BAM-DMUX: %w", err)
		}
	case DataPathQualcomm410:
		if c.internet == nil {
			return nil
		}
		if err := c.internet.SetQualcomm410Enabled(ctx, modem, true); err != nil {
			return fmt.Errorf("enable Qualcomm 410 Internet: %w", err)
		}
	default:
		return fmt.Errorf("unsupported VoLTE data path %q", dataPath)
	}
	return nil
}

func (c *coordinator) restoreVoLTEDataPath(ctx context.Context, modem *mmodem.Modem, dataPath DataPath) error {
	switch dataPath {
	case DataPathMBIM:
		return nil
	case DataPathQMAP:
		if c.internet != nil {
			if err := c.internet.SetQMAPEnabled(ctx, modem, false); err != nil {
				return fmt.Errorf("restore normal Internet bearer: %w", err)
			}
		}
	case DataPathLegacyBAMDMUX:
		return c.restoreLegacyInternet(ctx, modem)
	case DataPathQualcomm410:
		if c.internet != nil {
			if err := c.internet.SetQualcomm410Enabled(ctx, modem, false); err != nil {
				return fmt.Errorf("restore Qualcomm 410 Internet: %w", err)
			}
		}
	default:
		return fmt.Errorf("unsupported VoLTE data path %q", dataPath)
	}
	return nil
}

func (c *coordinator) restoreLegacyInternet(ctx context.Context, modem *mmodem.Modem) error {
	if c.internet == nil || modem == nil {
		return nil
	}
	prefs, ok, err := c.volteStore().SuspendedInternet(ctx, modem.EquipmentIdentifier)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := restoreInternet(ctx, modem, c.internet, prefs); err != nil {
		return fmt.Errorf("restore modem %s Internet after legacy BAM-DMUX VoLTE: %w", modem.EquipmentIdentifier, err)
	}
	return c.volteStore().DeleteSuspendedInternet(ctx, modem.EquipmentIdentifier)
}

func (c *coordinator) ReconnectWiFiCalling(ctx context.Context, modem *mmodem.Modem) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	settings, err := c.WiFiCallingSettings(ctx, modem)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return ErrNotConnected
	}
	c.restart(modem, profileID)
	return nil
}

func (c *coordinator) Disconnect(_ context.Context, modem *mmodem.Modem) error {
	if modem == nil || modem.EquipmentIdentifier == "" {
		return nil
	}
	c.stopAsync(modem.EquipmentIdentifier)
	return nil
}

type sessionStatus struct {
	Connected       bool
	State           string
	DurationSeconds int64
	Websheet        *websheet.Info
}

func (c *coordinator) SessionStatus(ctx context.Context, modem *mmodem.Modem, enabled bool) (sessionStatus, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return sessionStatus{}, err
	}
	c.mu.Lock()
	session := c.sessions[modem.EquipmentIdentifier]
	status := statusFromSession(enabled, session, profileID, time.Now())
	c.mu.Unlock()
	return status, nil
}

func statusFromSession(enabled bool, session *sessionState, profileID string, now time.Time) sessionStatus {
	status := sessionStatus{State: StateIdle}
	if session == nil || session.profileID != profileID {
		if enabled {
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
	case sessionPhaseWaitingForUplink:
		status.State = StateWaitingForUplink
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
