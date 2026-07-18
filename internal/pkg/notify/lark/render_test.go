package lark

import (
	"strings"
	"testing"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

func TestRenderReminder(t *testing.T) {
	t.Parallel()
	got, err := render(notifyevent.ReminderEvent{ProfileName: "Travel", ProfileID: "8985", Modem: "Office", Content: "Renew"})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	for _, want := range []string{"Reminder: Travel", "ICCID: 8985", "Modem: Office", "Renew"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render() = %q, want it to contain %q", got, want)
		}
	}
}
