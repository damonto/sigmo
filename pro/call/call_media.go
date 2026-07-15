//go:build ims

package call

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	imsvoice "github.com/damonto/ims-go/ims/voice"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pims "github.com/damonto/sigmo/pro/ims"
)

type callMedia struct {
	routes  *callRoutes
	records *callRecords
}

func (m *callMedia) Open(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	call, err := m.records.callForAction(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	switch call.Route {
	case RouteWiFiCalling, RouteVoLTE:
		voice := m.routes.voice(call.Route)
		if voice == nil {
			return nil, notConnectedError(call.Route)
		}
		session, err := voice.OpenCallMedia(ctx, modem, call.ID)
		if errors.Is(err, pims.ErrUnavailable) {
			m.endUnavailable(ctx, call)
		}
		if err := mapIMSMediaError(call.Route, err); err != nil {
			return nil, err
		}
		return imsMediaSession{session: session}, nil
	case RouteModem:
		return nil, ErrModemCallingUnavailable
	default:
		return nil, ErrInvalidRoute
	}
}

func (m *callMedia) endUnavailable(ctx context.Context, call storage.Call) {
	if isTerminalCallState(call.State) {
		return
	}
	now := time.Now()
	call.State = StateEnded
	call.Reason = ErrMediaUnavailable.Error()
	call.EndedAt = now
	call.UpdatedAt = now
	if err := m.records.store.SaveCall(ctx, call); err != nil {
		slog.Warn("save IMS call after media became unavailable",
			"call_id", call.ID,
			"route", call.Route,
			"modem_id", call.ModemID,
			"profile_id", call.ProfileID,
			"error", err,
		)
		return
	}
	m.records.events.publish(Event{Call: call})
}

type imsMediaSession struct {
	session pims.MediaSession
}

func (s imsMediaSession) Media() imsvoice.NegotiatedMedia {
	return s.session.Media()
}

func (s imsMediaSession) ReadPacket(ctx context.Context) ([]byte, error) {
	return s.session.ReadPacket(ctx)
}

func (s imsMediaSession) WritePacket(ctx context.Context, packet []byte) error {
	return s.session.WritePacket(ctx, packet)
}

func mapIMSMediaError(route string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pims.ErrUnsupportedCodec):
		return ErrUnsupportedCodec
	case errors.Is(err, pims.ErrUnavailable):
		return ErrMediaUnavailable
	case errors.Is(err, pims.ErrNotConnected):
		return notConnectedError(route)
	default:
		return fmt.Errorf("open %s media: %w", routeLabel(route), err)
	}
}
