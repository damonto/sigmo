package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/ts43-go"
	"github.com/labstack/echo/v5"
)

// peerBackend keeps the hardware boundary small enough to exercise authorization
// and connection teardown with fake cards in local integration tests.
type peerBackend interface {
	peerCatalog(context.Context, string) ([]peerCatalogSource, error)
	peerPrepare(context.Context, peerSelection, ProfileResponse, string) (*sourceConnection, error)
	peerDelete(context.Context, peerSelection, ProfileResponse, string) error
}

// RemoteSession accepts requests only. An authenticated source-side selection
// and approval is required before any source-side mutation is performed.
func (h *Handler) RemoteSession(c *echo.Context) error {
	// Browser clients use the authenticated control routes, never this endpoint.
	if c.Request().Header.Get("Origin") != "" {
		return c.NoContent(http.StatusForbidden)
	}
	ctx, release, err := h.peers.beginRemote(c.Request().Context(), c.RealIP())
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, errPeerCapacity) {
			status = http.StatusTooManyRequests
		}
		return httpapi.Error(c, status, "transfer_esim_failed", err.Error())
	}
	defer release()
	conn, err := transferWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	wire := newPeerWire(ctx, conn, h.peers.permits)
	defer wire.close()
	if err := servePeer(wire, h.peers, h.runner, c.RealIP()); err != nil {
		// No card/authentication data or backend error text crosses this boundary.
		_ = wire.send(peerFailure(err))
	}
	return nil
}

func peerFailure(err error) peerMessage {
	switch {
	case errors.Is(err, errPeerUnsupported):
		return peerMessage{Type: "error", Code: "unsupported", Message: "unsupported remote transfer protocol"}
	case errors.Is(err, errPeerDeclined):
		return peerMessage{Type: "error", Code: "declined", Message: "remote transfer request was declined"}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, errPeerExpired):
		return peerMessage{Type: "error", Code: "expired", Message: "remote transfer request expired"}
	default:
		return peerMessage{Type: "error", Code: "ended", Message: "remote transfer ended; check both devices before retrying"}
	}
}

func receivePeerWithin(w *peerWire, timeout time.Duration) (peerMessage, error) {
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()
	return w.receive(ctx)
}

func servePeer(w *peerWire, broker *peerBroker, backend peerBackend, remote string) error {
	hello, err := receivePeerWithin(w, 10*time.Second)
	if err != nil {
		return err
	}
	if hello.Version != peerVersion {
		return errPeerUnsupported
	}
	if hello.Type != "authorize" || hello.Target == nil ||
		!validPeerIMEI(hello.Target.IMEI) || len(hello.Target.EID) != 32 || len(hello.Target.Name) > 128 {
		return errors.New("invalid remote transfer handshake")
	}
	pending, err := broker.create(remote, *hello.Target, w.ctx.Done())
	if err != nil {
		return err
	}
	outcome := peerOutcomeFailed
	defer func() { broker.update(pending, "ended", outcome) }()
	if err := w.send(peerMessage{Type: "pending", Version: peerVersion}); err != nil {
		return err
	}
	approved, err := requestPeerSelection(w.ctx, broker, pending, backend)
	if err != nil {
		return err
	}
	selection, option, eid := approved.selection, approved.profile, approved.eid
	release, err := broker.reserve(string(selection.SourceType) + ":" + selection.SourceID)
	if err != nil {
		return err
	}
	defer release()
	if err := w.send(peerMessage{Type: "approved", Version: peerVersion, Profile: &option, SourceName: approved.sourceName, SourceEID: eid}); err != nil {
		return err
	}
	prepare, err := receivePeerWithin(w, 30*time.Second)
	if err != nil {
		return err
	}
	if prepare.Type != "prepare" {
		return errors.New("expected approved source preparation")
	}
	if !broker.permits() {
		return errors.New("remote transfer authorization ended")
	}
	broker.update(pending, peerPhaseRunning, "")
	outcome = peerOutcomeUncertain
	source, err := backend.peerPrepare(w.ctx, selection, option, eid)
	if err != nil {
		return err
	}
	defer source.Close()
	device := peerDeviceFrom(source.device)
	if err := w.send(peerMessage{Type: "ready", Device: &device}); err != nil {
		return err
	}
	session := peerSourceSession{wire: w, broker: broker, pending: pending, backend: backend,
		source: source, selection: selection, profile: option, eid: eid}
	outcome, err = session.run()
	return err
}

