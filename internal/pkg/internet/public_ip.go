package internet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	publicIPLookupURL     = "https://api.ip.sb/geoip"
	publicIPLookupTimeout = 15 * time.Second
)

// PublicIPInfo describes the public IP address and its associated network.
type PublicIPInfo struct {
	IP           string
	Country      string
	Organization string
}

// Public looks up the public IP through the modem's active Internet connection.
func (c *Connector) Public(ctx context.Context, modem *mmodem.Modem) (PublicIPInfo, error) {
	connection, err := c.Current(ctx, modem)
	if err != nil {
		return PublicIPInfo{}, err
	}
	if connection.Status != StatusConnected {
		return PublicIPInfo{}, nil
	}
	interfaceName := strings.TrimSpace(connection.InterfaceName)
	if interfaceName == "" {
		return PublicIPInfo{}, nil
	}

	client := &http.Client{
		Timeout:   publicIPLookupTimeout,
		Transport: boundTransportWithTimeout(interfaceName, connection.DNS, publicIPLookupTimeout),
	}
	defer client.CloseIdleConnections()
	return LookupPublicIP(ctx, client)
}

// LookupPublicIP finds the public IP address used by the supplied HTTP client.
func LookupPublicIP(ctx context.Context, client *http.Client) (PublicIPInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicIPLookupURL, nil)
	if err != nil {
		return PublicIPInfo{}, fmt.Errorf("create public IP lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// IP.SB may reject the default Go HTTP user agent.
	req.Header.Set("User-Agent", "Sigmo")

	resp, err := client.Do(req)
	if err != nil {
		return PublicIPInfo{}, fmt.Errorf("look up public IP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return PublicIPInfo{}, fmt.Errorf("public IP lookup returned HTTP status %d", resp.StatusCode)
	}

	var payload struct {
		IP           string `json:"ip"`
		CountryCode  string `json:"country_code"`
		Organization string `json:"organization"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return PublicIPInfo{}, fmt.Errorf("decode public IP lookup response: %w", err)
	}
	return PublicIPInfo{
		IP:           strings.TrimSpace(payload.IP),
		Country:      strings.ToUpper(strings.TrimSpace(payload.CountryCode)),
		Organization: strings.TrimSpace(payload.Organization),
	}, nil
}
