// Package content turns notification events into channel-neutral content.
//
// Compose decides once what a notification says. Channel packages only decide
// how to encode that content for their transport (MarkdownV2, HTML, a title
// and body pair), so a new event kind touches Compose alone and a new channel
// never touches Compose.
package content

import (
	"cmp"
	"fmt"
	"html/template"
	"strings"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

// Field is one labelled detail such as "From: +1 (222) 333-4444".
type Field struct {
	// Label is already localized.
	Label string
	// Value is raw text. Channels escape it for their own markup.
	Value string
	// Code marks a value the reader copies verbatim, such as a one-time
	// password. Channels that can set it apart typographically do so.
	Code bool
}

// Message is the channel-neutral content of a notification. Every string is
// raw text because the same Message is escaped differently by each channel.
type Message struct {
	// Event is the source event, for channels that forward structured data
	// instead of text.
	Event notifyevent.Event
	// Subject heads full renderings, such as "Incoming SMS".
	Subject string
	// Headline is a self-contained one-line title, such as
	// "Incoming SMS from +1 (222) 333-4444". It is empty when there is no
	// counterparty to name, and Title then falls back to Subject.
	Headline string
	// Fields are the labelled details in display order.
	Fields []Field
	// Body is the free-form content: SMS text or reminder content.
	Body string
}

// Title is the one-line title for channels with a dedicated title slot, such
// as an email subject or a push notification title.
func (m Message) Title() string {
	return cmp.Or(m.Headline, m.Subject)
}

// Summary is the text for the slot under Title. It is the body when the event
// has one, otherwise the details, so that a call, which carries no free text,
// still says who called and on which modem.
func (m Message) Summary() string {
	return cmp.Or(m.Body, m.details())
}

// Text is the complete plain-text rendering: subject, details, then body.
func (m Message) Text() string {
	return joinNonEmpty("\n\n", m.Subject, m.details(), m.Body)
}

// emailTemplate lays out the email body. Every block under the subject owns
// only its top margin, so the card padding stays even whichever block is last.
// html/template escapes every value, so nothing here calls an escaper by hand.
var emailTemplate = template.Must(template.New("email").Parse(`<div style="background:#f5f7fb;padding:24px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111827;">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #dbe2ea;border-radius:16px;padding:28px;">
<p style="margin:0 0 8px;color:#6b7280;font-size:12px;letter-spacing:0.08em;text-transform:uppercase;">Sigmo</p>
<h1 style="margin:0;font-size:24px;line-height:1.2;">{{.Subject}}</h1>
{{- with .Details}}
<div style="margin-top:18px;padding:16px 18px;border:1px solid #e5e7eb;border-radius:12px;background:#f9fafb;font-size:14px;line-height:1.7;">
{{- range $i, $f := .}}{{if $i}}<br>{{end}}<strong>{{$f.Label}}:</strong> {{$f.Value}}{{end -}}
</div>
{{- end}}
{{- range .Codes}}
<div style="margin-top:18px;">
<p style="margin:0 0 8px;color:#4b5563;font-size:14px;">{{.Label}}</p>
<div style="padding:18px 20px;border:1px solid #dbe2ea;border-radius:12px;background:#f9fafb;text-align:center;font-size:32px;font-weight:700;letter-spacing:0.24em;">{{.Value}}</div>
</div>
{{- end}}
{{- with .Body}}
<div style="margin-top:18px;padding:16px 18px;border:1px solid #dbe2ea;border-radius:12px;background:#ffffff;font-size:15px;line-height:1.7;white-space:pre-wrap;">{{.}}</div>
{{- end}}
</div>
</div>`))

// HTML is the email rendering.
func (m Message) HTML() (string, error) {
	data := struct {
		Subject string
		Details []Field
		Codes   []Field
		Body    string
	}{Subject: m.Subject, Body: m.Body}
	for _, f := range m.Fields {
		if f.Code {
			data.Codes = append(data.Codes, f)
		} else {
			data.Details = append(data.Details, f)
		}
	}

	var b strings.Builder
	if err := emailTemplate.Execute(&b, data); err != nil {
		return "", fmt.Errorf("execute email template: %w", err)
	}
	return b.String(), nil
}

func (m Message) details() string {
	lines := make([]string, 0, len(m.Fields))
	for _, f := range m.Fields {
		lines = append(lines, f.Label+": "+f.Value)
	}
	return strings.Join(lines, "\n")
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}
