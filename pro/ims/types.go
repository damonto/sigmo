//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/pro/websheet"
)

const (
	WiFiCallingFeatureName = "wifiCalling"
	VoLTEFeatureName       = "volte"

	scopePrefix               = "profile:"
	keyEnabled                = "wifi_calling.enabled"
	keyPreferred              = "wifi_calling.preferred"
	modemScopePrefix          = "modem:"
	keyVoLTESettings          = "volte.settings"
	keyVoLTESuspendedInternet = "volte.suspended_internet"
	actionUSSDInitialize      = "initialize"
	actionUSSDReply           = "reply"

	StateIdle             = "idle"
	StateConnecting       = "connecting"
	StateConnected        = "connected"
	StateWebsheetRequired = "websheet_required"
	StateDisconnected     = "disconnected"
)

type Access string

const (
	AccessWiFiCalling Access = "wifi_calling"
	AccessVoLTE       Access = "volte"
)

type DataPath string

const (
	DataPathMBIM          DataPath = "mbim"
	DataPathQMAP          DataPath = "qmap"
	DataPathLegacyBAMDMUX DataPath = "legacy_bam_dmux"
)

var (
	ErrUnavailable             = errors.New("ims access is unavailable")
	ErrNotConnected            = errors.New("ims access is not connected")
	ErrWiFiCallingSetupPending = errors.New("wifi calling setup is pending")
	ErrWiFiCallingSetupDenied  = errors.New("wifi calling setup denied")
	ErrUnsupportedCodec        = errors.New("ims voice codec is not supported")
	ErrUnsupportedDTMF         = errors.New("ims dtmf is not supported")
	ErrCallOnHold              = errors.New("ims call is on hold")
	ErrWebsheetNotPending      = errors.New("wifi calling websheet is not pending")
	ErrWebsheetDismissed       = errors.New("wifi calling websheet was dismissed")
	ErrWebsheetUnavailable     = errors.New("wifi calling websheet is unavailable")
)

type Settings struct {
	Enabled            bool
	Preferred          bool
	DataPath           DataPath
	SetIMSAPNAsDefault bool
	EnablePCSCFViaPCO  bool
}

type Status struct {
	Settings
	Connected       bool
	State           string
	DurationSeconds int64
	Websheet        *websheet.Info
}

type IncomingSMS struct {
	ModemID string
	Message storage.Message
}

type IncomingSMSFunc func(context.Context, IncomingSMS) error

type VoiceCall struct {
	ID         string
	Route      string
	ModemID    string
	ProfileID  string
	Direction  string
	Number     string
	State      string
	Hold       string
	Reason     string
	StartedAt  time.Time
	AnsweredAt time.Time
	EndedAt    time.Time
	UpdatedAt  time.Time
}

type VoiceEvent struct {
	Call VoiceCall
}

type VoiceEventFunc func(VoiceEvent)

type MediaInfo struct {
	Codec           string
	PayloadType     int
	ClockRate       int
	Channels        int
	OctetAlign      bool
	HFOnly          bool
	DTMFPayloadType int
	DTMFClockRate   int
	PTimeMillis     int
}

type MediaSession interface {
	Info() MediaInfo
	ReadPacket(context.Context) ([]byte, error)
	WritePacket(context.Context, []byte) error
}

type Coordinator interface {
	Run(context.Context, *mmodem.Registry) error
	Settings(context.Context, *mmodem.Modem) (Settings, error)
	UpdateSettings(context.Context, *mmodem.Modem, Settings) error
	Reconnect(context.Context, *mmodem.Modem) error
	Disconnect(context.Context, *mmodem.Modem) error
	Status(context.Context, *mmodem.Modem) (Status, error)
	EmergencyAddressUpdateAvailable(context.Context, *mmodem.Modem) bool
	StartWebsheet(context.Context, *mmodem.Modem) (websheet.Info, error)
	StartEmergencyAddressUpdate(context.Context, *mmodem.Modem) (websheet.Info, error)
	SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error)
	ApplyPendingSMSStatus(context.Context, storage.Message) error
	ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error)
	DialCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	AnswerCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	RejectCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	HangupCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	HoldCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	ResumeCall(context.Context, *mmodem.Modem, string) (VoiceCall, error)
	SendCallDTMF(context.Context, *mmodem.Modem, string, string) error
	OpenCallMedia(context.Context, *mmodem.Modem, string) (MediaSession, error)
	SubscribeVoiceEvents(VoiceEventFunc) func()
}

