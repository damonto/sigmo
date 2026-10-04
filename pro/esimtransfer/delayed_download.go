//go:build esim_transfer

package esimtransfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/damonto/ts43-go"
)

func (s *wsSession) delayedDownloadConfig(ctx context.Context, event ts43.DelayedDownloadEvent) (ts43.DownloadConfig, error) {
	// Only SMDSDiscoveryEvent authorizes discovery. A delayed download needs
	// the carrier's out-of-band download information before installation.
	answer, err := s.userInput(ctx, ts43.UserInputEvent{Message: ts43.Message{
		Text:               "The carrier has delayed the download. Enter the activation code supplied by the carrier when it is ready.",
		AcceptButton:       true,
		AcceptButtonLabel:  "Download",
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
	if _, err := activationCode(config); err != nil {
		return ts43.DownloadConfig{}, fmt.Errorf("parse carrier activation code: %w", err)
	}
	return config, nil
}
