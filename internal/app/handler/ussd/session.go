package ussd

import (
	"context"
	"errors"
	"fmt"

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
		return nil, fmt.Errorf("read ussd state: %w", err)
	}
	if state != mmodem.Modem3gppUssdSessionStateIdle {
		if err := ussd.Cancel(); err != nil {
			return nil, fmt.Errorf("cancel ussd session: %w", err)
		}
	}
	reply, err := ussd.Initiate(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("initiate ussd: %w", err)
	}
	return &ExecuteResponse{Reply: reply}, nil
}

func (s *session) executeReply(ctx context.Context, modem *mmodem.Modem, ussd *mmodem.USSD, code string) (*ExecuteResponse, error) {
	state, err := ussd.State()
	if err != nil {
		return nil, fmt.Errorf("read ussd state: %w", err)
	}
	if state == mmodem.Modem3gppUssdSessionStateUnknown {
		return nil, errUnknownSessionStatus
	}
	if state != mmodem.Modem3gppUssdSessionStateUserResponse {
		return nil, errSessionNotReady
	}
	reply, err := ussd.Respond(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("respond to ussd: %w", err)
	}
	return &ExecuteResponse{Reply: reply}, nil
}
