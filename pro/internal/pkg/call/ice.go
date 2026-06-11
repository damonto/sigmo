//go:build wifi_calling

package call

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	cloudflareTURNEndpoint = "https://speed.cloudflare.com/turn-creds"
	webRTCICEHTTPTimeout   = 5 * time.Second
)

var errWebRTCICEURLsRequired = errors.New("WebRTC ICE server URLs are required")

type webRTCICEProvider struct {
	client   *http.Client
	endpoint string
}

type cloudflareTURNCredentials struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

func newWebRTCICEProvider() webRTCICEProvider {
	return webRTCICEProvider{
		client:   &http.Client{Timeout: webRTCICEHTTPTimeout},
		endpoint: cloudflareTURNEndpoint,
	}
}

func (m *Media) WebRTCICEServers(ctx context.Context) ([]WebRTCICEServer, error) {
	servers, err := m.webRTCICEServers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WebRTCICEServer, 0, len(servers))
	for _, server := range servers {
		credential, _ := server.Credential.(string)
		out = append(out, WebRTCICEServer{
			URLs:       slices.Clone(server.URLs),
			Username:   server.Username,
			Credential: credential,
		})
	}
	return out, nil
}

func (m *Media) webRTCICEServers(ctx context.Context) ([]webrtc.ICEServer, error) {
	return m.ice.servers(ctx)
}

func (p *webRTCICEProvider) servers(ctx context.Context) ([]webrtc.ICEServer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Cloudflare TURN request: %w", err)
	}
	req.Header.Set("Referer", "https://speed.cloudflare.com")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Cloudflare TURN credentials: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request Cloudflare TURN credentials: status %d", resp.StatusCode)
	}

	var credentials cloudflareTURNCredentials
	if err := json.NewDecoder(resp.Body).Decode(&credentials); err != nil {
		return nil, fmt.Errorf("decode Cloudflare TURN credentials: %w", err)
	}
	if len(credentials.URLs) == 0 {
		return nil, errWebRTCICEURLsRequired
	}
	return []webrtc.ICEServer{
		{
			URLs:           credentials.URLs,
			Username:       credentials.Username,
			Credential:     credentials.Credential,
			CredentialType: webrtc.ICECredentialTypePassword,
		},
	}, nil
}
