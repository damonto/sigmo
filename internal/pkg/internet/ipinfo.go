package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	ipInfoURL     = "https://ipinfo.io/json"
	ipInfoTimeout = 4 * time.Second
)

type IPInfo struct {
	IP           string
	Country      string
	Organization string
}

func (c *Connector) Public(ctx context.Context, modem *mmodem.Modem) (IPInfo, error) {
	connection, err := c.Current(ctx, modem)
	if err != nil {
		return IPInfo{}, err
	}
	if connection.Status != StatusConnected {
		return IPInfo{}, nil
	}
	interfaceName := strings.TrimSpace(connection.InterfaceName)
	if interfaceName == "" {
		return IPInfo{}, nil
	}
	return fetchIPInfo(ctx, interfaceName)
}

type ipInfoResponse struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

func fetchIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return IPInfo{}, errors.New("interface name is empty")
	}

	return requestIPInfo(ctx, interfaceName)
}

func requestIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	client := &http.Client{
		Timeout:   ipInfoTimeout,
		Transport: boundTransportWithTimeout(interfaceName, ipInfoTimeout),
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoURL, nil)
	if err != nil {
		return IPInfo{}, fmt.Errorf("create ipinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return IPInfo{}, fmt.Errorf("request ipinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return IPInfo{}, fmt.Errorf("ipinfo status: %d", resp.StatusCode)
	}

	var payload ipInfoResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return IPInfo{}, fmt.Errorf("decode ipinfo response: %w", err)
	}
	country := strings.ToUpper(strings.TrimSpace(payload.Country))
	return IPInfo{
		IP:           strings.TrimSpace(payload.IP),
		Country:      country,
		Organization: strings.TrimSpace(payload.Org),
	}, nil
}
