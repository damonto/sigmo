package gotify

import notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"

type content struct {
	Title string
	Body  string
}

func render(msg notifycontent.Message) content {
	return content{Title: msg.Title(), Body: msg.Summary()}
}
