//go:build esim_transfer

package esimtransfer

import (
	"context"

	"github.com/damonto/ts43-go/ts43"
)

func (s *session) userInput(ctx context.Context, event ts43.UserInputEvent) (*ts43.UserInputResult, error) {
	message := event.Message
	if err := s.send(serverMessage{
		Type: wsTypeUserInput,
		Input: &userInputMessage{
			Text:         message.Text,
			AcceptLabel:  message.AcceptButtonLabel,
			RejectLabel:  message.RejectButtonLabel,
			FreeText:     message.AcceptFreeText,
			FreeTextHint: message.AcceptFreeTextHint,
		},
	}); err != nil {
		return nil, err
	}
	msg, ok := s.waitForUserInput(ctx)
	if !ok {
		return nil, ctx.Err()
	}
	result := &ts43.UserInputResult{Button: ts43.MessageButtonAccepted, Response: msg.Response}
	if msg.Accept != nil && !*msg.Accept {
		result.Button = ts43.MessageButtonRejected
	}
	return result, nil
}

func (s *session) confirmSourceDeletion(ctx context.Context, iccid string) error {
	if err := s.send(serverMessage{Type: wsTypeSourceDeletion, ICCID: iccid}); err != nil {
		return err
	}
	msg, ok := s.waitForSourceDeletion(ctx)
	if !ok {
		return ctx.Err()
	}
	if msg.Accept == nil || !*msg.Accept {
		return errSourceDeletionDeclined
	}
	return nil
}
