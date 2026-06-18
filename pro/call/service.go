//go:build wifi_calling

package call

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/pro/wificalling"
)

const (
	wifiCallingHangupCleanupTimeout = 3 * time.Second

	RouteAuto        = "auto"
	RouteWiFiCalling = "wifi_calling"
	RouteModem       = "modem"

	DirectionIncoming = "incoming"
	DirectionOutgoing = "outgoing"

	StateDialing    = "dialing"
	StateRinging    = "ringing"
	StateAnswering  = "answering"
	StateEarlyMedia = "early_media"
	StateActive     = "active"
	StateConfirmed  = "confirmed"
	StateEnding     = "ending"
	StateEnded      = "ended"
	StateFailed     = "failed"

	HoldNone        = "none"
	HoldLocal       = "local"
	HoldRemote      = "remote"
	HoldLocalRemote = "local_remote"

	ReasonBusy = "busy"
)

var (
	ErrNumberRequired          = errors.New("number is required")
	ErrInvalidNumber           = errors.New("invalid number")
	ErrUSSDDialString          = errors.New("ussd dial strings must use the USSD API")
	ErrInvalidRoute            = errors.New("route must be auto, wifi_calling, or modem")
	ErrNoRouteAvailable        = errors.New("no call route is available")
	ErrWiFiCallingNotConnected = errors.New("wifi calling is not connected")
	ErrModemCallingUnavailable = errors.New("modem calling is not available in this version")
	ErrCallNotFound            = errors.New("call not found")
	ErrInvalidCallState        = errors.New("call state must be active or ended")
	ErrCallRecordActive        = errors.New("active calls cannot be deleted")
	ErrMediaUnavailable        = errors.New("call media is not available")
	ErrUnsupportedCodec        = errors.New("call media codec is not supported")
	ErrUnsupportedDTMF         = errors.New("call dtmf is not supported")
	ErrInvalidCallHold         = errors.New("call hold must be local or none")
	ErrCallUpdateConflict      = errors.New("call update cannot change state and hold together")
	ErrDTMFDigitsRequired      = errors.New("dtmf digits are required")
	ErrInvalidDTMFDigit        = errors.New("dtmf digits must contain 0-9, *, #, or A-D")
	ErrInvalidDTMFCallState    = errors.New("call state must be early_media, active, or confirmed")
	ErrCallOnHold              = errors.New("call is on hold")
)

type Service struct {
	store       *storage.Store
	wifiCalling wifiCallingVoice

	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

type Event struct {
	Call storage.Call
}

type UpdateRequest struct {
	State  string
	Reason string
	Hold   string
}

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

type wifiCallingVoice interface {
	Status(context.Context, *mmodem.Modem) (wificalling.Status, error)
	DialCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	AnswerCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	RejectCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	HangupCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	HoldCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	ResumeCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error)
	SendCallDTMF(context.Context, *mmodem.Modem, string, string) error
	OpenCallMedia(context.Context, *mmodem.Modem, string) (wificalling.MediaSession, error)
	SubscribeVoiceEvents(wificalling.VoiceEventFunc) func()
}

func New(store *storage.Store, wifiCalling wifiCallingVoice) *Service {
	return &Service{
		store:       store,
		wifiCalling: wifiCalling,
		subscribers: make(map[uint64]chan Event),
	}
}

func (s *Service) Run(ctx context.Context) error {
	if s.wifiCalling == nil {
		<-ctx.Done()
		return nil
	}
	unsubscribe := s.wifiCalling.SubscribeVoiceEvents(func(event wificalling.VoiceEvent) {
		if event.Call.ID == "" {
			return
		}
		call := callFromWiFiCalling(event.Call)
		if _, err := s.saveAndPublish(ctx, call); err != nil {
			slog.Warn("save Wi-Fi Calling voice event",
				"call_id", call.ID,
				"modem_id", call.ModemID,
				"profile_id", call.ProfileID,
				"state", call.State,
				"error", err,
			)
			s.publish(Event{Call: call})
			return
		}
	})
	defer unsubscribe()
	<-ctx.Done()
	return nil
}

func (s *Service) List(ctx context.Context, modem *mmodem.Modem, query string) ([]storage.Call, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.ListCalls(ctx, profileID, modem.EquipmentIdentifier, 50, query)
}

