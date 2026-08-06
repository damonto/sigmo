package network

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/labstack/echo/v5"
)

func TestNetworkScanHTTPResourceLifecycle(t *testing.T) {
	store := newTestScanTaskStore(t)
	started := make(chan struct{})
	release := make(chan struct{})
	store.scanFunc = func(ctx context.Context, _ *mmodem.Modem) ([]NetworkResponse, error) {
		close(started)
		select {
		case <-release:
			return []NetworkResponse{{OperatorCode: "00101"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	h := &Handler{
		registry: staticModemFinder{modem: modem},
		networks: &network{scans: store},
	}
	e := echo.New()
	e.POST("/api/v1/modems/:id/network-scans", h.StartNetworkScan)
	e.GET("/api/v1/modems/:id/network-scans/:scanID", h.GetNetworkScan)

	first := serveNetworkScanRequest(t, e, http.MethodPost, "/api/v1/modems/modem-1/network-scans")
	if first.Code != http.StatusCreated {
		t.Fatalf("first POST status = %d, want %d", first.Code, http.StatusCreated)
	}
	var created NetworkScanResponse
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode first POST: %v", err)
	}
	if created.Status != networkScanStatusRunning {
		t.Fatalf("first POST response = %+v, want running", created)
	}
	if got := first.Header().Get("Location"); got != "/api/v1/modems/modem-1/network-scans/"+created.ID {
		t.Fatalf("Location = %q", got)
	}
	assertNetworkScanHeaders(t, first, true)
	<-started

	running := serveNetworkScanRequest(t, e, http.MethodGet, "/api/v1/modems/modem-1/network-scans/"+created.ID)
	if running.Code != http.StatusOK {
		t.Fatalf("running GET status = %d, want %d", running.Code, http.StatusOK)
	}
	assertNetworkScanHeaders(t, running, true)

	shared := serveNetworkScanRequest(t, e, http.MethodPost, "/api/v1/modems/modem-1/network-scans")
	if shared.Code != http.StatusOK {
		t.Fatalf("shared POST status = %d, want %d", shared.Code, http.StatusOK)
	}
	var sharedResponse NetworkScanResponse
	if err := json.Unmarshal(shared.Body.Bytes(), &sharedResponse); err != nil {
		t.Fatalf("decode shared POST: %v", err)
	}
	if sharedResponse.ID != created.ID {
		t.Fatalf("shared scan ID = %q, want %q", sharedResponse.ID, created.ID)
	}
	assertNetworkScanHeaders(t, shared, true)

	close(release)
	store.mu.Lock()
	task := store.tasks[created.ID]
	store.mu.Unlock()
	if task == nil {
		t.Fatalf("scan task %q not found", created.ID)
	}
	if _, err := store.wait(t.Context(), task); err != nil {
		t.Fatalf("wait() error = %v", err)
	}

	completed := serveNetworkScanRequest(t, e, http.MethodGet, "/api/v1/modems/modem-1/network-scans/"+created.ID)
	if completed.Code != http.StatusOK {
		t.Fatalf("completed GET status = %d, want %d", completed.Code, http.StatusOK)
	}
	var completedResponse NetworkScanResponse
	if err := json.Unmarshal(completed.Body.Bytes(), &completedResponse); err != nil {
		t.Fatalf("decode completed GET: %v", err)
	}
	if completedResponse.Status != networkScanStatusCompleted || len(completedResponse.Networks) != 1 {
		t.Fatalf("completed response = %+v", completedResponse)
	}
	assertNetworkScanHeaders(t, completed, false)

	cached := serveNetworkScanRequest(t, e, http.MethodPost, "/api/v1/modems/modem-1/network-scans")
	if cached.Code != http.StatusOK {
		t.Fatalf("cached POST status = %d, want %d", cached.Code, http.StatusOK)
	}
	var cachedResponse NetworkScanResponse
	if err := json.Unmarshal(cached.Body.Bytes(), &cachedResponse); err != nil {
		t.Fatalf("decode cached POST: %v", err)
	}
	if cachedResponse.ID != created.ID || cachedResponse.Status != networkScanStatusCompleted {
		t.Fatalf("cached response = %+v, want completed task %q", cachedResponse, created.ID)
	}
	assertNetworkScanHeaders(t, cached, false)
}

type staticModemFinder struct {
	modem *mmodem.Modem
	err   error
}

func (f staticModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, f.err
}

func serveNetworkScanRequest(t *testing.T, e *echo.Echo, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func assertNetworkScanHeaders(t *testing.T, rec *httptest.ResponseRecorder, running bool) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	wantRetryAfter := ""
	if running {
		wantRetryAfter = "1"
	}
	if got := rec.Header().Get("Retry-After"); got != wantRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, wantRetryAfter)
	}
}
