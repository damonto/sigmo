package message

import (
	"errors"
	"log/slog"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errRecipientRequired = errors.New("recipient is required")
	errTextRequired      = errors.New("text is required")
)

func (s *Service) Send(modem *mmodem.Modem, to string, text string) error {
	if strings.TrimSpace(to) == "" {
		return errRecipientRequired
	}
	if strings.TrimSpace(text) == "" {
		return errTextRequired
	}
	_, err := modem.Messaging().Send(to, text)
	if err != nil {
		slog.Error("failed to send SMS", "modem", modem.EquipmentIdentifier, "to", to, "error", err)
		return err
	}
	return nil
}
