package internet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	speedTestOrigin      = "https://speed.cloudflare.com"
	speedTestDialTimeout = 20 * time.Second
)

func measureBoundSpeed(ctx context.Context, connection *Connection, size SpeedTestSize) (storage.InternetSpeedTest, error) {
	transport := boundTransportWithTimeout(strings.TrimSpace(connection.InterfaceName), connection.DNS, speedTestDialTimeout)
	transport.DisableCompression = true
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	return measureInternetSpeed(ctx, client, speedTestOrigin, size)
}

func measureInternetSpeed(ctx context.Context, client *http.Client, origin string, size SpeedTestSize) (storage.InternetSpeedTest, error) {
	totalBytes := int64(size) * 1_000_000
	uploadBytes := totalBytes / 5
	download, err := speedDownload(ctx, client, origin+"/__down", totalBytes-uploadBytes)
	if err != nil {
		return storage.InternetSpeedTest{}, fmt.Errorf("measure download speed: %w", err)
	}
	upload, err := speedUpload(ctx, client, origin+"/__up", uploadBytes)
	if err != nil {
		return storage.InternetSpeedTest{}, fmt.Errorf("measure upload speed: %w", err)
	}
	return storage.InternetSpeedTest{DownloadBPS: download, UploadBPS: upload, TestedAt: time.Now().UTC().Truncate(time.Millisecond)}, nil
}

func speedDownload(ctx context.Context, client *http.Client, url string, size int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"?bytes="+strconv.FormatInt(size, 10), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, started, err := speedRequest(client, req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Read one extra byte at most to detect an unexpectedly large payload.
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, size+1))
	if err != nil {
		return 0, err
	}
	if n != size {
		return 0, fmt.Errorf("downloaded %d bytes, expected %d", n, size)
	}
	return speedRate(n, started)
}

func speedUpload(ctx context.Context, client *http.Client, url string, size int64) (int64, error) {
	payload := io.LimitReader(speedTestPayload{}, size)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"?bytes="+strconv.FormatInt(size, 10), payload)
	if err != nil {
		return 0, err
	}
	// The fixed-length body is non-rewindable, so the transport cannot replay the upload payload.
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, started, err := speedRequest(client, req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return speedRate(size, started)
}

// speedTestPayload supplies bytes for the uncompressed upload using the transport's buffers.
type speedTestPayload struct{}

func (speedTestPayload) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func speedRequest(client *http.Client, req *http.Request) (*http.Response, time.Time, error) {
	var started time.Time
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { started = time.Now() }}
	var wroteUpload chan error
	if req.Method == http.MethodPost {
		wroteUpload = make(chan error, 1)
		trace.WroteRequest = func(info httptrace.WroteRequestInfo) {
			select {
			case wroteUpload <- info.Err:
			default:
			}
		}
	}
	resp, err := client.Do(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
	if err != nil {
		return nil, time.Time{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, time.Time{}, fmt.Errorf("measurement returned HTTP status %d", resp.StatusCode)
	}
	if wroteUpload != nil {
		select {
		case err = <-wroteUpload:
		case <-req.Context().Done():
			err = req.Context().Err()
		}
		if err != nil {
			resp.Body.Close()
			return nil, time.Time{}, err
		}
	}
	return resp, started, nil
}

func speedRate(n int64, started time.Time) (int64, error) {
	elapsed := time.Since(started)
	if started.IsZero() || elapsed <= 0 {
		return 0, errors.New("measurement has no elapsed time")
	}
	return int64(float64(n*8) / elapsed.Seconds()), nil
}
