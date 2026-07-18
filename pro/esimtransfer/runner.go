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
	"github.com/damonto/sigmo/pro/websheet"
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

type transferRunner struct {
	store         *settings.Store
	registry      *mmodem.Registry
	enableProfile func(context.Context, *mmodem.Modem, string, sgp22.ICCID) error
	deleteProfile func(context.Context, *mmodem.Modem, string, sgp22.ICCID) error
	websheets     *websheet.Broker
}

func newTransferRunner(opts Config) *transferRunner {
	return &transferRunner{
		store:         opts.Store,
		registry:      opts.Registry,
		enableProfile: opts.EnableProfile,
		deleteProfile: opts.DeleteProfile,
		websheets:     opts.Websheets,
	}
}

type startRequest struct {
	SEID       string
	SourceType SourceType
	SourceID   string
	ProfileID  string
	SourceIMEI string
}

type sourceConnection struct {
	channel ts43.Channel
	release func()
	device  ts43.Device
	simType ts43.SIMType
}

func (s *sourceConnection) Close() {
	if s == nil || s.release == nil {
		return
	}
	s.release()
	s.release = nil
}

type transferState struct {
	settings        *settings.Settings
	source          *sourceConnection
	target          *mmodem.Modem
	targetSEID      string
	targetLPA       *ilpa.LPA
	ts43Client      *ts43.Client
	logger          *slog.Logger
	targetLPAClosed bool
}

func (t *transferState) Close() {
	if t == nil {
		return
	}
	t.CloseTarget()
	t.source.Close()
}

func (t *transferState) CloseTarget() {
	if t == nil || t.targetLPAClosed || t.targetLPA == nil {
		return
	}
	t.targetLPAClosed = true
	if cerr := t.targetLPA.Close(); cerr != nil {
		t.Logger().Warn("close target LPA client", "error", cerr)
	}
}

func (t *transferState) Logger() *slog.Logger {
	return t.logger
}

