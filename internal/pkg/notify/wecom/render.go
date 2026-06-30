package wecom

import (
	"fmt"
	"strings"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func render(ev notifyevent.Event) (string, error) {
	switch ev := ev.(type) {
	case notifyevent.OTPEvent:
		code := strings.TrimSpace(ev.Code)
		return fmt.Sprintf("Sigmo Login\nYour verification code is %s", code), nil
	case notifyevent.SMSEvent:
		return fmt.Sprintf("%s\n%s", ev.DisplayCounterparty(), ev.DisplayText()), nil
	case notifyevent.CallEvent:
		return fmt.Sprintf("%s\nModem: %s\nTime: %s", callTitle(ev), strings.TrimSpace(ev.Modem), ev.DisplayTimestamp()), nil
	default:
		return "", fmt.Errorf("rendering wecom content for %q: unsupported event", ev.Kind())
	}
}

func callTitle(ev notifyevent.CallEvent) string {
	number := ev.DisplayCounterparty()
	if number == "" {
		return ev.DirectionLabel()
	}
	return fmt.Sprintf("%s from %s", ev.DirectionLabel(), number)
}
