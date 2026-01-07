package notify

import (
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/wneessen/go-mail"
)

const defaultEmailSubject = "Sigmo Notification"

type Email struct {
	client     *mail.Client
	from       string
	subject    string
	recipients []string
}

func NewEmail(cfg *config.Channel) (*Email, error) {
	host := strings.TrimSpace(cfg.SMTPHost)
	if host == "" {
		return nil, errors.New("email smtp_host is required")
	}
	if cfg.SMTPPort <= 0 {
		return nil, errors.New("email smtp_port is required")
	}
	from := strings.TrimSpace(cfg.From)
	if from == "" {
		return nil, errors.New("email from is required")
	}
	recipients := cfg.Recipients.Strings()
	if len(recipients) == 0 {
		return nil, errors.New("email recipients is required")
	}
	subject := strings.TrimSpace(cfg.Subject)
	if subject == "" {
		subject = defaultEmailSubject
	}

	tlsPolicy, err := parseTLSPolicy(cfg.TLSPolicy)
	if err != nil {
		return nil, err
	}

	options := []mail.Option{}
	if cfg.SSL {
		options = append(options, mail.WithSSLPort(true))
	}
	options = append(options, mail.WithPort(cfg.SMTPPort))

	username := strings.TrimSpace(cfg.SMTPUsername)
	password := strings.TrimSpace(cfg.SMTPPassword)
	if username != "" || password != "" {
		if username == "" || password == "" {
			return nil, errors.New("email smtp_username and smtp_password must be set together")
		}
		options = append(
			options,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(username),
			mail.WithPassword(password),
		)
	}

	client, err := mail.NewClient(host, options...)
	if err != nil {
		return nil, fmt.Errorf("creating email client: %w", err)
	}
	client.SetTLSPolicy(tlsPolicy)

	return &Email{
		client:     client,
		from:       from,
		subject:    subject,
		recipients: recipients,
	}, nil
}

func (e *Email) Send(message Message) error {
	if message == nil {
		return errors.New("email message is required")
	}
	if len(e.recipients) == 0 {
		return errors.New("email recipients are required")
	}

	msg := mail.NewMsg()
	if err := msg.From(e.from); err != nil {
		return fmt.Errorf("setting email from: %w", err)
	}
	if err := msg.To(e.recipients...); err != nil {
		return fmt.Errorf("setting email recipients: %w", err)
	}
	msg.Subject(e.subject)
	msg.SetBodyString(mail.TypeTextPlain, message.String())

	if err := e.client.DialAndSend(msg); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}

func parseTLSPolicy(raw string) (mail.TLSPolicy, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "mandatory":
		return mail.TLSMandatory, nil
	case "opportunistic":
		return mail.TLSOpportunistic, nil
	case "none", "notls", "no_tls":
		return mail.NoTLS, nil
	default:
		return mail.TLSMandatory, fmt.Errorf("unsupported email tls_policy: %q", raw)
	}
}