func (s *transferRunner) Sources(ctx context.Context, target *mmodem.Modem) (SourcesResponse, error) {
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

func (s *transferRunner) Profiles(ctx context.Context, target *mmodem.Modem, req ProfilesRequest) ([]ProfileResponse, error) {
	req = normalizeProfilesRequest(req)
	if err := validateTarget(target, startRequest{SourceType: req.SourceType, SourceID: req.SourceID}); err != nil {
		return nil, err
	}
	currentSettings := s.settingsSnapshot()
	return s.sourceProfileOptions(ctx, currentSettings, req)
}

func (s *transferRunner) Serve(ctx context.Context, conn *websocket.Conn, target *mmodem.Modem) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session := newWSSession(conn, cancel)
	startMsg, ok := session.waitForStart(sessionCtx)
	if !ok {
		return nil
	}
	start := startRequest{
		SEID:       strings.TrimSpace(startMsg.SEID),
		SourceType: SourceType(strings.TrimSpace(string(startMsg.SourceType))),
		SourceID:   strings.TrimSpace(startMsg.SourceID),
		ProfileID:  strings.TrimSpace(startMsg.ProfileID),
		SourceIMEI: strings.TrimSpace(startMsg.SourceIMEI),
	}
	if err := s.run(sessionCtx, session, target, start); err != nil {
		_ = session.send(wsServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}
	_ = session.send(wsServerMessage{Type: wsTypeCompleted})
	return nil
}

func (s *transferRunner) settingsSnapshot() *settings.Settings {
	currentSettings := s.store.Snapshot()
	return &currentSettings
}

func (s *transferRunner) run(ctx context.Context, session *wsSession, target *mmodem.Modem, start startRequest) error {
	if err := validateStart(start); err != nil {
		return err
	}
	if err := validateTarget(target, start); err != nil {
		return err
	}
	session.sendIfConnected(wsServerMessage{Type: wsTypeProgress, Stage: stagePreparing})

	active, err := s.prepare(ctx, target, start)
	if err != nil {
		return err
	}
	defer active.Close()

	session.sendIfConnected(wsServerMessage{Type: wsTypeProgress, Stage: stageCarrier})
	result, err := active.ts43Client.Transfer(ctx)
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

func validateStart(start startRequest) error {
	if start.SEID == "" || start.SourceType == "" || start.SourceID == "" || start.ProfileID == "" {
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

func validateTarget(target *mmodem.Modem, start startRequest) error {
	if target == nil || start.SourceType != SourceModem {
		return nil
	}
	if start.SourceID == target.EquipmentIdentifier {
		return ErrSourceIsTarget
	}
	return nil
}

func (s *transferRunner) prepare(ctx context.Context, target *mmodem.Modem, start startRequest) (*transferState, error) {
	currentSettings := s.settingsSnapshot()
	options, err := s.sourceProfileOptions(ctx, currentSettings, ProfilesRequest{
		SourceType: start.SourceType,
		SourceID:   start.SourceID,
		SourceIMEI: start.SourceIMEI,
	})
	if err != nil {
		return nil, err
	}
	option, ok := findSourceProfileOption(options, start.ProfileID)
	if !ok || !option.Supported {
		return nil, ErrProfileUnsupported
	}
	if err := s.activateSourceProfile(ctx, currentSettings, start, option); err != nil {
		return nil, err
	}

	source, err := s.openSource(ctx, currentSettings, start)
	if err != nil {
		return nil, err
	}
	source.simType = ts43SourceSIMType(option.Type)
	source.device.ICCID = option.ICCID
	releaseSource := true
	defer func() {
		if releaseSource {
			source.Close()
		}
	}()

	targetSE, err := ilpa.ResolveSE(target, start.SEID)
	if err != nil {
		return nil, fmt.Errorf("resolve target eUICC SE: %w", err)
	}
	targetLPA, err := ilpa.NewWithAID(target, currentSettings, targetSE.AID)
	if err != nil {
		return nil, fmt.Errorf("create target LPA client: %w", err)
	}
	releaseTarget := true
	defer func() {
		if releaseTarget {
			if cerr := targetLPA.Close(); cerr != nil {
				targetLPA.Logger().Warn("close target LPA client", "error", cerr)
			}
		}
	}()

	targetIMEI, err := target.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read target IMEI: %w", err)
	}
	eid, err := targetLPA.EID()
	if err != nil {
		return nil, fmt.Errorf("read target EID: %w", err)
	}
	targetDevice := ts43Device(targetIMEI)
	targetDevice.EID = strings.ToUpper(hex.EncodeToString(eid))
	logger := transferLogger(targetIMEI, source.device.IMEI)
	ts43Client, err := ts43.New(&ts43.Config{
		Logger:      logger,
		Entitlement: ts43.Entitlement{SourceSIMType: source.simType},
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
	return &transferState{
		settings:   currentSettings,
		source:     source,
		target:     target,
		targetSEID: targetSE.ID,
		targetLPA:  targetLPA,
		ts43Client: ts43Client,
		logger:     logger,
	}, nil
}

func (s *transferRunner) handleEvent(ctx context.Context, session *wsSession, active *transferState, start startRequest, result *ts43.Result) (*ts43.Result, bool, error) {
	switch event := result.Event.(type) {
	case ts43.UserInputEvent:
		active.Logger().Info("TS.43 transfer requires user input")
		answer, err := session.userInput(ctx, event)
		if err != nil {
			active.Logger().Warn("TS.43 user input interrupted", "error", err)
			return result, false, err
		}
		next, err := active.ts43Client.Continue(ctx, result, ts43.ContinueRequest{UserInput: answer})
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
		next, err := s.downloadAndCompleteActivation(ctx, session, active, result, event.Config)
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

func (s *transferRunner) handleSMDSDiscovery(ctx context.Context, session *wsSession, active *transferState, result *ts43.Result, event ts43.SMDSDiscoveryEvent) (*ts43.Result, error) {
	active.Logger().Info("TS.43 transfer requires SM-DS discovery", "targetEID", event.TargetEID, "subscriptionResult", event.SubscriptionResult)
	downloadConfig, err := smdsDownloadConfig(ctx, active.targetLPA, event)
	if err != nil {
		active.Logger().Warn("resolve TS.43 SM-DS download config", "error", err)
		return result, err
	}
	next, err := s.downloadAndCompleteActivation(ctx, session, active, result, downloadConfig)
	if err != nil {
		active.Logger().Warn("download TS.43 SM-DS transferred profile", "error", err)
	}
	return next, err
}

func (s *transferRunner) handleSourceProfileDeletion(ctx context.Context, session *wsSession, active *transferState, start startRequest, result *ts43.Result, event ts43.SourceProfileDeletionEvent) (*ts43.Result, error) {
	if err := session.confirmSourceDeletion(ctx, event.ICCID); err != nil {
		return result, err
	}
	session.sendIfConnected(wsServerMessage{Type: wsTypeProgress, Stage: stageDeleting})
	active.source.Close()
	if err := s.deleteSourceProfile(ctx, active.settings, start, event.ICCID); err != nil {
		return result, err
	}
	return active.ts43Client.Continue(ctx, result, ts43.ContinueRequest{SourceProfileDeleted: true})
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