func (s *Service) Dial(ctx context.Context, modem *mmodem.Modem, number string, route string) (storage.Call, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return storage.Call{}, ErrNumberRequired
	}
	route = normalizeRoute(route)
	if !validRoute(route) {
		return storage.Call{}, ErrInvalidRoute
	}
	if isUSSDDialString(number) {
		return storage.Call{}, ErrUSSDDialString
	}
	number, err := normalizeDialString(number)
	if err != nil {
		return storage.Call{}, err
	}
	selected, err := s.selectRoute(ctx, modem, route)
	if err != nil {
		return storage.Call{}, err
	}
	switch selected {
	case RouteWiFiCalling:
		call, err := s.wifiCalling.DialCall(ctx, modem, number)
		if err != nil {
			if errors.Is(err, wificalling.ErrNotConnected) {
				return storage.Call{}, ErrWiFiCallingNotConnected
			}
			if call.ID != "" {
				failedCall := callFromWiFiCalling(call)
				if _, saveErr := s.saveAndPublish(ctx, failedCall); saveErr != nil {
					return storage.Call{}, errors.Join(fmt.Errorf("dial Wi-Fi Calling: %w", err), fmt.Errorf("save failed call: %w", saveErr))
				}
			}
			return storage.Call{}, fmt.Errorf("dial Wi-Fi Calling: %w", err)
		}
		stored := callFromWiFiCalling(call)
		stored, err = s.saveAndPublish(ctx, stored)
		if err != nil {
			return storage.Call{}, err
		}
		return stored, nil
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrNoRouteAvailable
	}
}

