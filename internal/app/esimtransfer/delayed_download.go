package esimtransfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/damonto/ts43-go"
)

func (s *wsSession) delayedDownloadConfig(ctx context.Context, event ts43.DelayedDownloadEvent) (ts43.DownloadConfig, error) {
	// A delayed download can be checked through the same carrier session.
	// Only SMDSDiscoveryEvent authorizes discovery through SM-DS.
	answer, err := s.userInput(ctx, ts43.UserInputEvent{Message: ts43.Message{
		Text:               "The carrier has delayed the download. Check again when it is ready, or enter an activation code supplied by the carrier.",
		AcceptButton:       true,
		AcceptButtonLabel:  "Check again",
		RejectButton:       true,
		RejectButtonLabel:  "Cancel",
		AcceptFreeText:     true,
		AcceptFreeTextHint: "LPA:1$...",
	}})
	if err != nil {
		return ts43.DownloadConfig{}, err
	}
	if answer.Button != ts43.MessageButtonAccepted {
		return ts43.DownloadConfig{}, context.Canceled
	}
	config := ts43.DownloadConfig{ActivationCode: strings.TrimSpace(answer.Response), IMEI: event.TargetIMEI}
	if config.ActivationCode == "" {
		return config, nil
	}
	if _, err := activationCode(config); err != nil {
		return ts43.DownloadConfig{}, fmt.Errorf("parse carrier activation code: %w", err)
	}
	return config, nil
}
