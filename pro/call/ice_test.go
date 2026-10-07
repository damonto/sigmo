//go:build ims

package call

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebRTCICEProviderFetchesCloudflareCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://speed.cloudflare.com" {
			t.Errorf("Referer = %q, want %q", r.Header.Get("Referer"), "https://speed.cloudflare.com")
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

	got, err := provider.servers(t.Context())
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

func TestWebRTCICEServersFallsBackToDirectCandidates(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "service unavailable", status: http.StatusBadGateway},
		{name: "invalid JSON", status: http.StatusOK, body: "{"},
		{name: "missing URLs", status: http.StatusOK, body: `{"urls":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			client := server.Client()
			service := &Media{ice: webRTCICEProvider{client: client, endpoint: server.URL}}

			got, err := service.webRTCICEServers(t.Context())
			if err != nil || len(got) != 0 {
				t.Fatalf("webRTCICEServers() = %v, %v, want empty servers and nil error", got, err)
			}
			public, err := service.WebRTCICEServers(t.Context())
			if err != nil || public == nil || len(public) != 0 {
				t.Fatalf("WebRTCICEServers() = %v, %v, want non-nil empty servers and nil error", public, err)
			}
		})
	}
}

func TestWebRTCICEServersPreservesContextErrors(t *testing.T) {
	tests := []struct {
		name       string
		newContext func(context.Context) (context.Context, context.CancelFunc)
		want       error
	}{
		{
			name: "canceled",
			newContext: func(parent context.Context) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(parent)
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			newContext: func(parent context.Context) (context.Context, context.CancelFunc) {
				return context.WithDeadline(parent, time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("canceled request reached credential service")
			}))
			defer server.Close()
			service := &Media{ice: webRTCICEProvider{client: server.Client(), endpoint: server.URL}}
			ctx, cancel := tt.newContext(t.Context())
			defer cancel()
			if _, err := service.webRTCICEServers(ctx); !errors.Is(err, tt.want) {
				t.Fatalf("webRTCICEServers() error = %v, want %v", err, tt.want)
			}
			if _, err := service.WebRTCICEServers(ctx); !errors.Is(err, tt.want) {
				t.Fatalf("WebRTCICEServers() error = %v, want %v", err, tt.want)
			}
		})
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

	_, err := provider.servers(t.Context())
	if !errors.Is(err, errWebRTCICEURLsRequired) {
		t.Fatalf("servers() error = %v, want %v", err, errWebRTCICEURLsRequired)
	}
}

// iceRoundTripFunc injects HTTP failures without relying on wall-clock timing.
type iceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f iceRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebRTCICEServersFallsBackOnTransportErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "credential timeout", err: context.DeadlineExceeded},
		{name: "connection error", err: io.EOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newWebRTCICEProvider()
			provider.client = &http.Client{Transport: iceRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tt.err
			})}
			service := &Media{ice: provider}
			got, err := service.webRTCICEServers(t.Context())
			if err != nil || len(got) != 0 {
				t.Fatalf("webRTCICEServers() = %v, %v, want empty servers and nil error", got, err)
			}
		})
	}
}

func TestWebRTCICEServersChecksCancellationAfterFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider := newWebRTCICEProvider()
	provider.client = &http.Client{Transport: iceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		// Cancellation can race with a successful response already in flight.
		cancel()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"urls":["stun:example.com"]}`)),
		}, nil
	})}
	service := &Media{ice: provider}
	if _, err := service.webRTCICEServers(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("webRTCICEServers() error = %v, want context.Canceled", err)
	}
}

func TestWebRTCICEServersReturnsConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		url        string
		username   string
		credential string
	}{
		{
			name: "STUN without credentials",
			body: `{"urls":["stun:example.com"]}`,
			url:  "stun:example.com",
		},
		{
			name:       "TURN with credentials",
			body:       `{"urls":["turns:example.com?transport=tcp"],"username":"sigmo","credential":"secret"}`,
			url:        "turns:example.com?transport=tcp",
			username:   "sigmo",
			credential: "secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.WriteString(w, tt.body); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			service := &Media{ice: webRTCICEProvider{client: server.Client(), endpoint: server.URL}}
			got, err := service.WebRTCICEServers(t.Context())
			if err != nil || len(got) != 1 {
				t.Fatalf("WebRTCICEServers() = %v, %v, want one server and nil error", got, err)
			}
			if len(got[0].URLs) != 1 || got[0].URLs[0] != tt.url ||
				got[0].Username != tt.username || got[0].Credential != tt.credential {
				t.Fatalf("WebRTCICEServers() = %+v, want configured URL and credentials", got)
			}
		})
	}
}
