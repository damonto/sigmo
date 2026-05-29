package gotify

import (
	"fmt"
	"strings"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

type content struct {
	Title string
	Body  string
}

func render(ev notifyevent.Event) (content, error) {
	switch ev := ev.(type) {
	case notifyevent.OTPEvent:
		code := strings.TrimSpace(ev.Code)
		return content{
			Title: "Sigmo Login",
			Body:  fmt.Sprintf("Your verification code is %s", code),
		}, nil
	case notifyevent.SMSEvent:
		return content{
			Title: ev.DisplayCounterparty(),
			Body:  ev.DisplayText(),
		}, nil
	case notifyevent.CallEvent:
		return content{
			Title: callTitle(ev),
			Body:  fmt.Sprintf("Modem: %s\nTime: %s", strings.TrimSpace(ev.Modem), ev.DisplayTimestamp()),
		}, nil
	default:
		return content{}, fmt.Errorf("rendering gotify content for %q: unsupported event", ev.Kind())
	}
}

func callTitle(ev notifyevent.CallEvent) string {
	number := ev.DisplayCounterparty()
	if number == "" {
		return ev.DirectionLabel()
	}
	return fmt.Sprintf("%s from %s", ev.DirectionLabel(), number)
}
