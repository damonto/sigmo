package ims

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	imsvoice "github.com/damonto/ims-go/ims/voice"
	"github.com/damonto/sigmo/internal/app/websheet"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	scopePrefix               = "profile:"
	keyWiFiCallingSettings    = "wifi_calling.settings"
	modemScopePrefix          = "modem:"
	keyVoLTESettings          = "volte.settings"
	keyVoLTESuspendedInternet = "volte.suspended_internet"
	actionUSSDInitialize      = "initialize"
	actionUSSDReply           = "reply"

	StateIdle             = "idle"
	StateConnecting       = "connecting"
	StateWaitingForUplink = "waiting_for_uplink"
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

type UnderlayMode string

const (
	DataPathMBIM          DataPath = "mbim"
	DataPathQMAP          DataPath = "qmap"
	DataPathLegacyBAMDMUX DataPath = "legacy_bam_dmux"
	DataPathQualcomm410   DataPath = "qualcomm_410"
)

const (
	UnderlayModeSystem UnderlayMode = "system"
	UnderlayModeSelf   UnderlayMode = "self"
	UnderlayModeModem  UnderlayMode = "modem"
	UnderlayModeSOCKS5 UnderlayMode = "socks5"
)

var (
	ErrUnavailable                    = errors.New("ims access is unavailable")
	ErrNotConnected                   = errors.New("ims access is not connected")
	ErrWiFiCallingSetupPending        = errors.New("wifi calling setup is pending")
	ErrWiFiCallingSetupDenied         = errors.New("wifi calling setup denied")
	ErrUnsupportedCodec               = errors.New("ims voice codec is not supported")
	ErrUnsupportedDTMF                = errors.New("ims dtmf is not supported")
	ErrCallOnHold                     = errors.New("ims call is on hold")
	ErrWebsheetNotPending             = errors.New("wifi calling websheet is not pending")
	ErrWebsheetDismissed              = errors.New("wifi calling websheet was dismissed")
	ErrWebsheetUnavailable            = errors.New("wifi calling websheet is unavailable")
	ErrInvalidWiFiCallingUnderlay     = errors.New("wifi calling underlay is invalid")
	ErrWiFiCallingUnderlayUnavailable = errors.New("wifi calling underlay is unavailable")
	ErrVoLTEAirplaneMode              = errors.New("VoLTE settings cannot change in airplane mode")
	ErrVoLTEDataPathRequired          = errors.New("QMI VoLTE data path is required")
	ErrVoLTEDataPathUnsupported       = errors.New("VoLTE data path is unsupported")
)

type WiFiCallingSettings struct {
	Enabled  bool
	Underlay UnderlaySettings
}

type UnderlaySettings struct {
	Mode           UnderlayMode           `json:"mode" jsonschema:"outer network used by Wi-Fi Calling: system, self, modem, or socks5"`
	ModemID        string                 `json:"modemId,omitempty" jsonschema:"stable modem identifier used when mode is modem"`
	ProxyID        string                 `json:"proxyId,omitempty" jsonschema:"active SOCKS5 proxy or remembered proxy when using a direct network"`
	DirectUnderlay DirectUnderlaySettings `json:"directUnderlay,omitzero" jsonschema:"direct network to restore when turning off SOCKS5"`
}

// DirectUnderlaySettings identifies a Wi-Fi Calling network that can be restored after using a proxy.
type DirectUnderlaySettings struct {
	Mode    UnderlayMode `json:"mode" jsonschema:"direct network: system, self, or modem"`
	ModemID string       `json:"modemId,omitempty" jsonschema:"stable modem identifier used when mode is modem"`
}

func (u UnderlaySettings) withSavedRoute(previous UnderlaySettings) UnderlaySettings {
	if u.Mode != UnderlayModeSOCKS5 {
		if u.ProxyID == "" {
			u.ProxyID = previous.ProxyID
		}
		return u
	}
	if previous.Mode == UnderlayModeSOCKS5 {
		u.DirectUnderlay = previous.DirectUnderlay
	} else {
		u.DirectUnderlay = DirectUnderlaySettings{Mode: previous.Mode, ModemID: previous.ModemID}
	}
	if u.DirectUnderlay.Mode == "" {
		u.DirectUnderlay.Mode = UnderlayModeSystem
	}
	return u
}

func ResolveWiFiCallingSettings(modem *mmodem.Modem, settings WiFiCallingSettings) (WiFiCallingSettings, error) {
	underlay := settings.Underlay
	underlay.Mode = UnderlayMode(strings.ToLower(strings.TrimSpace(string(underlay.Mode))))
	underlay.ProxyID = strings.TrimSpace(underlay.ProxyID)
	if underlay.Mode == UnderlayModeSOCKS5 {
		if underlay.ProxyID == "" {
			return WiFiCallingSettings{}, fmt.Errorf("%w: proxy id is required for socks5 mode", ErrInvalidWiFiCallingUnderlay)
		}
		underlay.ModemID = ""
		if underlay.DirectUnderlay != (DirectUnderlaySettings{}) {
			direct, err := resolveDirectUnderlay(modem, underlay.DirectUnderlay)
			if err != nil {
				return WiFiCallingSettings{}, err
			}
			underlay.DirectUnderlay = direct
		}
	} else {
		direct, err := resolveDirectUnderlay(modem, DirectUnderlaySettings{Mode: underlay.Mode, ModemID: underlay.ModemID})
		if err != nil {
			return WiFiCallingSettings{}, err
		}
		underlay.Mode, underlay.ModemID = direct.Mode, direct.ModemID
		underlay.DirectUnderlay = DirectUnderlaySettings{}
	}
	settings.Underlay = underlay
	return settings, nil
}