func (s *Service) Answer(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		updated, err := s.wifiCalling.AnswerCall(ctx, modem, call.ID)
		if err := mapWiFiCallingActionError("answer", err); err != nil {
			return storage.Call{}, err
		}
		return s.saveAndPublish(ctx, callFromWiFiCalling(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (s *Service) Reject(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		updated, err := s.wifiCalling.RejectCall(ctx, modem, call.ID)
		if err := mapWiFiCallingActionError("reject", err); err != nil {
			return storage.Call{}, err
		}
		return s.saveAndPublish(ctx, callFromWiFiCalling(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (s *Service) Update(ctx context.Context, modem *mmodem.Modem, callID string, req UpdateRequest) (storage.Call, error) {
	req.State = strings.TrimSpace(req.State)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Hold = strings.TrimSpace(req.Hold)
	if req.State != "" && req.Hold != "" {
		return storage.Call{}, ErrCallUpdateConflict
	}
	if req.Hold != "" {
		return s.SetHold(ctx, modem, callID, req.Hold)
	}
	switch req.State {
	case StateActive:
		return s.Answer(ctx, modem, callID)
	case StateEnded:
		if req.Reason == ReasonBusy {
			return s.Reject(ctx, modem, callID)
		}
		return s.Hangup(ctx, modem, callID)
	default:
		return storage.Call{}, ErrInvalidCallState
	}
}

func (s *Service) SetHold(ctx context.Context, modem *mmodem.Modem, callID string, hold string) (storage.Call, error) {
	hold = strings.TrimSpace(hold)
	if hold != HoldLocal && hold != HoldNone {
		return storage.Call{}, ErrInvalidCallHold
	}
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		var updated wificalling.VoiceCall
		if hold == HoldLocal {
			updated, err = s.wifiCalling.HoldCall(ctx, modem, call.ID)
		} else {
			updated, err = s.wifiCalling.ResumeCall(ctx, modem, call.ID)
		}
		if err := mapWiFiCallingActionError("update hold", err); err != nil {
			return storage.Call{}, err
		}
		return s.saveAndPublish(ctx, callFromWiFiCalling(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (s *Service) Hangup(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		ended, err := s.endWiFiCallingCall(ctx, call)
		if err != nil {
			return storage.Call{}, err
		}
		s.cleanupWiFiCallingHangup(ctx, modem, call.ID)
		return ended, nil
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (s *Service) endWiFiCallingCall(ctx context.Context, call storage.Call) (storage.Call, error) {
	if isTerminalCallState(call.State) {
		return call, nil
	}
	now := time.Now()
	call.State = StateEnded
	call.EndedAt = now
	call.UpdatedAt = now
	return s.saveAndPublish(ctx, call)
}

func (s *Service) cleanupWiFiCallingHangup(ctx context.Context, modem *mmodem.Modem, callID string) {
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), wifiCallingHangupCleanupTimeout)
		defer cancel()
		if _, err := s.wifiCalling.HangupCall(cleanupCtx, modem, callID); err != nil {
			if errors.Is(err, wificalling.ErrNotConnected) || errors.Is(err, wificalling.ErrUnavailable) {
				slog.Debug("clean up Wi-Fi Calling hangup", "call_id", callID, "error", err)
				return
			}
			slog.Warn("clean up Wi-Fi Calling hangup", "call_id", callID, "error", err)
		}
	}()
}

func (s *Service) SendDTMF(ctx context.Context, modem *mmodem.Modem, callID string, digits string) error {
	digits = strings.TrimSpace(digits)
	if digits == "" {
		return ErrDTMFDigitsRequired
	}
	if !validDTMFDigits(digits) {
		return ErrInvalidDTMFDigit
	}
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return err
	}
	if !dtmfCallState(call.State) {
		return ErrInvalidDTMFCallState
	}
	if call.Hold == HoldLocal || call.Hold == HoldLocalRemote {
		return ErrCallOnHold
	}
	switch call.Route {
	case RouteWiFiCalling:
		if err := s.wifiCalling.SendCallDTMF(ctx, modem, call.ID, digits); err != nil {
			return mapWiFiCallingActionError("send DTMF", err)
		}
		return nil
	case RouteModem:
		return ErrModemCallingUnavailable
	default:
		return ErrInvalidRoute
	}
}

func (s *Service) Delete(ctx context.Context, modem *mmodem.Modem, callID string) error {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return err
	}
	return s.deleteCall(ctx, call)
}

func (s *Service) deleteCall(ctx context.Context, call storage.Call) error {
	if !isTerminalCallState(call.State) {
		return ErrCallRecordActive
	}
	if err := s.store.DeleteCall(ctx, call.ProfileID, call.ModemID, call.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ErrCallNotFound
		}
		return err
	}
	return nil
}

func (s *Service) OpenMedia(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		session, err := s.wifiCalling.OpenCallMedia(ctx, modem, call.ID)
		if errors.Is(err, wificalling.ErrUnavailable) {
			s.endUnavailableWiFiCallingMedia(ctx, call)
		}
		if err := mapWiFiCallingMediaError(err); err != nil {
			return nil, err
		}
		return wifiCallingMediaSession{session: session}, nil
	case RouteModem:
		return nil, ErrModemCallingUnavailable
	default:
		return nil, ErrInvalidRoute
	}
}

func (s *Service) endUnavailableWiFiCallingMedia(ctx context.Context, call storage.Call) {
	if isTerminalCallState(call.State) {
		return
	}
	now := time.Now()
	call.State = StateEnded
	call.Reason = ErrMediaUnavailable.Error()
	call.EndedAt = now
	call.UpdatedAt = now
	if err := s.store.SaveCall(ctx, call); err != nil {
		slog.Warn("save Wi-Fi Calling call after media became unavailable",
			"call_id", call.ID,
			"modem_id", call.ModemID,
			"profile_id", call.ProfileID,
			"error", err,
		)
		return
	}
	s.publish(Event{Call: call})
}

func isTerminalCallState(state string) bool {
	return state == StateEnded || state == StateFailed
}

func dtmfCallState(state string) bool {
	return state == StateEarlyMedia || state == StateActive || state == StateConfirmed
}

func validDTMFDigits(digits string) bool {
	for _, digit := range digits {
		if !validDTMFDigit(digit) {
			return false
		}
	}
	return true
}

func validDTMFDigit(digit rune) bool {
	return digit >= '0' && digit <= '9' ||
		digit == '*' ||
		digit == '#' ||
		digit >= 'A' && digit <= 'D' ||
		digit >= 'a' && digit <= 'd'
}

func mapWiFiCallingMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, wificalling.ErrUnsupportedCodec):
		return ErrUnsupportedCodec
	case errors.Is(err, wificalling.ErrUnavailable):
		return ErrMediaUnavailable
	case errors.Is(err, wificalling.ErrNotConnected):
		return ErrWiFiCallingNotConnected
	default:
		return fmt.Errorf("open Wi-Fi Calling media: %w", err)
	}
}

func mapWiFiCallingActionError(action string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, wificalling.ErrNotConnected):
		return ErrWiFiCallingNotConnected
	case errors.Is(err, wificalling.ErrUnavailable):
		return ErrCallNotFound
	case errors.Is(err, wificalling.ErrUnsupportedDTMF):
		return ErrUnsupportedDTMF
	default:
		return fmt.Errorf("%s Wi-Fi Calling: %w", action, err)
	}
}

