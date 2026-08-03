package link

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/wwan-go/qcom"
)

// Qualcomm410RawIPLease owns the WDA client that keeps the modem data format in
// raw-IP, non-QMAP mode. WDS bearer ownership remains in wwan-go.
type Qualcomm410RawIPLease struct {
	mu     sync.Mutex
	client *qcom.Client
}

// OpenQualcomm410RawIPLease configures DATA5 before WDS creates a bearer. It
// keeps the WDA client alive until the Qualcomm 410 mode is disabled or invalidated.
func OpenQualcomm410RawIPLease(ctx context.Context) (*Qualcomm410RawIPLease, error) {
	if err := modem.ValidateQualcomm410Layout(); err != nil {
		return nil, err
	}

	client, err := wwan.OpenQMIClient(ctx, wwan.QMIClientConfig{Device: modem.Qualcomm410InternetQMI})
	if err != nil {
		return nil, fmt.Errorf("open BAM-DMUX QMI proxy %s: %w", modem.Qualcomm410InternetQMI, err)
	}
	if err := restoreNonQMAPWDADataFormat(ctx, client, qcom.WDALinkLayerRawIP, nil); err != nil {
		return nil, errors.Join(fmt.Errorf("set BAM-DMUX raw IP: %w", err), client.Close())
	}
	return &Qualcomm410RawIPLease{client: client}, nil
}

// Close releases the WDA data-format lease.
//
// Close is terminal: resources are detached before their close methods run,
// so a second call is a no-op. The underlying wwan-go resources use sync.Once
// and therefore cannot provide a meaningful retry contract themselves.
func (l *Qualcomm410RawIPLease) Close() error {
	return l.closeWith(func(client *qcom.Client) error { return client.Close() })
}

func (l *Qualcomm410RawIPLease) closeWith(closeClient func(*qcom.Client) error) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	client := l.client
	l.client = nil
	l.mu.Unlock()

	if client == nil {
		return nil
	}
	return closeClient(client)
}
