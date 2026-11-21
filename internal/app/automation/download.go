package automation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/damonto/sigmo/internal/pkg/internet"
)

const downloadURL = "https://speed.cloudflare.com/__down"

func downloadTraffic(ctx context.Context, connection *internet.Connection, bytes int64) (int64, error) {
	if connection.InterfaceName == "" {
		return 0, errors.New("cellular interface is missing")
	}
	underlay, err := internet.NewBoundUnderlay(connection.InterfaceName, connection.DNS)
	if err != nil {
		return 0, err
	}
	client := &http.Client{
		Transport:     &http.Transport{DialContext: underlay.DialContext, DisableCompression: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	return readDownload(ctx, client, downloadURL, bytes)
}

func readDownload(ctx context.Context, client *http.Client, endpoint string, bytes int64) (int64, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return 0, err
	}
	params := target.Query()
	params.Set("bytes", strconv.FormatInt(bytes, 10))
	target.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request automation download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("automation download status %d", resp.StatusCode)
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return 0, errors.New("unexpected automation download encoding")
	}
	if resp.ContentLength >= 0 && resp.ContentLength != bytes {
		return 0, errors.New("unexpected automation download content length")
	}
	n, err := io.CopyN(io.Discard, resp.Body, bytes)
	if err != nil {
		return n, fmt.Errorf("read automation download: %w", err)
	}
	return n, nil
}
