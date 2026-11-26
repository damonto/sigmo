package internet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookupPublicIP(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != "https://api.ip.sb/geoip" {
			t.Errorf("request = %s %s, want GET https://api.ip.sb/geoip", req.Method, req.URL)
		}
		if got := req.UserAgent(); got != "Sigmo" {
			t.Errorf("User-Agent = %q, want Sigmo", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"ip": " 203.0.113.1 ",
				"country": "United States",
				"country_code": " us ",
				"isp": " Example ISP ",
				"organization": " Example Network ",
				"asn_organization": "Example ASN Holder"
			}`)),
		}, nil
	})}

	got, err := LookupPublicIP(t.Context(), client)
	if err != nil {
		t.Fatalf("LookupPublicIP() error = %v", err)
	}
	want := PublicIPInfo{IP: "203.0.113.1", Country: "US", Organization: "Example Network", ISP: "Example ISP"}
	if got != want {
		t.Errorf("LookupPublicIP() = %+v, want %+v", got, want)
	}
}

func TestPublicIPCacheRefreshesAfterTTL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cache := &publicIPCache{}
	now := time.Now()
	clock := func() time.Time { return now }
	var calls int
	lookupErr := errors.New("provider unavailable")
	info := PublicIPInfo{IP: "2001:db8::1"}
	lookup := func(context.Context) (PublicIPInfo, error) {
		calls++
		if calls == 1 {
			return PublicIPInfo{}, lookupErr
		}
		return info, nil
	}
	if _, err := cache.get(ctx, ctx, clock, lookup); !errors.Is(err, lookupErr) {
		t.Fatalf("first lookup error = %v", err)
	}
	now = now.Add(publicIPFailureTTL - time.Second)
	if _, err := cache.get(ctx, ctx, clock, lookup); !errors.Is(err, lookupErr) || calls != 1 {
		t.Fatalf("cached failure: calls = %d, error = %v", calls, err)
	}
	now = now.Add(time.Second)
	if got, err := cache.get(ctx, ctx, clock, lookup); err != nil || got != info || calls != 2 {
		t.Fatalf("retried lookup: info = %+v, calls = %d, error = %v", got, calls, err)
	}
	now = now.Add(publicIPCacheTTL - time.Second)
	if got, err := cache.get(ctx, ctx, clock, lookup); err != nil || got != info || calls != 2 {
		t.Fatalf("cached lookup: info = %+v, calls = %d, error = %v", got, calls, err)
	}
	now = now.Add(time.Second)
	if _, err := cache.get(ctx, ctx, clock, lookup); err != nil || calls != 3 {
		t.Fatalf("expired lookup: calls = %d, error = %v", calls, err)
	}
}

func TestPublicIPCacheSharesLookupAndRetiresWithSession(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cache := &publicIPCache{}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	info := PublicIPInfo{IP: "203.0.113.1"}
	lookup := func(ctx context.Context) (PublicIPInfo, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return info, nil
		case <-ctx.Done():
			return PublicIPInfo{}, ctx.Err()
		}
	}
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	first := make(chan error, 1)
	go func() {
		_, err := cache.get(firstCtx, ctx, time.Now, lookup)
		first <- err
	}()
	<-started
	cancelFirst()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	close(release)
	if got, err := cache.get(t.Context(), ctx, time.Now, lookup); err != nil || got != info || calls.Load() != 1 {
		t.Fatalf("shared lookup: info = %+v, calls = %d, error = %v", got, calls.Load(), err)
	}
	cancel()
	if _, err := cache.get(t.Context(), ctx, time.Now, lookup); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired session still returned its cache: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
