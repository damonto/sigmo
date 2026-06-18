//go:build wifi_calling

package call

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebRTCICEProviderFetchesCloudflareCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://speed.cloudflare.com" {
			t.Fatalf("Referer = %q, want %q", r.Header.Get("Referer"), "https://speed.cloudflare.com")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"urls": [
				"stun:stun.cloudflare.com:3478",
				"turn:turn.cloudflare.com:3478?transport=udp",
				"turn:turn.cloudflare.com:3478?transport=tcp",
				"turns:turn.cloudflare.com:5349?transport=tcp"
			],
			"username": "sigmo",
			"credential": "secret"
		}`))
	}))
	defer server.Close()

	provider := newWebRTCICEProvider()
	provider.client = server.Client()
	provider.endpoint = server.URL

	got, err := provider.servers(context.Background())
	if err != nil {
		t.Fatalf("servers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("servers() len = %d, want 1", len(got))
	}
	turn := got[0]
	if len(turn.URLs) != 4 {
		t.Fatalf("TURN urls len = %d, want 4", len(turn.URLs))
	}
	if turn.Username != "sigmo" || turn.Credential != "secret" {
		t.Fatalf("TURN auth = %q/%q, want sigmo/secret", turn.Username, turn.Credential)
	}
}

func TestWebRTCICEServersReturnsCloudflareErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()

	service := NewMedia(nil)
	service.ice.client = server.Client()
	service.ice.endpoint = server.URL

	got, err := service.webRTCICEServers(context.Background())
	if err == nil {
		t.Fatalf("webRTCICEServers() error = nil, want error")
	}
	if got != nil {
		t.Fatalf("webRTCICEServers() = %v, want nil", got)
	}
}

func TestWebRTCICEProviderRejectsMissingURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"urls":[],"username":"sigmo","credential":"secret"}`))
	}))
	defer server.Close()

	provider := newWebRTCICEProvider()
	provider.client = server.Client()
	provider.endpoint = server.URL

	_, err := provider.servers(context.Background())
	if !errors.Is(err, errWebRTCICEURLsRequired) {
		t.Fatalf("servers() error = %v, want %v", err, errWebRTCICEURLsRequired)
	}
}
