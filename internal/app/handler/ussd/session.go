package ussd

import (
	"context"
	"errors"
	"log/slog"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Service struct{}

const (
	actionInitialize = "initialize"
	actionReply      = "reply"
)

var (
	errInvalidAction        = errors.New("action must be initialize or reply")
	errSessionNotReady      = errors.New("ussd session is not waiting for user response")
	errUnknownSessionStatus = errors.New("unable to determine ussd session state")
)

func NewService() *Service {
	return &Service{}
}

func (s *Service) Execute(ctx context.Context, modem *mmodem.Modem, action string, code string) (*ExecuteResponse, error) {
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

func (s *Service) executeInitialize(ctx context.Context, modem *mmodem.Modem, ussd *mmodem.USSD, code string) (*ExecuteResponse, error) {
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
	reply, err := executeWithTimeout(ctx, func() (string, error) {
		return ussd.Initiate(code)
	})
	if err != nil {
		slog.Error("failed to initiate ussd", "modem", modem.EquipmentIdentifier, "code", code, "error", err)
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}

func (s *Service) executeReply(ctx context.Context, modem *mmodem.Modem, ussd *mmodem.USSD, code string) (*ExecuteResponse, error) {
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
	reply, err := executeWithTimeout(ctx, func() (string, error) {
		return ussd.Respond(code)
	})
	if err != nil {
		slog.Error("failed to respond to ussd", "modem", modem.EquipmentIdentifier, "code", code, "error", err)
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}

type executeResult struct {
	reply string
	err   error
}

func executeWithTimeout(ctx context.Context, fn func() (string, error)) (string, error) {
	resultCh := make(chan executeResult, 1)
	go func() {
		reply, err := fn()
		resultCh <- executeResult{reply: reply, err: err}
	}()

	select {
	case result := <-resultCh:
		return result.reply, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
