package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

type Recipient string

type Recipients []Recipient

func (r Recipients) Int64s() ([]int64, error) {
	ids := make([]int64, 0, len(r))
	for i, raw := range r {
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return nil, fmt.Errorf("recipient %d is empty", i)
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("recipient %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r Recipients) Strings() []string {
	values := make([]string, 0, len(r))
	for _, raw := range r {
		value := strings.TrimSpace(string(raw))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func (r *Recipients) UnmarshalTOML(node *unstable.Node) error {
	switch node.Kind {
	case unstable.Array:
		recipients := make([]Recipient, 0)
		it := node.Children()
		for i := 0; it.Next(); i++ {
			recipient, err := parseRecipientNode(it.Node())
			if err != nil {
				return fmt.Errorf("recipients[%d]: %w", i, err)
			}
			recipients = append(recipients, recipient)
		}
		*r = recipients
		return nil
	case unstable.String, unstable.Integer:
		recipient, err := parseRecipientNode(node)
		if err != nil {
			return err
		}
		*r = []Recipient{recipient}
		return nil
	default:
		return errors.New("recipients must be strings or integers")
	}
}

func parseRecipientNode(node *unstable.Node) (Recipient, error) {
	switch node.Kind {
	case unstable.String:
		value := strings.TrimSpace(string(node.Data))
		if value == "" {
			return "", errors.New("recipient cannot be empty")
		}
		return Recipient(value), nil
	case unstable.Integer:
		value := strings.TrimSpace(string(node.Data))
		if value == "" {
			return "", errors.New("recipient cannot be empty")
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("recipient must be a string or integer: %w", err)
		}
		return Recipient(strconv.FormatInt(id, 10)), nil
	default:
		return "", errors.New("recipient must be a string or integer")
	}
}
