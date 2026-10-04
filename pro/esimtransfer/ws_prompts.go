//go:build esim_transfer

package esimtransfer

import (
	"context"

	"github.com/damonto/ts43-go"
)

func (s *wsSession) userInput(ctx context.Context, event ts43.UserInputEvent) (*ts43.UserInputResult, error) {
	message := event.Message
	if err := s.send(wsServerMessage{
		Type: wsTypeUserInput,
		Input: &wsUserInputPrompt{
			Text:         message.Text,
			AcceptLabel:  message.AcceptButtonLabel,
			RejectLabel:  message.RejectButtonLabel,
			FreeText:     message.AcceptFreeText,
			FreeTextHint: message.AcceptFreeTextHint,
		},
	}); err != nil {
		return nil, err
	}
	msg, err := s.waitMessage(ctx, s.inputCh)
	if err != nil {
		return nil, err
	}
	result := &ts43.UserInputResult{Button: ts43.MessageButtonAccepted, Response: msg.Response}
	if msg.Accept != nil && !*msg.Accept {
		result.Button = ts43.MessageButtonRejected
	}
	return result, nil
}

func (s *wsSession) confirmSourceDeletion(ctx context.Context, iccid string) error {
	if err := s.send(wsServerMessage{Type: wsTypeSourceDeletion, ICCID: iccid}); err != nil {
		return err
	}
	msg, err := s.waitMessage(ctx, s.deleteCh)
	if err != nil {
		return err
	}
	if msg.Accept == nil || !*msg.Accept {
		return errSourceDeletionDeclined
	}
	return nil
}
