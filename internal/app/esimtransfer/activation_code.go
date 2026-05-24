//go:build esim_transfer

package esimtransfer

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func parseSMDP(raw string) (*url.URL, error) {
	smdp := strings.TrimSpace(raw)
	if smdp == "" {
		return nil, errors.New("smdp is required")
	}
	if !strings.Contains(smdp, "://") {
		smdp = "https://" + smdp
	}
	parsed, err := url.Parse(smdp)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid smdp %q", raw)
	}
	return &url.URL{Scheme: "https", Host: parsed.Host}, nil
}
