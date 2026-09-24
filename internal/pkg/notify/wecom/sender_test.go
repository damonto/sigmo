package wecom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestSenderSend(t *testing.T) {
	t.Parallel()

	payloads := make(chan message, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got message
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("Decode() error = %v", err)
		}
		payloads <- got
	}))
	t.Cleanup(server.Close)

	sender, err := New(&settings.Channel{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	msg := notifycontent.Message{
		Subject:  "Incoming Call",
		Headline: "Incoming Call from 10010",
		Fields:   []notifycontent.Field{{Label: "From", Value: "10010"}, {Label: "Modem", Value: "Office"}},
	}
	if err := sender.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	want := message{MsgType: "text", Text: text{Content: "Incoming Call\n\nFrom: 10010\nModem: Office"}}
	if got := <-payloads; got != want {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}