func resolveDirectUnderlay(modem *mmodem.Modem, direct DirectUnderlaySettings) (DirectUnderlaySettings, error) {
	mode := UnderlayMode(strings.ToLower(strings.TrimSpace(string(direct.Mode))))
	switch mode {
	case "", UnderlayModeSystem:
		return DirectUnderlaySettings{Mode: UnderlayModeSystem}, nil
	case UnderlayModeSelf:
		return DirectUnderlaySettings{Mode: UnderlayModeSelf}, nil
	case UnderlayModeModem:
		modemID := strings.TrimSpace(direct.ModemID)
		if modemID == "" {
			return DirectUnderlaySettings{}, fmt.Errorf("%w: modem id is required for modem mode", ErrInvalidWiFiCallingUnderlay)
		}
		if modem != nil && modemID == strings.TrimSpace(modem.EquipmentIdentifier) {
			return DirectUnderlaySettings{Mode: UnderlayModeSelf}, nil
		}
		return DirectUnderlaySettings{Mode: mode, ModemID: modemID}, nil
	default:
		return DirectUnderlaySettings{}, fmt.Errorf("%w: unsupported direct network mode %q", ErrInvalidWiFiCallingUnderlay, mode)
	}
}

type VoLTESettings struct {
	Enabled  bool
	DataPath DataPath
}

type WiFiCallingStatus struct {
	Progress  *ConnectionProgress
	LastError string
	WiFiCallingSettings
	Connected       bool
	State           string
	DurationSeconds int64
	Websheet        *websheet.Info
}

type VoLTEStatus struct {
	Progress *ConnectionProgress
	VoLTESettings
	Connected       bool
	State           string
	DurationSeconds int64
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

// VoiceDTMF reports one detected remote key, without a release or final duration.
type VoiceDTMF struct {
	Digit string
	At    time.Time
}

// VoiceEvent carries a call snapshot and optionally a transient remote key.
type VoiceEvent struct {
	Call VoiceCall
	DTMF *VoiceDTMF
}

type VoiceEventFunc func(VoiceEvent)

type MediaSession interface {
	Media() imsvoice.NegotiatedMedia
	ReadPacket(context.Context) ([]byte, error)
	WritePacket(context.Context, []byte) error
}

type wifiCallingSettingsStore struct {
	store *storage.Store
}

type wifiCallingSettingsRecord struct {
	Enabled  bool             `json:"enabled"`
	Underlay UnderlaySettings `json:"underlay"`
}

type voLTESettingsStore struct {
	store *storage.Store
}

func newVoLTESettingsStore(store *storage.Store) *voLTESettingsStore {
	return &voLTESettingsStore{store: store}
}

func (s *voLTESettingsStore) Get(ctx context.Context, modemID string) (VoLTESettings, error) {
	if s == nil || s.store == nil {
		return VoLTESettings{DataPath: DataPathQMAP}, nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return VoLTESettings{}, err
	}
	settings := VoLTESettings{DataPath: DataPathQMAP}
	if err := s.store.Get(ctx, scope, keyVoLTESettings, &settings); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return settings, nil
		}
		return VoLTESettings{}, fmt.Errorf("read VoLTE settings: %w", err)
	}
	return settings, nil
}

func (s *voLTESettingsStore) Put(ctx context.Context, modemID string, settings VoLTESettings) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := modemScope(modemID)
	if err != nil {
		return err
	}
	if err := s.store.Put(ctx, scope, keyVoLTESettings, settings); err != nil {
		return fmt.Errorf("save VoLTE settings: %w", err)
	}
	return nil
}

func (s *voLTESettingsStore) SuspendedInternet(ctx context.Context, modemID string) (pinternet.Preferences, bool, error) {
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

func (s *voLTESettingsStore) PutSuspendedInternet(ctx context.Context, modemID string, prefs pinternet.Preferences) error {
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

func (s *voLTESettingsStore) DeleteSuspendedInternet(ctx context.Context, modemID string) error {
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

func newWiFiCallingSettingsStore(store *storage.Store) *wifiCallingSettingsStore {
	return &wifiCallingSettingsStore{store: store}
}

func (s *wifiCallingSettingsStore) Get(ctx context.Context, profileID string) (WiFiCallingSettings, error) {
	if s == nil || s.store == nil {
		return WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeSystem}}, nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return WiFiCallingSettings{}, err
	}
	record := wifiCallingSettingsRecord{Underlay: UnderlaySettings{Mode: UnderlayModeSystem}}
	if err := s.store.Get(ctx, scope, keyWiFiCallingSettings, &record); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return WiFiCallingSettings{Underlay: record.Underlay}, nil
		}
		return WiFiCallingSettings{}, fmt.Errorf("read wifi calling settings: %w", err)
	}
	return ResolveWiFiCallingSettings(nil, WiFiCallingSettings(record))
}

func (s *wifiCallingSettingsStore) Put(ctx context.Context, profileID string, settings WiFiCallingSettings) error {
	if s == nil || s.store == nil {
		return nil
	}
	scope, err := profileScope(profileID)
	if err != nil {
		return err
	}
	settings, err = ResolveWiFiCallingSettings(nil, settings)
	if err != nil {
		return err
	}
	record := wifiCallingSettingsRecord(settings)
	if err := s.store.Put(ctx, scope, keyWiFiCallingSettings, record); err != nil {
		return fmt.Errorf("save wifi calling settings: %w", err)
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
