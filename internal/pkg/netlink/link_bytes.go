package netlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// LinkBytes contains the kernel byte counters and identity of a network interface.
type LinkBytes struct {
	Index   uint32
	RXBytes uint64
	TXBytes uint64
}

// ReadLinkBytes reads the 64-bit counters for a named link in the current network namespace.
func ReadLinkBytes(name string) (LinkBytes, error) {
	name, err := validInterfaceName(name)
	if err != nil {
		return LinkBytes{}, err
	}
	fd, err := openRouteSocket()
	if err != nil {
		return LinkBytes{}, err
	}
	defer unix.Close(fd)

	// The optional sample must not hold the modem operation lock indefinitely.
	timeout := unix.NsecToTimeval(int64(time.Second))
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return LinkBytes{}, fmt.Errorf("set netlink receive timeout: %w", err)
	}
	payload := appendAttr(make([]byte, unix.SizeofIfInfomsg), unix.IFLA_IFNAME, append([]byte(name), 0))
	seq, err := sendRouteRequest(fd, unix.RTM_GETLINK, unix.NLM_F_REQUEST, payload)
	if err != nil {
		return LinkBytes{}, err
	}
	stats, err := receiveLinkBytes(fd, seq)
	if errors.Is(err, unix.ENODEV) {
		return LinkBytes{}, fmt.Errorf("%w: %s", ErrInterfaceNotFound, name)
	}
	if err != nil {
		return LinkBytes{}, fmt.Errorf("read counters for %s: %w", name, err)
	}
	return stats, nil
}

func receiveLinkBytes(fd int, seq uint32) (LinkBytes, error) {
	var buffer [16 * 1024]byte
	n, _, flags, from, err := unix.Recvmsg(fd, buffer[:], nil, 0)
	if err != nil {
		return LinkBytes{}, fmt.Errorf("receive netlink link response: %w", err)
	}
	if sender, ok := from.(*unix.SockaddrNetlink); !ok || sender.Pid != 0 {
		return LinkBytes{}, errors.New("netlink link response did not come from the kernel")
	}
	if flags&unix.MSG_TRUNC != 0 {
		return LinkBytes{}, errors.New("netlink link response is truncated")
	}
	messages, err := syscall.ParseNetlinkMessage(buffer[:n])
	if err != nil {
		return LinkBytes{}, fmt.Errorf("parse netlink link response: %w", err)
	}
	for _, msg := range messages {
		if msg.Header.Seq != seq {
			continue
		}
		switch msg.Header.Type {
		case unix.RTM_NEWLINK:
			return parseLinkBytes(msg.Data)
		case unix.NLMSG_ERROR:
			if err := netlinkResponseError(msg.Data); err != nil {
				return LinkBytes{}, err
			}
		}
	}
	return LinkBytes{}, errors.New("netlink link response has no matching statistics")
}

func parseLinkBytes(data []byte) (LinkBytes, error) {
	if len(data) < unix.SizeofIfInfomsg {
		return LinkBytes{}, errors.New("netlink interface header is truncated")
	}
	index := binary.NativeEndian.Uint32(data[4:8])
	stats := parseAttrs(data[unix.SizeofIfInfomsg:])[unix.IFLA_STATS64]
	// rtnl_link_stats64 starts with RX and TX packet counts, then byte counts.
	if index == 0 || len(stats) < 32 {
		return LinkBytes{}, errors.New("netlink link response has incomplete statistics")
	}
	return LinkBytes{
		Index:   index,
		RXBytes: binary.NativeEndian.Uint64(stats[16:24]),
		TXBytes: binary.NativeEndian.Uint64(stats[24:32]),
	}, nil
}
