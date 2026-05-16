package message

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errRecipientRequired = errors.New("recipient is required")
	errTextRequired      = errors.New("text is required")
)

func (m *message) Send(ctx context.Context, modem *mmodem.Modem, to string, text string) error {
	if strings.TrimSpace(to) == "" {
		return errRecipientRequired
	}
	if strings.TrimSpace(text) == "" {
		return errTextRequired
	}
	_, err := modem.Messaging().Send(ctx, to, text)
	if err != nil {
		return fmt.Errorf("send SMS to %s: %w", to, err)
	}
	return nil
}