type SettingsStore struct {
	store *storage.Store
}

type VoLTESettingsStore struct {
	store *storage.Store
}

func NewVoLTESettingsStore(store *storage.Store) *VoLTESettingsStore {
	return &VoLTESettingsStore{store: store}
}

func (s *VoLTESettingsStore) Get(ctx context.Context, modemID string) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{DataPath: DataPathQMAP}, nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return Settings{}, err
	}
	settings := Settings{DataPath: DataPathQMAP}
	if err := s.store.Get(ctx, scope, keyVoLTESettings, &settings); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return settings, nil
		}
		return Settings{}, fmt.Errorf("read VoLTE settings: %w", err)
	}
	return settings, nil
}

func (s *VoLTESettingsStore) Put(ctx context.Context, modemID string, settings Settings) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return err
	}
	settings.Preferred = false
	if err := s.store.Put(ctx, scope, keyVoLTESettings, settings); err != nil {
		return fmt.Errorf("save VoLTE settings: %w", err)
	}
	return nil
}

func (s *VoLTESettingsStore) SuspendedInternet(ctx context.Context, modemID string) (pinternet.Preferences, bool, error) {
	if s == nil || s.store == nil {
		return pinternet.Preferences{}, false, nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return pinternet.Preferences{}, false, err
	}
	var prefs pinternet.Preferences
	if err := s.store.Get(ctx, scope, keyVoLTESuspendedInternet, &prefs); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return pinternet.Preferences{}, false, nil
		}
		return pinternet.Preferences{}, false, fmt.Errorf("read suspended Internet: %w", err)
	}
	return prefs, true, nil
}

func (s *VoLTESettingsStore) PutSuspendedInternet(ctx context.Context, modemID string, prefs pinternet.Preferences) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return err
	}
	if err := s.store.Put(ctx, scope, keyVoLTESuspendedInternet, prefs); err != nil {
		return fmt.Errorf("save suspended Internet: %w", err)
	}
	return nil
}

func (s *VoLTESettingsStore) DeleteSuspendedInternet(ctx context.Context, modemID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, scope, keyVoLTESuspendedInternet); err != nil {
		return fmt.Errorf("delete suspended Internet: %w", err)
	}
	return nil
}

func NewSettingsStore(store *storage.Store) *SettingsStore {
	return &SettingsStore{store: store}
}

func (s *SettingsStore) Get(ctx context.Context, profileID string) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return Settings{}, err
	}
	var settings Settings
	if err := s.store.Get(ctx, scope, keyEnabled, &settings.Enabled); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Settings{}, fmt.Errorf("read wifi calling enabled: %w", err)
	}
	if err := s.store.Get(ctx, scope, keyPreferred, &settings.Preferred); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Settings{}, fmt.Errorf("read wifi calling preference: %w", err)
	}
	return settings, nil
}

func (s *SettingsStore) Put(ctx context.Context, profileID string, settings Settings) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		settings.Preferred = false
	}
	if err := s.store.Put(ctx, scope, keyEnabled, settings.Enabled); err != nil {
		return fmt.Errorf("save wifi calling enabled: %w", err)
	}
	if err := s.store.Put(ctx, scope, keyPreferred, settings.Preferred); err != nil {
		return fmt.Errorf("save wifi calling preference: %w", err)
	}
	return nil
}

func profileScope(profileID string) (string, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "", mmodem.ErrProfileIDMissing
	}
	return scopePrefix + profileID, nil
}

func modemScope(modemID string) (string, error) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return "", errors.New("modem identifier is required")
	}
	return modemScopePrefix + modemID, nil
}
