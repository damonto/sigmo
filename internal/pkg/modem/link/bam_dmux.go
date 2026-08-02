package link

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/wwan-go/qcom"
)

// BAMDMUXLinkConfig describes one dedicated BAM-DMUX control endpoint.
// The selected QMI endpoint determines the data interface, so WDS clients on
// the link must not issue BindDataPort.
type BAMDMUXLinkConfig struct {
	ControlPort   string
	InterfaceName string
}

// BAMDMUXPDNConfig describes one WDS packet-data leg on a BAM-DMUX link.
type BAMDMUXPDNConfig struct {
	APN          string
	IPPreference qcom.WDSIPPreference
	ProfileIndex uint8
}

// BAMDMUXLink owns the shared WDA raw-IP client and all WDS packet-data legs
// using one QMI control endpoint. Dual-stack calls must share this link: this
// firmware permits one held WDA client per control endpoint.
type BAMDMUXLink struct {
	mu            sync.Mutex
	client        *qcom.Client
	pdns          []*qcom.PDNSession
	InterfaceName string
}

type bamDMUXCloseOps struct {
	pdn    func(*qcom.PDNSession) error
	client func(*qcom.Client) error
}

// OpenBAMDMUXLink opens a QMI endpoint that is mapped one-to-one to a
// BAM-DMUX network interface and holds its WDA raw-IP client for the lifetime
// of all packet-data legs.
func OpenBAMDMUXLink(ctx context.Context, cfg BAMDMUXLinkConfig) (*BAMDMUXLink, error) {
	cfg.ControlPort = strings.TrimSpace(cfg.ControlPort)
	cfg.InterfaceName = strings.TrimSpace(cfg.InterfaceName)
	if cfg.ControlPort == "" {
		return nil, errors.New("BAM-DMUX QMI control port is required")
	}
	if cfg.InterfaceName == "" {
		return nil, errors.New("BAM-DMUX interface is required")
	}
	if cfg.ControlPort != modem.Qualcomm410InternetQMI || cfg.InterfaceName != modem.Qualcomm410InternetInterface {
		return nil, fmt.Errorf("unsupported Qualcomm 410 Internet layout %s/%s", cfg.ControlPort, cfg.InterfaceName)
	}
	if err := modem.ValidateQualcomm410Layout(); err != nil {
		return nil, err
	}

	client, err := wwan.OpenQMIClient(ctx, wwan.QMIClientConfig{Device: cfg.ControlPort})
	if err != nil {
		return nil, fmt.Errorf("open BAM-DMUX QMI proxy %s: %w", cfg.ControlPort, err)
	}
	if err := ensureBAMDMUXRawIP(ctx, client); err != nil {
		return nil, errors.Join(fmt.Errorf("set BAM-DMUX raw IP: %w", err), client.Close())
	}
	return &BAMDMUXLink{client: client, InterfaceName: cfg.InterfaceName}, nil
}

// OpenPDN starts one IPv4 or IPv6 packet-data leg on the shared link.
func (l *BAMDMUXLink) OpenPDN(ctx context.Context, cfg BAMDMUXPDNConfig) (qcom.PDNInfo, error) {
	if l == nil {
		return qcom.PDNInfo{}, errors.New("BAM-DMUX link is closed")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client == nil {
		return qcom.PDNInfo{}, errors.New("BAM-DMUX link is closed")
	}
	if cfg.IPPreference != qcom.WDSIPPreferenceIPv4 && cfg.IPPreference != qcom.WDSIPPreferenceIPv6 {
		return qcom.PDNInfo{}, errors.New("BAM-DMUX IP preference is required")
	}
	if cfg.ProfileIndex == 0 && strings.TrimSpace(cfg.APN) != "" {
		profileIndex, profileErr := wdsProfileIndex(ctx, l.client, cfg.APN, cfg.IPPreference)
		switch {
		case profileErr == nil:
			cfg.ProfileIndex = profileIndex
		case !errors.Is(profileErr, qcom.ErrWDSProfileNotFound):
			return qcom.PDNInfo{}, fmt.Errorf("find BAM-DMUX profile: %w", profileErr)
		}
	}
	pdn, err := l.client.OpenPDN(ctx, qcom.PDNConfig{
		APN:          cfg.APN,
		IPPreference: cfg.IPPreference,
		ProfileIndex: cfg.ProfileIndex,
	})
	if err != nil {
		return qcom.PDNInfo{}, err
	}
	l.pdns = append(l.pdns, pdn)
	return pdn.Info(), nil
}

func ensureBAMDMUXRawIP(ctx context.Context, client *qcom.Client) error {
	format, err := client.WDADataFormat(ctx)
	if err == nil && isBAMDMUXRawIP(format) {
		return nil
	}
	if err := client.SetWDALinkLayerProtocol(ctx, qcom.WDALinkLayerRawIP); err != nil {
		format, readErr := client.WDADataFormat(ctx)
		if readErr == nil && isBAMDMUXRawIP(format) {
			return nil
		}
		return err
	}
	return nil
}

func isBAMDMUXRawIP(format qcom.WDADataFormat) bool {
	return format.LinkLayerProtocolKnown && format.LinkLayerProtocol == qcom.WDALinkLayerRawIP
}

// Close stops all packet-data legs and the shared client.
//
// Close is terminal: resources are detached before their close methods run,
// so a second call is a no-op. The underlying wwan-go resources use sync.Once
// and therefore cannot provide a meaningful retry contract themselves.
func (l *BAMDMUXLink) Close() error {
	return l.closeWith(bamDMUXCloseOps{
		pdn:    func(pdn *qcom.PDNSession) error { return pdn.Close() },
		client: func(client *qcom.Client) error { return client.Close() },
	})
}

func (l *BAMDMUXLink) closeWith(ops bamDMUXCloseOps) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	pdns := l.pdns
	client := l.client
	l.pdns = nil
	l.client = nil
	l.mu.Unlock()

	var result error
	for i := len(pdns) - 1; i >= 0; i-- {
		result = errors.Join(result, qmapStopError(ops.pdn(pdns[i])))
	}
	if client != nil {
		result = errors.Join(result, ops.client(client))
	}
	return result
}
