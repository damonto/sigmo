package telegram

import (
	"fmt"
	"strings"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

const parseModeMarkdownV2 = "MarkdownV2"

type content struct {
	Text      string
	ParseMode string
}

func render(ev notifyevent.Event) (content, error) {
	switch ev := ev.(type) {
	case notifyevent.OTPEvent:
		code := strings.TrimSpace(ev.Code)
		return content{
			Text: fmt.Sprintf(
				"*Sigmo Login*\nVerification code\n\n`%s`",
				escapeMarkdownV2(code),
			),
			ParseMode: parseModeMarkdownV2,
		}, nil
	case notifyevent.SMSEvent:
		return content{
			Text: fmt.Sprintf(
				"*%s*\n\n*From:* %s\n*To:* %s\n*Modem:* %s\n*Time:* %s\n\n*Message:*\n%s",
				escapeMarkdownV2(ev.DirectionLabel()),
				escapeMarkdownV2(strings.TrimSpace(ev.From)),
				escapeMarkdownV2(strings.TrimSpace(ev.To)),
				escapeMarkdownV2(strings.TrimSpace(ev.Modem)),
				escapeMarkdownV2(ev.DisplayTimestamp()),
				escapeMarkdownV2(ev.DisplayText()),
			),
			ParseMode: parseModeMarkdownV2,
		}, nil
	case notifyevent.CallEvent:
		return content{
			Text: fmt.Sprintf(
				"*%s*\n\n*From:* %s\n*Modem:* %s\n*Time:* %s",
				escapeMarkdownV2(ev.DirectionLabel()),
				escapeMarkdownV2(strings.TrimSpace(ev.From)),
				escapeMarkdownV2(strings.TrimSpace(ev.Modem)),
				escapeMarkdownV2(ev.DisplayTimestamp()),
			),
			ParseMode: parseModeMarkdownV2,
		}, nil
	default:
		return content{}, fmt.Errorf("rendering telegram content for %q: unsupported event", ev.Kind())
	}
}

var markdownV2Escaper = strings.NewReplacer(
	"\\", "\\\\",
	"_", "\\_",
	"*", "\\*",
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"~", "\\~",
	"`", "\\`",
	">", "\\>",
	"#", "\\#",
	"+", "\\+",
	"-", "\\-",
	"=", "\\=",
	"|", "\\|",
	"{", "\\{",
	"}", "\\}",
	".", "\\.",
	"!", "\\!",
)

func escapeMarkdownV2(text string) string {
	return markdownV2Escaper.Replace(text)
}