// The catalog is read-only and is disclosed only through authenticated local
// request events. The requester receives only the profile chosen by the source.
func requestPeerSelection(ctx context.Context, broker *peerBroker, pending *peerPending, backend peerBackend) (peerApproval, error) {
	ctx, cancel := context.WithTimeout(ctx, broker.waitTimeout)
	defer cancel()
	catalog, err := backend.peerCatalog(ctx, pending.request.Target.IMEI)
	if err != nil {
		return peerApproval{}, err
	}
	return broker.awaitApproval(ctx, pending, catalog)
}

func validPeerIMEI(value string) bool {
	if len(value) != 15 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

type peerSourceSession struct {
	wire      *peerWire
	broker    *peerBroker
	pending   *peerPending
	backend   peerBackend
	source    *sourceConnection
	selection peerSelection
	profile   ProfileResponse
	eid       string
}

func (s *peerSourceSession) run() (string, error) {
	outcome := peerOutcomeUncertain
	deleted := false
	nextID := uint64(1)
	for {
		message, err := s.wire.receive(s.wire.ctx)
		if err != nil {
			return outcome, err
		}
		if !s.broker.permits() {
			return outcome, errors.New("remote transfer authorization ended")
		}
		if message.ID != nextID {
			return outcome, errors.New("invalid remote operation sequence")
		}
		nextID++
		reply := peerMessage{Type: "reply", ID: message.ID}
		switch message.Type {
		case "identity":
			var identity *ts43.Identity
			identity, err = s.identity()
			reply.Identity = peerIdentityFrom(identity)
		case "aka":
			if message.AKA == nil {
				return outcome, errors.New("authentication challenge is missing")
			}
			if _, err := message.AKA.request().MarshalBinary(); err != nil {
				return outcome, err
			}
			if _, err = s.identity(); err == nil {
				var response *ts43.AKAResponse
				response, err = s.source.channel.AuthenticateAKA(s.wire.ctx, message.AKA.request())
				reply.AKAResponse = peerAKAResponseFrom(response)
			}
		case "close_source":
			s.source.Close()
		case "delete":
			if deleted || message.ICCID != s.profile.ICCID {
				return outcome, errors.New("source deletion is outside the approved transfer")
			}
			deleted = true
			s.source.Close()
			err = s.backend.peerDelete(s.wire.ctx, s.selection, s.profile, s.eid)
		case "progress":
			if len(message.Stage) > 64 {
				return outcome, errors.New("invalid transfer progress")
			}
			s.broker.progress(s.pending, message.Stage)
		case "finish":
			if message.Outcome == "completed" {
				outcome = peerOutcomeCompleted
			} else {
				outcome = peerOutcomeFailed
			}
			s.source.Close()
			s.broker.update(s.pending, peerPhaseEnded, outcome)
			if err := s.wire.send(reply); err != nil {
				return outcome, err
			}
			// The peer closes after receiving the terminal acknowledgement. No
			// further operations are dispatched while waiting for that close.
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			select {
			case <-s.wire.ctx.Done():
			case <-timer.C:
			}
			return outcome, nil
		default:
			return outcome, errors.New("unsupported remote transfer operation")
		}
		if err != nil {
			return outcome, err
		}
		if err := s.wire.send(reply); err != nil {
			return outcome, err
		}
	}
}

func (s *peerSourceSession) identity() (*ts43.Identity, error) {
	if s.source.release == nil {
		return nil, errors.New("source channel is closed")
	}
	identity, err := s.source.channel.Identity(s.wire.ctx)
	if err != nil {
		return nil, err
	}
	if identity == nil || identity.ICCID != s.profile.ICCID {
		return nil, errors.New("source profile changed")
	}
	return identity, nil
}

func catalogSelection(catalog []peerCatalogSource, selection peerSelection) (ProfileResponse, string, string, bool) {
	for _, entry := range catalog {
		if entry.Source.ID != selection.SourceID || entry.Source.Type != selection.SourceType {
			continue
		}
		option, ok := findSourceProfileOption(entry.Profiles, selection.ProfileID)
		eid := entry.EIDs[option.ID]
		return option, eid, entry.Source.Name, ok && option.Supported && option.Type == ProfileESIM && eid != ""
	}
	return ProfileResponse{}, "", "", false
}

func selectionStart(selection peerSelection) startRequest {
	return startRequest{SourceType: selection.SourceType, SourceID: selection.SourceID, ProfileID: selection.ProfileID, SourceIMEI: selection.SourceIMEI}
}

func (s *transferRunner) peerCatalog(ctx context.Context, targetIMEI string) ([]peerCatalogSource, error) {
	sources, err := s.Sources(ctx, nil)
	if err != nil {
		return nil, err
	}
	if s.carrierConfig == nil {
		return nil, errors.New("carrier configuration is unavailable")
	}
	config, err := s.carrierConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load transfer carrier configuration: %w", err)
	}
	catalog := make([]peerCatalogSource, 0, len(sources.Sources))
	current := s.settingsSnapshot()
	for _, source := range sources.Sources {
		if source.Type == SourceModem && source.ID == targetIMEI {
			continue
		}
		// Read eUICC profiles directly: physical SIM identity reads can authenticate
		// or open an active USIM and are unnecessary for this eSIM-only catalog.
		req := ProfilesRequest{SourceType: source.Type, SourceID: source.ID, SourceIMEI: targetIMEI}
		var profiles []ProfileResponse
		if source.Type == SourceModem {
			modem, findErr := s.registry.Find(ctx, source.ID)
			if findErr != nil {
				continue
			}
			rows, listErr := s.sourceModemProfiles(ctx, modem, current)
			if listErr != nil {
				continue
			}
			profiles = modemESIMOptions(rows, config.Entitlements)
		} else {
			rows, listErr := s.sourceCCIDProfiles(ctx, current, req)
			if listErr != nil {
				continue
			}
			profiles = esimOptions(rows, "", config.Entitlements)
		}
		entry := peerCatalogSource{Source: source, Profiles: make([]ProfileResponse, 0), EIDs: make(map[string]string)}
		eids := make(map[string]string)
		for _, profile := range profiles {
			if !profile.Supported || profile.Type != ProfileESIM {
				continue
			}
			start := startRequest{SourceType: source.Type, SourceID: source.ID, ProfileID: profile.ID, SourceIMEI: targetIMEI}
			eid, known := eids[profile.SEID]
			if !known {
				eid, err = s.sourceEID(ctx, current, start)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					continue
				}
				eids[profile.SEID] = eid
			}
			entry.Profiles = append(entry.Profiles, profile)
			entry.EIDs[profile.ID] = eid
		}
		if len(entry.Profiles) > 0 {
			catalog = append(catalog, entry)
		}
	}
	return catalog, nil
}

