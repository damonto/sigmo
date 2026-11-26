package internet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestMeasureInternetSpeed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name          string
		size          SpeedTestSize
		downloadSize  int64
		uploadSize    int64
		timeout       time.Duration
		uploadStatus  int
		downloadDelta int64
	}{
		{"10 MB", 10, 8_000_000, 2_000_000, 20 * time.Second, http.StatusOK, 0},
		{"25 MB", 25, 20_000_000, 5_000_000, 50 * time.Second, http.StatusOK, 0},
		{"50 MB", 50, 40_000_000, 10_000_000, 100 * time.Second, http.StatusOK, 0},
		{"100 MB", 100, 80_000_000, 20_000_000, 200 * time.Second, http.StatusOK, 0},
		{"upload failed", 25, 20_000_000, 5_000_000, 50 * time.Second, http.StatusServiceUnavailable, 0},
		{"short download", 10, 8_000_000, 2_000_000, 20 * time.Second, http.StatusOK, -1},
		{"oversized download", 10, 8_000_000, 2_000_000, 20 * time.Second, http.StatusOK, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.size.timeout(); got != tt.timeout {
				t.Errorf("timeout = %v, want %v", got, tt.timeout)
			}
			var uploads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/__down":
					if r.Method != http.MethodGet || r.URL.Query().Get("bytes") != strconv.FormatInt(tt.downloadSize, 10) {
						t.Errorf("unexpected download request: %s %s", r.Method, r.URL)
					}
					_, _ = io.CopyN(w, speedTestPayload{}, tt.downloadSize+tt.downloadDelta)
				case "/__up":
					uploads.Add(1)
					if r.Method != http.MethodPost || r.ContentLength != tt.uploadSize {
						t.Errorf("upload = %s %d bytes", r.Method, r.ContentLength)
					}
					if got := r.URL.Query().Get("bytes"); got != strconv.FormatInt(tt.uploadSize, 10) {
						t.Errorf("upload bytes parameter = %q, want %d", got, tt.uploadSize)
					}
					if n, err := io.Copy(io.Discard, r.Body); err != nil || n != tt.uploadSize {
						t.Errorf("upload body = %d bytes, %v", n, err)
					}
					w.WriteHeader(tt.uploadStatus)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			result, err := measureInternetSpeed(t.Context(), srv.Client(), srv.URL, tt.size)
			if tt.downloadDelta == 0 && tt.uploadStatus == http.StatusOK {
				if err != nil || result.DownloadBPS <= 0 || result.UploadBPS <= 0 || result.TestedAt.IsZero() {
					t.Fatalf("measurement = %+v, %v", result, err)
				}
			} else if err == nil || result != (storage.InternetSpeedTest{}) {
				t.Fatalf("failed measurement = %+v, %v", result, err)
			}
			var wantUploads int32
			if tt.downloadDelta == 0 {
				wantUploads = 1
			}
			if got := uploads.Load(); got != wantUploads {
				t.Errorf("upload requests = %d, want %d", got, wantUploads)
			}
		})
	}
}

