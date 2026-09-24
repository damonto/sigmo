package email

import (
	"context"
	"errors"
	"fmt"
	"strings"

	notifycontent "github.com/damonto/sigmo/internal/pkg/notify/content"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/wneessen/go-mail"
)

type Sender struct {
	client     *mail.Client
	from       string
	recipients []string
}

func New(channel *settings.Channel) (*Sender, error) {
	host := strings.TrimSpace(channel.SMTPHost)
	if host == "" {
		return nil, errors.New("email smtp_host is required")
	}
	if channel.SMTPPort <= 0 {
		return nil, errors.New("email smtp_port is required")
	}
	from := strings.TrimSpace(channel.From)
	if from == "" {
		return nil, errors.New("email from is required")
	}
	recipients := channel.Recipients.Strings()
	if len(recipients) == 0 {
		return nil, errors.New("email recipients are required")
	}

	var policy tlsPolicy
	if err := policy.UnmarshalText([]byte(channel.TLSPolicy)); err != nil {
		return nil, err
	}

	options := []mail.Option{mail.WithPort(channel.SMTPPort)}
	if channel.SSL {
		options = append(options, mail.WithSSLPort(true))
	}

	username := strings.TrimSpace(channel.SMTPUsername)
	password := strings.TrimSpace(channel.SMTPPassword)
	if username != "" || password != "" {
		if username == "" || password == "" {
			return nil, errors.New("email smtp_username and smtp_password must be set together")
		}
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(username),
			mail.WithPassword(password),
		)
	}

	client, err := mail.NewClient(host, options...)
	if err != nil {
		return nil, fmt.Errorf("creating email client: %w", err)
	}
	client.SetTLSPolicy(policy.TLSPolicy)

	return &Sender{
		client:     client,
		from:       from,
		recipients: recipients,
	}, nil
}

func (s *Sender) Send(ctx context.Context, content notifycontent.Message) error {
	if len(s.recipients) == 0 {
		return errors.New("email recipients are required")
	}
	html, err := content.HTML()
	if err != nil {
		return fmt.Errorf("rendering email html: %w", err)
	}

	msg := mail.NewMsg()
	if err := msg.From(s.from); err != nil {
		return fmt.Errorf("setting email from: %w", err)
	}
	if err := msg.To(s.recipients...); err != nil {
		return fmt.Errorf("setting email recipients: %w", err)
	}
	msg.Subject(content.Title())
	msg.SetBodyString(mail.TypeTextPlain, content.Text())
	msg.AddAlternativeString(mail.TypeTextHTML, html)

	if err := s.client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}
