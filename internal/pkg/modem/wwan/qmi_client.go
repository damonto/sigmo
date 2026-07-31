package wwan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/wwan-go/qcom"
	"github.com/damonto/wwan-go/qcom/qmi"
)

// QMIClientConfig selects a QMI device and, when non-zero, a SIM slot.
type QMIClientConfig struct {
	Device string
	Slot   uint8
}

// OpenQMIClient opens an auto-detected QMI transport lease.
func OpenQMIClient(ctx context.Context, cfg QMIClientConfig) (*qcom.Client, error) {
	cfg.Device = strings.TrimSpace(cfg.Device)
	if cfg.Device == "" {
		return nil, errors.New("QMI device is required")
	}
	transport, err := qmi.Open(ctx, qmi.WithAutoDetect(cfg.Device))
	if err != nil {
		return nil, fmt.Errorf("open QMI transport: %w", err)
	}

	var opts []qcom.Option
	if cfg.Slot != 0 {
		opts = append(opts, qcom.WithSlot(cfg.Slot))
	}
	client, err := qcom.NewClient(transport, opts...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create QMI client: %w", err), transport.Close())
	}
	return client, nil
}
