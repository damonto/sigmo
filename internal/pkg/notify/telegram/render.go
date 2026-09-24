package telegram

import (
	"strings"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
)

const parseModeMarkdownV2 = "MarkdownV2"

type content struct {
	Text      string
	ParseMode string
}

func render(msg notifycontent.Message) content {
	sections := []string{"*" + escapeMarkdownV2(msg.Subject) + "*"}
	if len(msg.Fields) > 0 {
		lines := make([]string, 0, len(msg.Fields))
		for _, f := range msg.Fields {
			value := escapeMarkdownV2(f.Value)
			if f.Code {
				// Telegram copies monospace text on tap, which is what a reader
				// wants to do with a verification code.
				value = "`" + value + "`"
			}
			lines = append(lines, "*"+escapeMarkdownV2(f.Label)+":* "+value)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if msg.Body != "" {
		sections = append(sections, escapeMarkdownV2(msg.Body))
	}
	return content{Text: strings.Join(sections, "\n\n"), ParseMode: parseModeMarkdownV2}
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