func TestRunSpeedTestKeepsLastSuccessfulResultForSIM(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	connector, modem, session := speedTestConnection(t)
	iccid := modem.SIMIdentity().ICCID
	previous := storage.InternetSpeedTest{DownloadBPS: 12_000_000, UploadBPS: 2_000_000, TestedAt: time.Unix(1000, 0).UTC()}
	if err := connector.state.SaveInternetSpeedTest(ctx, iccid, previous); err != nil {
		t.Fatal(err)
	}
	latest := storage.InternetSpeedTest{DownloadBPS: 32_000_000, UploadBPS: 4_000_000, TestedAt: time.Unix(2000, 0).UTC()}
	measurementErr := errors.New("upload unavailable")
	const size SpeedTestSize = 50
	failedMeasurement := func(context.Context, *Connection, SpeedTestSize) (storage.InternetSpeedTest, error) {
		return storage.InternetSpeedTest{}, measurementErr
	}
	if _, err := connector.runSpeedTest(ctx, modem, iccid, size, failedMeasurement); !errors.Is(err, measurementErr) {
		t.Fatalf("failed test: %v", err)
	}
	if status, err := connector.LatestSpeedTest(ctx, modem); err != nil || status.Result == nil || *status.Result != previous {
		t.Fatalf("latest result after failure = %+v, %v", status, err)
	}
	if _, err := connector.runSpeedTest(ctx, modem, "8986000000000000002", size, failedMeasurement); !errors.Is(err, mmodem.ErrSIMChanged) {
		t.Fatalf("stale SIM test: %v", err)
	}
	started, complete := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := connector.runSpeedTest(ctx, modem, iccid, size, func(ctx context.Context, connection *Connection, selected SpeedTestSize) (storage.InternetSpeedTest, error) {
			if selected != size {
				t.Errorf("measurement size = %v, want %v", selected, size)
			}
			if connection.InterfaceName != "test-wwan" || len(connection.DNS) != 1 || connection.DNS[0] != "192.0.2.1" {
				t.Errorf("wrong connection for measurement: %+v", connection)
			}
			close(started)
			select {
			case <-complete:
				return latest, nil
			case <-ctx.Done():
				return storage.InternetSpeedTest{}, ctx.Err()
			}
		})
		finished <- err
	}()
	select {
	case <-started:
	case err := <-finished:
		t.Fatalf("test did not start: %v", err)
	}
	if _, err := connector.runSpeedTest(ctx, modem, iccid, size, failedMeasurement); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("parallel test: %v", err)
	}
	close(complete)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}

	if _, err := connector.runSpeedTest(ctx, modem, iccid, size, func(context.Context, *Connection, SpeedTestSize) (storage.InternetSpeedTest, error) {
		release, err := modem.ReserveSIMSlot(ctx)
		if err != nil {
			return storage.InternetSpeedTest{}, err
		}
		modem.InvalidateSIMActivation()
		release()
		return previous, nil
	}); !errors.Is(err, mmodem.ErrSIMChanged) {
		t.Fatalf("changed SIM activation: %v", err)
	}
	if _, err := connector.runSpeedTest(ctx, modem, iccid, size, func(ctx context.Context, _ *Connection, _ SpeedTestSize) (storage.InternetSpeedTest, error) {
		session.retire()
		<-ctx.Done()
		return previous, ctx.Err()
	}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("retired connection: %v", err)
	}
	if status, err := connector.LatestSpeedTest(ctx, modem); err != nil || status.Result == nil || *status.Result != latest {
		t.Fatalf("latest result = %+v, %v", status, err)
	}
	other := &mmodem.Modem{EquipmentIdentifier: modem.EquipmentIdentifier, SIM: &mmodem.SIM{Identifier: "8986000000000000002"}}
	if status, err := connector.LatestSpeedTest(ctx, other); err != nil || status.ICCID != other.SIM.Identifier || status.Result != nil {
		t.Fatalf("result on another SIM = %+v, %v", status, err)
	}
}

func speedTestConnection(t *testing.T) (*Connector, *mmodem.Modem, *connectionDiagnostics) {
	t.Helper()
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	connector.readLinkBytes = func(string) (netlink.LinkBytes, error) { return netlink.LinkBytes{}, nil }
	modem := &mmodem.Modem{EquipmentIdentifier: "test-modem", SIM: &mmodem.SIM{Identifier: "8986000000000000001"}}
	tracked := trackedConnection{profileID: modem.SIMIdentity().ICCID, interfaceName: "test-wwan", dns: []string{"192.0.2.1"}}
	connector.startConnectionMonitoring(&tracked)
	connector.qmapConnections[modem.EquipmentIdentifier] = &qmapConnection{modem: modem, tracked: tracked}
	t.Cleanup(tracked.diagnostics.retire)
	return connector, modem, tracked.diagnostics
}
