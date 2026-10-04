//go:build esim_transfer

package esimtransfer

import (
	"context"

	"github.com/damonto/ts43-go"
)

func transferPending(result *ts43.Result) bool {
	return result != nil && (result.State == ts43.StatePendingConfiguration || result.State == ts43.StatePendingActivation)
}

func continuePendingTransfer(ctx context.Context, session *wsSession, active *transferState, result *ts43.Result) (*ts43.Result, error) {
	stage := stageCarrier
	message := "The carrier is preparing the download. Check again after the carrier signals readiness."
	if result.State == ts43.StatePendingActivation {
		stage = stageCompleting
		message = "The profile is installed, but the carrier has not confirmed activation. Check again after the carrier signals readiness."
	}
	// A pending result can explicitly disable polling. Keep the same TS.43
	// session and let the user trigger the next check instead of restarting it.
	answer, err := session.userInput(ctx, ts43.UserInputEvent{Message: ts43.Message{
		Text:              message,
		AcceptButton:      true,
		AcceptButtonLabel: "Check again",
		RejectButton:      true,
		RejectButtonLabel: "Cancel",
	}})
	if err != nil {
		return result, err
	}
	if answer.Button != ts43.MessageButtonAccepted {
		return result, context.Canceled
	}
	session.sendIfConnected(wsServerMessage{Type: wsTypeProgress, Stage: stage})
	return active.ts43Client.Continue(ctx, result, ts43.ContinueRequest{})
}
