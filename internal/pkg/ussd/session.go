package ussd

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	actionInitialize = "initialize"
	actionReply      = "reply"
)

var (
	ErrInvalidAction        = errors.New("action must be initialize or reply")
	ErrSessionNotReady      = errors.New("ussd session is not waiting for user response")
	ErrUnknownSessionStatus = errors.New("unable to determine ussd session state")
)

type session struct{}

func newSession() *session {
	return &session{}
}

func (s *session) Execute(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	ussd := modem.ThreeGPP().USSD()
	switch action {
	case actionInitialize:
		return s.executeInitialize(ctx, ussd, code)
	case actionReply:
		return s.executeReply(ctx, ussd, code)
	default:
		return "", ErrInvalidAction
	}
}

func (s *session) executeInitialize(ctx context.Context, ussd *mmodem.USSD, code string) (string, error) {
	state, err := ussd.State(ctx)
	if err != nil {
		return "", fmt.Errorf("read ussd state: %w", err)
	}
	if state != mmodem.Modem3GPPUSSDSessionStateIdle {
		if err := ussd.Cancel(ctx); err != nil {
			return "", fmt.Errorf("cancel ussd session: %w", err)
		}
	}
	reply, err := ussd.Initiate(ctx, code)
	if err != nil {
		return "", fmt.Errorf("initiate ussd: %w", err)
	}
	return reply, nil
}

func (s *session) executeReply(ctx context.Context, ussd *mmodem.USSD, code string) (string, error) {
	state, err := ussd.State(ctx)
	if err != nil {
		return "", fmt.Errorf("read ussd state: %w", err)
	}
	if state == mmodem.Modem3GPPUSSDSessionStateUnknown {
		return "", ErrUnknownSessionStatus
	}
	if state != mmodem.Modem3GPPUSSDSessionStateUserResponse {
		return "", ErrSessionNotReady
	}
	reply, err := ussd.Respond(ctx, code)
	if err != nil {
		return "", fmt.Errorf("respond to ussd: %w", err)
	}
	return reply, nil
}