func (s *Service) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	s.mu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = ch
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

type wifiCallingMediaSession struct {
	session wificalling.MediaSession
}

func (s wifiCallingMediaSession) Info() MediaInfo {
	info := s.session.Info()
	return MediaInfo{
		Codec:           info.Codec,
		PayloadType:     info.PayloadType,
		ClockRate:       info.ClockRate,
		Channels:        info.Channels,
		OctetAlign:      info.OctetAlign,
		DTMFPayloadType: info.DTMFPayloadType,
		DTMFClockRate:   info.DTMFClockRate,
		PTimeMillis:     info.PTimeMillis,
	}
}

func (s wifiCallingMediaSession) ReadPacket(ctx context.Context) ([]byte, error) {
	return s.session.ReadPacket(ctx)
}

func (s wifiCallingMediaSession) WritePacket(ctx context.Context, packet []byte) error {
	return s.session.WritePacket(ctx, packet)
}

func (s *Service) callForAction(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := s.store.GetCall(ctx, callID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Call{}, ErrCallNotFound
	}
	if err != nil {
		return storage.Call{}, err
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return storage.Call{}, err
	}
	if call.ProfileID != profileID || call.ModemID != modem.EquipmentIdentifier {
		return storage.Call{}, ErrCallNotFound
	}
	return call, nil
}

func (s *Service) saveAndPublish(ctx context.Context, call storage.Call) (storage.Call, error) {
	call, publish, err := s.store.SaveCallPreservingTerminal(ctx, call)
	if err != nil {
		return storage.Call{}, err
	}
	if publish {
		s.publish(Event{Call: call})
	}
	return call, nil
}

func (s *Service) publish(event Event) {
	s.mu.Lock()
	subscribers := make([]chan Event, 0, len(s.subscribers))
	for _, ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.mu.Unlock()
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Service) selectRoute(ctx context.Context, modem *mmodem.Modem, requested string) (string, error) {
	switch requested {
	case RouteWiFiCalling:
		status, err := s.wifiCalling.Status(ctx, modem)
		if err != nil {
			return "", err
		}
		if !status.Connected {
			return "", ErrWiFiCallingNotConnected
		}
		return RouteWiFiCalling, nil
	case RouteModem:
		return RouteModem, nil
	}
	status, err := s.wifiCalling.Status(ctx, modem)
	if err != nil {
		return "", err
	}
	if status.Connected {
		return RouteWiFiCalling, nil
	}
	return "", ErrNoRouteAvailable
}

func callFromWiFiCalling(call wificalling.VoiceCall) storage.Call {
	state := strings.TrimSpace(call.State)
	now := time.Now()
	updatedAt := call.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	startedAt := call.StartedAt
	if startedAt.IsZero() {
		startedAt = updatedAt
	}
	return storage.Call{
		ID:         call.ID,
		ProfileID:  call.ProfileID,
		ModemID:    call.ModemID,
		Route:      RouteWiFiCalling,
		Direction:  call.Direction,
		Number:     call.Number,
		State:      state,
		Hold:       normalizeHold(call.Hold),
		Reason:     call.Reason,
		StartedAt:  startedAt,
		AnsweredAt: call.AnsweredAt,
		EndedAt:    call.EndedAt,
		UpdatedAt:  updatedAt,
	}
}

func normalizeHold(hold string) string {
	hold = strings.TrimSpace(hold)
	switch hold {
	case HoldLocal, HoldRemote, HoldLocalRemote:
		return hold
	default:
		return HoldNone
	}
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return RouteAuto
	}
	return route
}

func validRoute(route string) bool {
	return slices.Contains([]string{RouteAuto, RouteWiFiCalling, RouteModem}, route)
}

func isUSSDDialString(number string) bool {
	return strings.HasPrefix(number, "*") || strings.HasPrefix(number, "#")
}

func normalizeDialString(number string) (string, error) {
	normalized, ok := compactDialString(number)
	if !ok {
		return "", ErrInvalidNumber
	}
	if normalized == "" {
		return "", ErrNumberRequired
	}
	return normalized, nil
}

func compactDialString(value string) (string, bool) {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == ' ', r == '-', r == '.', r == '(', r == ')':
		default:
			return "", false
		}
	}
	number := b.String()
	if number == "+" {
		return "", false
	}
	return number, true
}
