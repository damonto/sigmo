package lpa

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/damonto/euicc-go/driver"
	wwanmbim "github.com/damonto/wwan-go/mbim"
)

type mbimLPAReader interface {
	OpenChannel(ctx context.Context, aid []byte) (uint32, error)
	TransmitAPDU(ctx context.Context, channel uint32, command []byte) ([]byte, uint32, error)
	CloseChannel(ctx context.Context, channel uint32) error
	Close() error
}

type mbimLPAChannel struct {
	mu      sync.Mutex
	reader  mbimLPAReader
	channel uint32
	closed  bool
}

var _ driver.SmartCardChannel = (*mbimLPAChannel)(nil)

func newMBIMLPAChannel(reader mbimLPAReader) (driver.SmartCardChannel, error) {
	if reader == nil {
		return nil, errors.New("MBIM LPA reader is required")
	}
	return &mbimLPAChannel{reader: reader}, nil
}

func (c *mbimLPAChannel) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("MBIM LPA channel is closed")
	}
	return nil
}

func (c *mbimLPAChannel) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.reader.Close()
}

func (c *mbimLPAChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	return c.OpenLogicalChannelContext(context.Background(), aid)
}

func (c *mbimLPAChannel) OpenLogicalChannelContext(ctx context.Context, aid []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errors.New("MBIM LPA channel is closed")
	}
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	channel, err := c.reader.OpenChannel(ctx, aid)
	if err != nil {
		if errors.Is(err, wwanmbim.StatusMSSelectFailed) || errors.Is(err, wwanmbim.StatusNoDeviceSupport) {
			return 0, errors.Join(errAIDNotSupported, err)
		}
		return 0, err
	}
	c.channel = channel
	return byte(channel), nil
}

func (c *mbimLPAChannel) Transmit(command []byte) ([]byte, error) {
	return c.TransmitContext(context.Background(), command)
}

func (c *mbimLPAChannel) TransmitContext(ctx context.Context, command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("MBIM LPA channel is closed")
	}
	ctx, cancel := context.WithTimeout(ctx, channelOpenTimeout)
	defer cancel()
	response, status, err := c.reader.TransmitAPDU(ctx, c.channel, command)
	if err != nil {
		return nil, err
	}
	return binary.LittleEndian.AppendUint16(response, uint16(status)), nil
}

func (c *mbimLPAChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("MBIM LPA channel is closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelOpenTimeout)
	defer cancel()
	if err := c.reader.CloseChannel(ctx, uint32(channel)); err != nil {
		return err
	}
	if c.channel == uint32(channel) {
		c.channel = 0
	}
	return nil
}
