package message

import "strings"

func normalizeSMSAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrRecipientRequired
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == ' ', r == '-', r == '.', r == '(', r == ')':
		default:
			return "", ErrRecipientInvalid
		}
	}
	recipient := b.String()
	if recipient == "" {
		return "", ErrRecipientRequired
	}
	if recipient == "+" {
		return "", ErrRecipientInvalid
	}
	return recipient, nil
}
