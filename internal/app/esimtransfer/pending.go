package esimtransfer

import (
	"context"
	"errors"
	"net/http"

	"github.com/damonto/ts43-go"
)

func transferPending(result *ts43.Result) bool {
	return result != nil && (result.State == ts43.StatePendingConfiguration || result.State == ts43.StatePendingActivation)
}

func transferRetryable(result *ts43.Result, err error) bool {
	var denied *ts43.CarrierDeniedError
	if !transferPending(result) || errors.As(err, &denied) {
		return false
	}
	var httpErr *ts43.HTTPError
	if errors.As(err, &httpErr) {
		// The library has already exhausted credential rejection retries.
		return httpErr.StatusCode == http.StatusRequestTimeout ||
			httpErr.StatusCode == http.StatusTooManyRequests ||
			(httpErr.StatusCode >= 500 && httpErr.StatusCode != http.StatusNetworkAuthenticationRequired)
	}
	return true
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
