package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// InternetSpeedTest stores both measured rates in bits per second.
type InternetSpeedTest struct {
	DownloadBPS int64
	UploadBPS   int64
	TestedAt    time.Time
}

// LatestInternetSpeedTest reads the last successful test for an ICCID.
func (s *Store) LatestInternetSpeedTest(ctx context.Context, iccid string) (*InternetSpeedTest, error) {
	row, err := query.New(s.db).LatestInternetSpeedTest(ctx, iccid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read internet speed test: %w", err)
	}
	return &InternetSpeedTest{
		DownloadBPS: row.DownloadBps,
		UploadBPS:   row.UploadBps,
		TestedAt:    time.UnixMilli(row.TestedAt).UTC(),
	}, nil
}

// SaveInternetSpeedTest replaces both rates and the timestamp atomically.
func (s *Store) SaveInternetSpeedTest(ctx context.Context, iccid string, result InternetSpeedTest) error {
	if iccid == "" || result.DownloadBPS <= 0 || result.UploadBPS <= 0 || result.TestedAt.IsZero() {
		return errors.New("invalid internet speed test result")
	}
	err := query.New(s.db).SaveInternetSpeedTest(ctx, query.SaveInternetSpeedTestParams{
		Iccid:       iccid,
		DownloadBps: result.DownloadBPS,
		UploadBps:   result.UploadBPS,
		TestedAt:    result.TestedAt.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("save internet speed test: %w", err)
	}
	return nil
}
