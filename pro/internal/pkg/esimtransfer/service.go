//go:build esim_transfer

package esimtransfer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/ts43-go"
	"github.com/gorilla/websocket"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/pro/internal/pkg/websheet"
)

const (
	stagePreparing   = "preparing"
	stageCarrier     = "carrier"
	stageWebsheet    = "websheet"
	stageDownloading = "downloading"
	stageEnabling    = "enabling"
	stageCompleting  = "completing"
	stageDeleting    = "deleting"
)

const (
	ts43DeviceVendor          = "Google"
	ts43DeviceModel           = "Pixel 8 Pro"
	ts43DeviceSoftwareVersion = "15/AP3A.240905.015"
)

const (
	scardNoServiceName = "SCARD_E_NO_SERVICE"
	scardNoServiceCode = "0x8010001D"
)

var (
	errWebsheetUnavailable    = errors.New("carrier websheet service is unavailable")
	errCarrierDismissed       = errors.New("carrier dismissed transfer")
	errSourceDeletionDeclined = errors.New("carrier requires source profile deletion")
	errPhysicalSourceDeletion = errors.New("carrier requested physical SIM deletion")
)

type Service struct {
	store         *settings.Store
	registry      *mmodem.Registry
	enableProfile func(context.Context, *mmodem.Modem, sgp22.ICCID) error
	deleteProfile func(context.Context, *mmodem.Modem, sgp22.ICCID) error
	websheets     *websheet.Broker
}

func New(opts Config) *Service {
	return &Service{
		store:         opts.Store,
		registry:      opts.Registry,
		enableProfile: opts.EnableProfile,
		deleteProfile: opts.DeleteProfile,
		websheets:     opts.Websheets,
	}
}

type Start struct {
	SourceType SourceType
	SourceID   string
	ProfileID  string
	SourceIMEI string
}

type profileCandidate struct {
	response ProfileResponse
}

type sourceEndpoint struct {
	channel       ts43.Channel
	release       func()
	device        ts43.Device
	sourceSIMType ts43.SIMType
}

func (s *sourceEndpoint) Close() {
	if s == nil || s.release == nil {
		return
	}
	s.release()
	s.release = nil
}

type activeSession struct {
	settings     *settings.Settings
	source       *sourceEndpoint
	target       *mmodem.Modem
	targetClient *ilpa.LPA
	client       *ts43.Client
	logger       *slog.Logger
	targetClosed bool
}

func (t *activeSession) Close() {
	if t == nil {
		return
	}
	t.CloseTarget()
	t.source.Close()
}

func (t *activeSession) CloseTarget() {
	if t == nil || t.targetClosed || t.targetClient == nil {
		return
	}
	t.targetClosed = true
	if cerr := t.targetClient.Close(); cerr != nil {
		t.Logger().Warn("close target LPA client", "error", cerr)
	}
}

func (t *activeSession) Logger() *slog.Logger {
	return t.logger
}

func (s *Service) Sources(ctx context.Context, target *mmodem.Modem) (SourcesResponse, error) {
	modems, err := s.registry.Modems(ctx)
	if err != nil {
		return SourcesResponse{}, fmt.Errorf("list modems: %w", err)
	}
	currentSettings := s.settingsSnapshot()
	response := make([]SourceResponse, 0, len(modems))
	for _, modem := range modems {
		if modem.EquipmentIdentifier == "" || modem.EquipmentIdentifier == target.EquipmentIdentifier {
			continue
		}
		response = append(response, SourceResponse{
			Type:   SourceModem,
			ID:     modem.EquipmentIdentifier,
			Name:   modemName(currentSettings, modem),
			Detail: modem.EquipmentIdentifier,
		})
	}
	readers, ccidErr := listCCIDReaders()
	for _, reader := range readers {
		response = append(response, SourceResponse{
			Type:               SourceCCID,
			ID:                 reader,
			Name:               reader,
			RequiresSourceIMEI: true,
		})
	}
	out := SourcesResponse{Sources: response}
	if ccidErr != nil {
		out.CCIDError = ccidErr.Error()
	}
	return out, nil
}

func (s *Service) Profiles(ctx context.Context, target *mmodem.Modem, req ProfilesRequest) ([]ProfileResponse, error) {
	req = normalizeProfilesRequest(req)
	if err := validateTarget(target, Start{SourceType: req.SourceType, SourceID: req.SourceID}); err != nil {
		return nil, err
	}
	currentSettings := s.settingsSnapshot()
	return s.profileResponses(ctx, currentSettings, req)
}

