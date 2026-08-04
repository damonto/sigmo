package modem

import (
	"context"
	"errors"
	"log/slog"
	"time"

	devicewwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	simRefreshEndWait      = 3 * time.Second
	simRefreshProbeTimeout = 3 * time.Second
)

type simRefreshState struct {
	version    uint64
	inProgress bool
	notify     chan struct{}
}

func (m *Modem) startSIMRefreshWatcher(ctx context.Context) {
	if m.core.Protocol() != wwanmodem.ProtocolQMI {
		return
	}
	device, err := OpenDevice(m)
	if err != nil {
		slog.Warn("open QMI device for SIM refresh watcher", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
		return
	}
	stream, err := device.WatchSIMRefresh(ctx)
	if errors.Is(err, devicewwan.ErrUnsupported) {
		slog.Debug("QMI UIM refresh watcher is unsupported", "imei", m.EquipmentIdentifier, "generation", m.Generation())
		return
	}
	if err != nil {
		slog.Warn("start QMI UIM refresh watcher", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
	}

	m.watchWG.Add(1)
	go m.watchSIMRefresh(ctx, device, stream)
}

func (m *Modem) watchSIMRefresh(ctx context.Context, device Device, stream <-chan devicewwan.SIMRefreshEvent) {
	defer m.watchWG.Done()
	for ctx.Err() == nil {
		if stream == nil {
			var err error
			stream, err = device.WatchSIMRefresh(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if m.reportTerminalRuntimeError(err) {
					return
				}
				slog.Warn("restart QMI UIM refresh watcher", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
				if err := sleepContext(ctx, modemWatchRetryDelay); err != nil {
					return
				}
				continue
			}
		}

		err := m.consumeSIMRefreshStream(ctx, stream)
		stream = nil
		if ctx.Err() != nil {
			return
		}
		slog.Warn("QMI UIM refresh watcher stopped", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
		if err := sleepContext(ctx, modemWatchRetryDelay); err != nil {
			return
		}
	}
}

func (m *Modem) consumeSIMRefreshStream(ctx context.Context, stream <-chan devicewwan.SIMRefreshEvent) error {
	if stream == nil {
		return errors.New("QMI UIM refresh watcher returned a nil stream")
	}

	var endTimer *time.Timer
	var endTimeout <-chan time.Time
	defer func() {
		if endTimer != nil {
			endTimer.Stop()
		}
		m.finishSIMRefresh()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-endTimeout:
			endTimeout = nil
			m.finishSIMRefresh()
			m.reloadSIMAfterRefresh(ctx, "start timeout")
		case event, ok := <-stream:
			if !ok {
				return errors.New("QMI UIM refresh stream closed")
			}
			slog.Info(
				"QMI UIM refresh event",
				"imei", m.EquipmentIdentifier,
				"generation", m.Generation(),
				"stage", event.Stage,
				"mode", event.Mode,
			)
			m.publishSIMRefresh(event.Stage == devicewwan.SIMRefreshStart && simRefreshReenumeratesSIM(event.Mode))

			switch event.Stage {
			case devicewwan.SIMRefreshStart:
				if !simRefreshReenumeratesSIM(event.Mode) {
					continue
				}
				if endTimer == nil {
					endTimer = time.NewTimer(simRefreshEndWait)
				} else {
					if !endTimer.Stop() {
						select {
						case <-endTimer.C:
						default:
						}
					}
					endTimer.Reset(simRefreshEndWait)
				}
				endTimeout = endTimer.C
			case devicewwan.SIMRefreshEndWithSuccess:
				if endTimer != nil {
					endTimer.Stop()
					endTimeout = nil
				}
				m.finishSIMRefresh()
				m.reloadSIMAfterRefresh(ctx, "end with success")
			case devicewwan.SIMRefreshEndWithFailure:
				if endTimer != nil {
					endTimer.Stop()
					endTimeout = nil
				}
				m.finishSIMRefresh()
			}
		}
	}
}

func simRefreshReenumeratesSIM(mode devicewwan.SIMRefreshMode) bool {
	return mode == devicewwan.SIMRefreshReset || mode == devicewwan.SIMRefreshInitFullFCN
}

func (m *Modem) reloadSIMAfterRefresh(ctx context.Context, reason string) {
	probeCtx, cancel := context.WithTimeout(ctx, simRefreshProbeTimeout)
	defer cancel()
	info, err := m.core.SIMInfo(probeCtx)
	if err != nil {
		slog.Debug("read SIM after QMI UIM refresh", "imei", m.EquipmentIdentifier, "reason", reason, "error", err)
		return
	}
	m.applySIMInfo(info)
}

func (m *Modem) publishSIMRefresh(inProgress bool) {
	if m == nil {
		return
	}
	m.simRefreshMu.Lock()
	previous := m.simRefresh.notify
	m.simRefresh.version++
	m.simRefresh.inProgress = inProgress
	m.simRefresh.notify = make(chan struct{})
	m.simRefreshMu.Unlock()
	if previous != nil {
		close(previous)
	}
}

func (m *Modem) finishSIMRefresh() {
	if m == nil {
		return
	}
	m.simRefreshMu.Lock()
	if !m.simRefresh.inProgress {
		m.simRefreshMu.Unlock()
		return
	}
	previous := m.simRefresh.notify
	m.simRefresh.version++
	m.simRefresh.inProgress = false
	m.simRefresh.notify = make(chan struct{})
	m.simRefreshMu.Unlock()
	if previous != nil {
		close(previous)
	}
}

func (m *Modem) currentSIMRefresh() (version uint64, inProgress bool, notify <-chan struct{}) {
	if m == nil {
		return 0, false, nil
	}
	m.simRefreshMu.Lock()
	defer m.simRefreshMu.Unlock()
	if m.simRefresh.notify == nil {
		m.simRefresh.notify = make(chan struct{})
	}
	return m.simRefresh.version, m.simRefresh.inProgress, m.simRefresh.notify
}
