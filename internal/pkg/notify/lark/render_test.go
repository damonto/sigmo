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

func TestRenderCall(t *testing.T) {
	t.Parallel()
	got, err := render(notifyevent.CallEvent{Modem: "Office", From: "+8613344445555", To: "+8613344445556", Incoming: true})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	for _, want := range []string{"Incoming Call from +86 133 4444 5555", "To: +86 133 4444 5556", "Modem: Office"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render() = %q, want it to contain %q", got, want)
		}
	}
}
