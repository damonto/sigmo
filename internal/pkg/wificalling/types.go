package wificalling

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	FeatureName = "wifiCalling"

	scopePrefix          = "profile:"
	keyEnabled           = "wifi_calling.enabled"
	keyPreferred         = "wifi_calling.preferred"
	actionUSSDInitialize = "initialize"
	actionUSSDReply      = "reply"

	StateIdle             = "idle"
	StateConnecting       = "connecting"
	StateConnected        = "connected"
	StateWebsheetRequired = "websheet_required"
	StateDisconnected     = "disconnected"
)

var (
	ErrUnavailable         = errors.New("wifi calling is unavailable")
	ErrNotConnected        = errors.New("wifi calling is not connected")
	ErrEntitlementPending  = errors.New("wifi calling entitlement is pending")
	ErrEntitlementDenied   = errors.New("wifi calling entitlement denied")
	ErrUnsupportedCodec    = errors.New("wifi calling voice codec is not supported")
	ErrUnsupportedDTMF     = errors.New("wifi calling dtmf is not supported")
	ErrCallOnHold          = errors.New("wifi calling call is on hold")
	ErrWebsheetNotPending  = errors.New("wifi calling websheet is not pending")
	ErrWebsheetDismissed   = errors.New("wifi calling websheet was dismissed")
	ErrWebsheetUnavailable = errors.New("wifi calling websheet is unavailable")
)

type Settings struct {
	Enabled   bool
	Preferred bool
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
