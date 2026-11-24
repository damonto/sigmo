package internet

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

// TrafficStatus reports whether a managed Internet connection is active and can be sampled.
type TrafficStatus struct {
	Connected bool
	Sample    *TrafficSample
}

// TrafficSample counts interface bytes since Sigmo established or recovered the connection.
type TrafficSample struct {
	SessionID           string
	ElapsedMilliseconds int64
	RXBytes             uint64
	TXBytes             uint64
}

// The modem operation lock protects sampling and replacement of this meter.
type connectionTraffic struct {
	id      string
	started time.Time
	last    netlink.LinkBytes
	rx      uint64
	tx      uint64
}

func (c *Connector) linkBytes(name string) (netlink.LinkBytes, error) {
	if c.readLinkBytes != nil {
		return c.readLinkBytes(name)
	}
	return netlink.ReadLinkBytes(name)
}

func (c *Connector) startConnectionTraffic(name string) *connectionTraffic {
	// A failed read leaves the first successful sample to establish the baseline.
	baseline, _ := c.linkBytes(name)
	return &connectionTraffic{id: rand.Text(), started: time.Now(), last: baseline}
}

func (meter *connectionTraffic) sample(current netlink.LinkBytes) *TrafficSample {
	if meter.last.Index != 0 && current.Index != meter.last.Index {
		return nil
	}
	if meter.last.Index != 0 {
		// A driver reset may clear either counter. Retain the bytes already
		// measured and include the bytes counted since the reset.
		meter.rx += counterAdvance(meter.last.RXBytes, current.RXBytes)
		meter.tx += counterAdvance(meter.last.TXBytes, current.TXBytes)
	}
	meter.last = current
	return &TrafficSample{
		SessionID: meter.id, ElapsedMilliseconds: time.Since(meter.started).Milliseconds(),
		RXBytes: meter.rx, TXBytes: meter.tx,
	}
}

func counterAdvance(previous, current uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

// Traffic reads the active data interface without invoking modem statistics commands.
func (c *Connector) Traffic(ctx context.Context, modem *mmodem.Modem) (TrafficStatus, error) {
	if modem == nil {
		return TrafficStatus{}, ErrModemRequired
	}
	if err := ctx.Err(); err != nil {
		return TrafficStatus{}, err
	}
	access := modemAccess{modem: modem}
	lock := c.modemLock(access.id())
	if !lock.TryAcquire(1) {
		return TrafficStatus{}, ErrOperationInProgress
	}
	defer lock.Release(1)
	if enabled, _ := c.airplaneModeState(access.id()); enabled || modem.Snapshot().AirplaneMode() {
		return TrafficStatus{}, nil
	}
	return c.trafficLocked(ctx, access)
}

func (c *Connector) trafficLocked(ctx context.Context, modem internetModem) (TrafficStatus, error) {
	profileID := modem.profileID()
	if qmap := c.qmapConnectionFor(modem.id(), modem.generation()); qmap != nil {
		if qmap.tracked.profileID != profileID {
			return TrafficStatus{}, nil
		}
		return c.sampleTraffic(qmap.tracked), nil
	}
	tracked, ok := c.connection(modem.id())
	if !ok || tracked.modemGeneration != modem.generation() || tracked.profileID != profileID {
		return TrafficStatus{}, nil
	}
	bearer, err := modem.bearer(ctx, tracked.bearerPath)
	if errors.Is(err, mmodem.ErrNotFound) {
		return TrafficStatus{}, nil
	}
	if err != nil {
		return TrafficStatus{}, err
	}
	connected, err := bearer.Connected(ctx)
	if err != nil || !connected {
		return TrafficStatus{}, err
	}
	return c.sampleTraffic(tracked), nil
}

func (c *Connector) sampleTraffic(tracked trackedConnection) TrafficStatus {
	result := TrafficStatus{Connected: true}
	if tracked.traffic == nil {
		return result
	}
	current, err := c.linkBytes(tracked.interfaceName)
	if errors.Is(err, netlink.ErrInterfaceNotFound) {
		return TrafficStatus{}
	}
	if err != nil {
		// An unreadable counter does not imply that the interface is gone.
		return result
	}
	result.Sample = tracked.traffic.sample(current)
	if result.Sample == nil {
		// The interface name was reused for a different link.
		return TrafficStatus{}
	}
	return result
}
