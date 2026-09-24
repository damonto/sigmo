package bark

import (
	"testing"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  notifycontent.Message
		want content
	}{
		{
			name: "headline titles the body",
			msg: notifycontent.Message{
				Subject:  "Incoming SMS",
				Headline: "Incoming SMS from 10086",
				Fields:   []notifycontent.Field{{Label: "From", Value: "10086"}},
				Body:     "Hi",
			},
			want: content{Title: "Incoming SMS from 10086", Body: "Hi"},
		},
		{
			name: "details stand in for a missing body",
			msg: notifycontent.Message{
				Subject: "Incoming Call",
				Fields:  []notifycontent.Field{{Label: "Modem", Value: "Office"}},
			},
			want: content{Title: "Incoming Call", Body: "Modem: Office"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := render(tt.msg); got != tt.want {
				t.Fatalf("render() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
