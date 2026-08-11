package lpa

import (
	"context"
	"errors"
	"sync"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/wwan-go/qcom"
)

var errQMILPAChannelClosed = errors.New("QMI LPA channel is closed")

type qmiLPAReader interface {
	OpenLogicalChannel(ctx context.Context, aid []byte) (uint8, error)
	SendAPDU(ctx context.Context, channel uint8, command []byte) ([]byte, error)
	CloseLogicalChannel(ctx context.Context, channel uint8) error
	Close() error
}

// qmiLPAChannel does not activate a SIM slot. Its reader is already bound to
// the active physical slot's QMI logical slot, and activation here would
// invalidate other logical channels owned by the modem.
type qmiLPAChannel struct {
	mu      sync.Mutex
	reader  qmiLPAReader
	channel uint8
	closed  bool
}

var _ driver.SmartCardChannel = (*qmiLPAChannel)(nil)

func newQMILPAChannel(reader qmiLPAReader) (driver.SmartCardChannel, error) {
	if reader == nil {
		return nil, errors.New("QMI LPA reader is required")
	}
	return &qmiLPAChannel{reader: reader}, nil
}

func (c *qmiLPAChannel) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errQMILPAChannelClosed
	}
	return nil
}

func (c *qmiLPAChannel) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.reader.Close()
}

func (c *qmiLPAChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	return c.OpenLogicalChannelContext(context.Background(), aid)
}

func (c *qmiLPAChannel) OpenLogicalChannelContext(ctx context.Context, aid []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errQMILPAChannelClosed
	}
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	channel, err := c.reader.OpenLogicalChannel(ctx, aid)
	if err != nil {
		if errors.Is(err, qcom.QMIErrorNotSupported) {
			return 0, errors.Join(errAIDNotSupported, err)
		}
		return 0, err
	}
	c.channel = channel
	return channel, nil
}

func (c *qmiLPAChannel) Transmit(command []byte) ([]byte, error) {
	return c.TransmitContext(context.Background(), command)
}

func (c *qmiLPAChannel) TransmitContext(ctx context.Context, command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errQMILPAChannelClosed
	}
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	return c.reader.SendAPDU(ctx, c.channel, command)
}

func (c *qmiLPAChannel) CloseLogicalChannel(channel byte) error {
	return c.CloseLogicalChannelContext(context.Background(), channel)
}

func (c *qmiLPAChannel) CloseLogicalChannelContext(ctx context.Context, channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errQMILPAChannelClosed
	}
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	if err := c.reader.CloseLogicalChannel(ctx, channel); err != nil {
		c.closed = true
		return errors.Join(err, c.reader.Close())
	}
	if c.channel == channel {
		c.channel = 0
	}
	return nil
}
