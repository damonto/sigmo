package wwan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
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

// OpenQMISession opens a reusable QMI session through an already-open modem.
// This preserves the modem generation's resolved direct or proxy access method.
func OpenQMISession(cfg Config, modem *wwanmodem.Modem) (*Session, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.PortType != PortTypeQMI {
		return nil, ErrUnsupported
	}
	if modem == nil {
		return nil, errors.New("QMI modem is required")
	}
	backend := newQMISessionWithQCOMOpener(cfg, modem.QMIClient)
	return &Session{deviceOperations: deviceOperations{backend: backend}}, nil
}