func (s *Service) Serve(ctx context.Context, conn *websocket.Conn, target *mmodem.Modem) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session := newSession(conn, cancel)
	startMsg, ok := session.waitForStart(sessionCtx)
	if !ok {
		return nil
	}
	start := Start{
		SourceType: SourceType(strings.TrimSpace(string(startMsg.SourceType))),
		SourceID:   strings.TrimSpace(startMsg.SourceID),
		ProfileID:  strings.TrimSpace(startMsg.ProfileID),
		SourceIMEI: strings.TrimSpace(startMsg.SourceIMEI),
	}
	if err := s.run(sessionCtx, session, target, start); err != nil {
		_ = session.send(serverMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}
	_ = session.send(serverMessage{Type: wsTypeCompleted})
	return nil
}

func (s *Service) settingsSnapshot() *settings.Settings {
	currentSettings := s.store.Snapshot()
	return &currentSettings
}

func (s *Service) run(ctx context.Context, session *session, target *mmodem.Modem, start Start) error {
	if err := validateStart(start); err != nil {
		return err
	}
	if err := validateTarget(target, start); err != nil {
		return err
	}
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stagePreparing})

	active, err := s.prepare(ctx, target, start)
	if err != nil {
		return err
	}
	defer active.Close()

	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageCarrier})
	result, err := active.client.Transfer(ctx)
	if err != nil {
		return fmt.Errorf("start transfer: %w", err)
	}
	for {
		var done bool
		result, done, err = s.handleEvent(ctx, session, active, start, result)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func validateStart(start Start) error {
	if start.SourceType == "" || start.SourceID == "" || start.ProfileID == "" {
		return errors.New("source and profile are required")
	}
	switch start.SourceType {
	case SourceModem:
	case SourceCCID:
		if start.SourceIMEI == "" {
			return ErrSourceIMEIRequired
		}
	default:
		return ErrSourceUnsupported
	}
	return nil
}

func normalizeProfilesRequest(req ProfilesRequest) ProfilesRequest {
	return ProfilesRequest{
		SourceType: SourceType(strings.TrimSpace(string(req.SourceType))),
		SourceID:   strings.TrimSpace(req.SourceID),
		SourceIMEI: strings.TrimSpace(req.SourceIMEI),
	}
}

func validateTarget(target *mmodem.Modem, start Start) error {
	if target == nil || start.SourceType != SourceModem {
		return nil
	}
	if start.SourceID == target.EquipmentIdentifier {
		return ErrSourceIsTarget
	}
	return nil
}

func (s *Service) prepare(ctx context.Context, target *mmodem.Modem, start Start) (*activeSession, error) {
	currentSettings := s.settingsSnapshot()
	candidates, err := s.profileCandidates(ctx, currentSettings, ProfilesRequest{
		SourceType: start.SourceType,
		SourceID:   start.SourceID,
		SourceIMEI: start.SourceIMEI,
	})
	if err != nil {
		return nil, err
	}
	candidate, ok := findCandidate(candidates, start.ProfileID)
	if !ok || !candidate.response.Supported {
		return nil, ErrProfileUnsupported
	}
	if err := s.activateSourceProfile(ctx, currentSettings, start, candidate); err != nil {
		return nil, err
	}

	source, err := s.openSource(ctx, currentSettings, start)
	if err != nil {
		return nil, err
	}
	source.sourceSIMType = ts43SourceSIMType(candidate.response.Type)
	source.device.ICCID = candidate.response.ICCID
	releaseSource := true
	defer func() {
		if releaseSource {
			source.Close()
		}
	}()

	targetClient, err := ilpa.New(target, currentSettings)
	if err != nil {
		return nil, fmt.Errorf("create target LPA client: %w", err)
	}
	releaseTarget := true
	defer func() {
		if releaseTarget {
			if cerr := targetClient.Close(); cerr != nil {
				targetClient.Logger().Warn("close target LPA client", "error", cerr)
			}
		}
	}()

	targetIMEI, err := target.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read target IMEI: %w", err)
	}
	eid, err := targetClient.EID()
	if err != nil {
		return nil, fmt.Errorf("read target EID: %w", err)
	}
	targetDevice := ts43Device(targetIMEI)
	targetDevice.EID = strings.ToUpper(hex.EncodeToString(eid))
	logger := transferLogger(targetIMEI, source.device.IMEI)
	client, err := ts43.New(&ts43.Config{
		Logger:      logger,
		Entitlement: ts43.Entitlement{SourceSIMType: source.sourceSIMType},
		Source:      ts43.Endpoint{Channel: source.channel, Device: source.device},
		Target: ts43.Endpoint{
			Device: targetDevice,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create TS.43 client: %w", err)
	}

	releaseSource = false
	releaseTarget = false
	return &activeSession{
		settings:     currentSettings,
		source:       source,
		target:       target,
		targetClient: targetClient,
		client:       client,
		logger:       logger,
	}, nil
}

func (s *Service) handleEvent(ctx context.Context, session *session, active *activeSession, start Start, result *ts43.Result) (*ts43.Result, bool, error) {
	switch event := result.Event.(type) {
	case ts43.UserInputEvent:
		active.Logger().Info("TS.43 transfer requires user input")
		answer, err := session.userInput(ctx, event)
		if err != nil {
			active.Logger().Warn("TS.43 user input interrupted", "error", err)
			return result, false, err
		}
		next, err := active.client.Continue(ctx, result, ts43.ContinueRequest{UserInput: answer})
		if err != nil {
			active.Logger().Warn("continue TS.43 transfer after user input", "error", err)
		}
		return next, false, err
	case ts43.WebsheetEvent:
		next, err := s.handleWebsheet(ctx, session, active, result, event)
		if err != nil {
			active.Logger().Warn("handle TS.43 websheet", "contentType", event.Websheet.ContentsType, "error", err)
		}
		return next, false, err
	case ts43.SourceProfileDeletionEvent:
		active.Logger().Info("TS.43 transfer requires source profile deletion", "iccid", event.ICCID)
		next, err := s.handleSourceProfileDeletion(ctx, session, active, start, result, event)
		if err != nil {
			active.Logger().Warn("handle TS.43 source profile deletion", "iccid", event.ICCID, "error", err)
		}
		return next, false, err
	case ts43.DownloadReadyEvent:
		active.Logger().Info("TS.43 transfer download is ready", "smdp", event.Config.SMDPFQDN, "profileICCID", event.Config.ProfileICCID)
		next, err := s.downloadEnableAndComplete(ctx, session, active, result, event.Config)
		if err != nil {
			active.Logger().Warn("download TS.43 transferred profile", "error", err)
		}
		return next, false, err
	case ts43.SMDSDiscoveryEvent:
		next, err := s.handleSMDSDiscovery(ctx, session, active, result, event)
		return next, false, err
	case ts43.DelayedDownloadEvent:
		next, err := s.handleSMDSDiscovery(ctx, session, active, result, smdsDiscoveryEventFromDelayedDownload(event))
		return next, false, err
	case ts43.ActivationPendingEvent:
		active.Logger().Info("TS.43 transfer activation is pending", "iccid", event.ICCID, "subscriptionResult", event.SubscriptionResult)
		return result, true, nil
	case ts43.DoneEvent:
		active.Logger().Info("TS.43 transfer completed")
		return result, true, nil
	case ts43.DismissEvent:
		active.Logger().Warn("TS.43 transfer dismissed by carrier", "subscriptionResult", event.SubscriptionResult)
		return result, false, errCarrierDismissed
	default:
		active.Logger().Warn("unexpected TS.43 transfer event", "event", fmt.Sprintf("%T", result.Event))
		return result, false, fmt.Errorf("unexpected TS.43 event %T", result.Event)
	}
}

func (s *Service) handleSMDSDiscovery(ctx context.Context, session *session, active *activeSession, result *ts43.Result, event ts43.SMDSDiscoveryEvent) (*ts43.Result, error) {
	active.Logger().Info("TS.43 transfer requires SM-DS discovery", "targetEID", event.TargetEID, "subscriptionResult", event.SubscriptionResult)
	downloadConfig, err := smdsDownloadConfig(ctx, active.targetClient, event)
	if err != nil {
		active.Logger().Warn("resolve TS.43 SM-DS download config", "error", err)
		return result, err
	}
	next, err := s.downloadEnableAndComplete(ctx, session, active, result, downloadConfig)
	if err != nil {
		active.Logger().Warn("download TS.43 SM-DS transferred profile", "error", err)
	}
	return next, err
}

func (s *Service) handleSourceProfileDeletion(ctx context.Context, session *session, active *activeSession, start Start, result *ts43.Result, event ts43.SourceProfileDeletionEvent) (*ts43.Result, error) {
	if err := session.confirmSourceDeletion(ctx, event.ICCID); err != nil {
		return result, err
	}
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageDeleting})
	active.source.Close()
	if err := s.deleteSourceProfile(ctx, active.settings, start, event.ICCID); err != nil {
		return result, err
	}
	return active.client.Continue(ctx, result, ts43.ContinueRequest{SourceProfileDeleted: true})
}

func ts43Device(imei string) ts43.Device {
	return ts43.Device{
		IMEI:            imei,
		Vendor:          ts43DeviceVendor,
		Model:           ts43DeviceModel,
		SoftwareVersion: ts43DeviceSoftwareVersion,
	}
}

func transferLogger(targetIMEI, sourceIMEI string) *slog.Logger {
	logger := mmodem.LoggerForIMEI(targetIMEI)
	sourceIMEI = strings.TrimSpace(sourceIMEI)
	if sourceIMEI != "" {
		logger = logger.With("source_imei", sourceIMEI)
	}
	return logger
}

func ts43SourceSIMType(profileType ProfileType) ts43.SIMType {
	if profileType == ProfilePhysical {
		return ts43.SIMTypePSIM
	}
	return ts43.SIMTypeESIM
}
