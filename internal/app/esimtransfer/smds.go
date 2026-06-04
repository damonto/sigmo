//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/ts43-go"

	ilpa "github.com/damonto/sigmo/internal/pkg/lpa"
)

func smdsDiscoveryEventFromDelayedDownload(event ts43.DelayedDownloadEvent) ts43.SMDSDiscoveryEvent {
	return ts43.SMDSDiscoveryEvent{
		SourceICCID:        event.SourceICCID,
		TargetEID:          event.TargetEID,
		TargetIMEI:         event.TargetIMEI,
		SubscriptionResult: event.SubscriptionResult,
	}
}

func smdsDownloadConfig(ctx context.Context, client *ilpa.LPA, event ts43.SMDSDiscoveryEvent) (ts43.DownloadConfig, error) {
	if err := ctx.Err(); err != nil {
		return ts43.DownloadConfig{}, err
	}
	targetIMEI := event.TargetIMEI
	if targetIMEI == "" {
		return ts43.DownloadConfig{}, errors.New("target IMEI is required for SM-DS discovery")
	}
	imei, err := sgp22.NewIMEI(targetIMEI)
	if err != nil {
		return ts43.DownloadConfig{}, fmt.Errorf("parse target IMEI: %w", err)
	}
	entries, err := client.Discovery(imei)
	if err != nil {
		return ts43.DownloadConfig{}, fmt.Errorf("discover SM-DS profiles: %w", err)
	}
	entry, ok := firstCompleteSMDSEntry(entries)
	if !ok {
		return ts43.DownloadConfig{}, errors.New("SM-DS returned no downloadable profiles")
	}
	return ts43.DownloadConfig{SMDPFQDN: entry.Address, MatchingID: entry.EventID, IMEI: targetIMEI}, nil
}

func firstCompleteSMDSEntry(entries []*sgp22.EventEntry) (*sgp22.EventEntry, bool) {
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if entry.Address == "" || entry.EventID == "" {
			continue
		}
		return entry, true
	}
	return nil, false
}
