package lpa

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type SMDPAddress struct {
	url url.URL
}

func (a SMDPAddress) MarshalText() ([]byte, error) {
	if a.url.Host == "" {
		return nil, errors.New("smdp is required")
	}
	return []byte(a.url.String()), nil
}

func (a *SMDPAddress) UnmarshalText(text []byte) error {
	raw := strings.TrimSpace(string(text))
	if raw == "" {
		return errors.New("smdp is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid smdp %q", text)
	}
	a.url = url.URL{Scheme: "https", Host: parsed.Host}
	return nil
}

func (a SMDPAddress) URL() *url.URL {
	value := a.url
	return &value
}