func (s *transferRunner) verifyPeerProfile(ctx context.Context, selection peerSelection, approved ProfileResponse, eid string) error {
	start := selectionStart(selection)
	actualEID, err := s.sourceEID(ctx, s.settingsSnapshot(), start)
	if err != nil {
		return err
	}
	if actualEID != eid {
		return errors.New("source eUICC changed")
	}
	options, err := s.sourceProfileOptions(ctx, s.settingsSnapshot(), ProfilesRequest{SourceType: selection.SourceType, SourceID: selection.SourceID, SourceIMEI: selection.SourceIMEI})
	if err != nil {
		return err
	}
	option, ok := findSourceProfileOption(options, selection.ProfileID)
	if !ok || !option.Supported || option.ICCID != approved.ICCID || option.SEID != approved.SEID {
		return errors.New("source profile changed")
	}
	return nil
}

func (s *transferRunner) peerPrepare(ctx context.Context, selection peerSelection, approved ProfileResponse, eid string) (*sourceConnection, error) {
	if err := s.verifyPeerProfile(ctx, selection, approved, eid); err != nil {
		return nil, err
	}
	start := selectionStart(selection)
	if err := s.activateSourceProfile(ctx, s.settingsSnapshot(), start, approved); err != nil {
		return nil, err
	}
	// Enabling a profile can replace the modem generation. Check its identity
	// again rather than trusting a reader/modem name that may have been reused.
	if err := s.verifyPeerProfile(ctx, selection, approved, eid); err != nil {
		return nil, err
	}
	source, err := s.openSource(ctx, s.settingsSnapshot(), start)
	if err != nil {
		return nil, err
	}
	identity, err := source.channel.Identity(ctx)
	if err != nil || identity == nil || identity.ICCID != approved.ICCID {
		source.Close()
		return nil, errors.New("activated source does not match the approved profile")
	}
	source.device.ICCID, source.device.EID = approved.ICCID, eid
	source.simType = ts43.SIMTypeESIM
	return source, nil
}

func (s *transferRunner) peerDelete(ctx context.Context, selection peerSelection, approved ProfileResponse, eid string) error {
	if err := s.verifyPeerProfile(ctx, selection, approved, eid); err != nil {
		return err
	}
	if err := s.deleteSourceProfile(ctx, s.settingsSnapshot(), selectionStart(selection), approved.ICCID); err != nil {
		return fmt.Errorf("delete approved source profile: %w", err)
	}
	return nil
}
