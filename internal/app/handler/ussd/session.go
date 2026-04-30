package ussd

import (
	"context"
	"errors"
	"log/slog"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type session struct{}

const (
	actionInitialize = "initialize"
	actionReply      = "reply"
)

var (
	errInvalidAction        = errors.New("action must be initialize or reply")
	errSessionNotReady      = errors.New("ussd session is not waiting for user response")
	errUnknownSessionStatus = errors.New("unable to determine ussd session state")
)

func newSession() *session {
	return &session{}
}

func (s *session) Execute(ctx context.Context, modem *mmodem.Modem, action string, code string) (*ExecuteResponse, error) {
	ussd := modem.ThreeGPP().USSD()
	switch action {
	case actionInitialize:
		return s.executeInitialize(ctx, modem, ussd, code)
	case actionReply:
		return s.executeReply(ctx, modem, ussd, code)
	default:
		return nil, errInvalidAction
	}
}

func (s *session) executeInitialize(ctx context.Context, modem *mmodem.Modem, ussd *mmodem.USSD, code string) (*ExecuteResponse, error) {
	state, err := ussd.State()
	if err != nil {
		slog.Error("failed to read ussd state", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	if state != mmodem.Modem3gppUssdSessionStateIdle {
		if err := ussd.Cancel(); err != nil {
			slog.Error("failed to cancel ussd session", "modem", modem.EquipmentIdentifier, "error", err)
			return nil, err
		}
	}
	reply, err := ussd.Initiate(ctx, code)
	if err != nil {
		slog.Error("failed to initiate ussd", "modem", modem.EquipmentIdentifier, "code", code, "error", err)
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}

func (s *session) executeReply(ctx context.Context, modem *mmodem.Modem, ussd *mmodem.USSD, code string) (*ExecuteResponse, error) {
	state, err := ussd.State()
	if err != nil {
		slog.Error("failed to read ussd state", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	if state == mmodem.Modem3gppUssdSessionStateUnknown {
		return nil, errUnknownSessionStatus
	}
	if state != mmodem.Modem3gppUssdSessionStateUserResponse {
		return nil, errSessionNotReady
	}
	reply, err := ussd.Respond(ctx, code)
	if err != nil {
		slog.Error("failed to respond to ussd", "modem", modem.EquipmentIdentifier, "code", code, "error", err)
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}
