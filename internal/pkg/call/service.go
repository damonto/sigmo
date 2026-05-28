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
	"github.com/damonto/sigmo/internal/pkg/phonenumber"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

const (
	RouteAuto        = "auto"
	RouteWiFiCalling = "wifi_calling"
	RouteModem       = "modem"

	DirectionIncoming = "incoming"
	DirectionOutgoing = "outgoing"

	StateDialing   = "dialing"
	StateRinging   = "ringing"
	StateAnswering = "answering"
	StateActive    = "active"
	StateEnding    = "ending"
	StateEnded     = "ended"
	StateFailed    = "failed"

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
	ErrMediaUnavailable        = errors.New("call media is not available")
	ErrUnsupportedCodec        = errors.New("call media codec is not supported")
)

type Service struct {
	store       *storage.Store
	wifiCalling wificalling.Coordinator

	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

type Event struct {
	Call storage.Call
}

type MediaInfo struct {
	Codec           string
	PayloadType     int
	ClockRate       int
	Channels        int
	DTMFPayloadType int
	DTMFClockRate   int
	PTimeMillis     int
}

type MediaSession interface {
	Info() MediaInfo
	ReadPacket(context.Context) ([]byte, error)
	WritePacket(context.Context, []byte) error
}

func New(store *storage.Store, wifiCalling wificalling.Coordinator) *Service {
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
		if err := s.store.SaveCall(ctx, call); err != nil {
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
		s.publish(Event{Call: call})
	})
	defer unsubscribe()
	<-ctx.Done()
	return nil
}

func (s *Service) List(ctx context.Context, modem *mmodem.Modem) ([]storage.Call, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.ListCalls(ctx, profileID, modem.EquipmentIdentifier, 50)
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
	number, err := normalizeDialNumber(ctx, modem, number)
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
				if saveErr := s.store.SaveCall(ctx, failedCall); saveErr != nil {
					return storage.Call{}, errors.Join(fmt.Errorf("dial Wi-Fi Calling: %w", err), fmt.Errorf("save failed call: %w", saveErr))
				}
				s.publish(Event{Call: failedCall})
			}
			return storage.Call{}, fmt.Errorf("dial Wi-Fi Calling: %w", err)
		}
		stored := callFromWiFiCalling(call)
		if err := s.store.SaveCall(ctx, stored); err != nil {
			return storage.Call{}, err
		}
		s.publish(Event{Call: stored})
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

func (s *Service) Update(ctx context.Context, modem *mmodem.Modem, callID string, state string, reason string) (storage.Call, error) {
	state = strings.TrimSpace(state)
	reason = strings.TrimSpace(reason)
	switch state {
	case StateActive:
		return s.Answer(ctx, modem, callID)
	case StateEnded:
		if reason == ReasonBusy {
			return s.Reject(ctx, modem, callID)
		}
		return s.Hangup(ctx, modem, callID)
	default:
		return storage.Call{}, ErrInvalidCallState
	}
}

func (s *Service) Hangup(ctx context.Context, modem *mmodem.Modem, callID string) (storage.Call, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return storage.Call{}, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		updated, err := s.wifiCalling.HangupCall(ctx, modem, call.ID)
		if err := mapWiFiCallingActionError("hang up", err); err != nil {
			return storage.Call{}, err
		}
		return s.saveAndPublish(ctx, callFromWiFiCalling(updated))
	case RouteModem:
		return storage.Call{}, ErrModemCallingUnavailable
	default:
		return storage.Call{}, ErrInvalidRoute
	}
}

func (s *Service) OpenMedia(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	call, err := s.callForAction(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	switch call.Route {
	case RouteWiFiCalling:
		session, err := s.wifiCalling.OpenCallMedia(ctx, modem, call.ID)
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
	if err := s.store.SaveCall(ctx, call); err != nil {
		return storage.Call{}, err
	}
	s.publish(Event{Call: call})
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
		Reason:     call.Reason,
		StartedAt:  startedAt,
		AnsweredAt: call.AnsweredAt,
		EndedAt:    call.EndedAt,
		UpdatedAt:  updatedAt,
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

func normalizeDialNumber(ctx context.Context, modem *mmodem.Modem, number string) (string, error) {
	normalized, err := phonenumber.Normalize(ctx, modem, number)
	switch {
	case err == nil:
		return normalized, nil
	case errors.Is(err, phonenumber.ErrRequired):
		return "", ErrNumberRequired
	case errors.Is(err, phonenumber.ErrInvalid):
		return "", ErrInvalidNumber
	default:
		return "", err
	}
}
