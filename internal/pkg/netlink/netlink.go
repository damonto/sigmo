package netlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	FamilyIPv4 = unix.AF_INET
	FamilyIPv6 = unix.AF_INET6
)

type DefaultRoute struct {
	Interface string
	Family    int
	Gateway   netip.Addr
	Metric    int
}

var ErrDefaultRouteExists = errors.New("default route already exists")

func DefaultRoutes() ([]DefaultRoute, error) {
	messages, err := routeDump(unix.AF_UNSPEC)
	if err != nil {
		return nil, err
	}
	var routes []DefaultRoute
	for _, msg := range messages {
		if msg.Header.Type != unix.RTM_NEWROUTE {
			continue
		}
		route, ok := parseDefaultRoute(msg.Data)
		if ok {
			routes = append(routes, route)
		}
	}
	return routes, nil
}

func SetUp(name string) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open control socket: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return fmt.Errorf("prepare interface flags: %w", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("read interface flags: %w", err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("set interface up: %w", err)
	}
	return nil
}

func SetMTU(name string, mtu uint32) error {
	if mtu == 0 {
		return nil
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open control socket: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return fmt.Errorf("prepare interface mtu: %w", err)
	}
	ifr.SetUint32(mtu)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFMTU, ifr); err != nil {
		return fmt.Errorf("set interface mtu: %w", err)
	}
	return nil
}

func AddAddress(interfaceName string, prefix netip.Prefix) error {
	return changeAddress(unix.RTM_NEWADDR, unix.NLM_F_CREATE|unix.NLM_F_REPLACE, interfaceName, prefix)
}

func DeleteAddress(interfaceName string, prefix netip.Prefix) error {
	err := changeAddress(unix.RTM_DELADDR, 0, interfaceName, prefix)
	if errors.Is(err, unix.EADDRNOTAVAIL) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func AddDefaultRoute(route DefaultRoute) error {
	err := changeDefaultRoute(unix.RTM_NEWROUTE, unix.NLM_F_CREATE|unix.NLM_F_EXCL, route)
	if errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("%w: interface %s metric %d", ErrDefaultRouteExists, route.Interface, route.Metric)
	}
	return err
}

func DeleteDefaultRoute(route DefaultRoute) error {
	err := changeDefaultRoute(unix.RTM_DELROUTE, 0, route)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func changeAddress(msgType uint16, flags uint16, interfaceName string, prefix netip.Prefix) error {
	if !prefix.IsValid() {
		return errors.New("address prefix is invalid")
	}
	ifi, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return fmt.Errorf("find interface: %w", err)
	}
	addr := prefix.Addr()
	family, raw, err := ipFamilyBytes(addr)
	if err != nil {
		return err
	}

	msg := make([]byte, unix.SizeofIfAddrmsg)
	msg[0] = byte(family)
	msg[1] = byte(prefix.Bits())
	binary.NativeEndian.PutUint32(msg[4:8], uint32(ifi.Index))
	msg = appendAttr(msg, unix.IFA_LOCAL, raw)
	msg = appendAttr(msg, unix.IFA_ADDRESS, raw)

	return send(msgType, unix.NLM_F_REQUEST|unix.NLM_F_ACK|flags, msg)
}

func changeDefaultRoute(msgType uint16, flags uint16, route DefaultRoute) error {
	if route.Family != FamilyIPv4 && route.Family != FamilyIPv6 {
		return errors.New("route family is invalid")
	}
	ifi, err := net.InterfaceByName(route.Interface)
	if err != nil {
		return fmt.Errorf("find interface: %w", err)
	}

	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = byte(route.Family)
	msg[4] = unix.RT_TABLE_MAIN
	msg[5] = unix.RTPROT_STATIC
	msg[6] = unix.RT_SCOPE_UNIVERSE
	msg[7] = unix.RTN_UNICAST

	oif := make([]byte, 4)
	binary.NativeEndian.PutUint32(oif, uint32(ifi.Index))
	msg = appendAttr(msg, unix.RTA_OIF, oif)
	if route.Metric > 0 {
		priority := make([]byte, 4)
		binary.NativeEndian.PutUint32(priority, uint32(route.Metric))
		msg = appendAttr(msg, unix.RTA_PRIORITY, priority)
	}
	if route.Gateway.IsValid() {
		family, raw, err := ipFamilyBytes(route.Gateway)
		if err != nil {
			return err
		}
		if family != route.Family {
			return errors.New("route gateway family does not match route family")
		}
		msg = appendAttr(msg, unix.RTA_GATEWAY, raw)
	}

	return send(msgType, unix.NLM_F_REQUEST|unix.NLM_F_ACK|flags, msg)
}

func send(msgType uint16, flags uint16, payload []byte) error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("open netlink socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("bind netlink socket: %w", err)
	}

	seq := uint32(time.Now().UnixNano())
	req := make([]byte, unix.SizeofNlMsghdr+len(payload))
	binary.NativeEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.NativeEndian.PutUint16(req[4:6], msgType)
	binary.NativeEndian.PutUint16(req[6:8], flags)
	binary.NativeEndian.PutUint32(req[8:12], seq)
	copy(req[unix.SizeofNlMsghdr:], payload)

	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("send netlink request: %w", err)
	}
	return receiveACK(fd, seq)
}

func routeDump(family int) ([]syscall.NetlinkMessage, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("open netlink socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, fmt.Errorf("bind netlink socket: %w", err)
	}

	seq := uint32(time.Now().UnixNano())
	payload := make([]byte, unix.SizeofRtMsg)
	payload[0] = byte(family)

	req := make([]byte, unix.SizeofNlMsghdr+len(payload))
	binary.NativeEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.NativeEndian.PutUint16(req[4:6], unix.RTM_GETROUTE)
	binary.NativeEndian.PutUint16(req[6:8], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	binary.NativeEndian.PutUint32(req[8:12], seq)
	copy(req[unix.SizeofNlMsghdr:], payload)

	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, fmt.Errorf("send netlink route dump request: %w", err)
	}

	var messages []syscall.NetlinkMessage
	buf := make([]byte, 32768)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return nil, fmt.Errorf("receive netlink route dump: %w", err)
		}
		batch, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return nil, fmt.Errorf("parse netlink route dump: %w", err)
		}
		for _, msg := range batch {
			if msg.Header.Seq != seq {
				continue
			}
			switch msg.Header.Type {
			case unix.NLMSG_DONE:
				return messages, nil
			case unix.NLMSG_ERROR:
				if len(msg.Data) < 4 {
					return nil, errors.New("netlink route dump error response is truncated")
				}
				code := int32(binary.NativeEndian.Uint32(msg.Data[:4]))
				if code == 0 {
					continue
				}
				return nil, unix.Errno(-code)
			default:
				messages = append(messages, msg)
			}
		}
	}
}

func receiveACK(fd int, seq uint32) error {
	buf := make([]byte, 8192)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return fmt.Errorf("receive netlink ack: %w", err)
		}
		messages, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return fmt.Errorf("parse netlink ack: %w", err)
		}
		for _, msg := range messages {
			if msg.Header.Seq != seq {
				continue
			}
			switch msg.Header.Type {
			case unix.NLMSG_ERROR:
				if len(msg.Data) < 4 {
					return errors.New("netlink error response is truncated")
				}
				code := int32(binary.NativeEndian.Uint32(msg.Data[:4]))
				if code == 0 {
					return nil
				}
				return unix.Errno(-code)
			case unix.NLMSG_DONE:
				return nil
			}
		}
	}
}

func parseDefaultRoute(data []byte) (DefaultRoute, bool) {
	if len(data) < unix.SizeofRtMsg {
		return DefaultRoute{}, false
	}
	family := int(data[0])
	if family != FamilyIPv4 && family != FamilyIPv6 {
		return DefaultRoute{}, false
	}
	if data[1] != 0 || data[7] != unix.RTN_UNICAST {
		return DefaultRoute{}, false
	}
	attrs := parseAttrs(data[unix.SizeofRtMsg:])
	if table := attrUint32(attrs[unix.RTA_TABLE]); table != 0 {
		if table != unix.RT_TABLE_MAIN {
			return DefaultRoute{}, false
		}
	} else if data[4] != unix.RT_TABLE_MAIN {
		return DefaultRoute{}, false
	}

	index := int(attrUint32(attrs[unix.RTA_OIF]))
	if index == 0 {
		return DefaultRoute{}, false
	}
	ifi, err := net.InterfaceByIndex(index)
	if err != nil {
		return DefaultRoute{}, false
	}

	return DefaultRoute{
		Interface: ifi.Name,
		Family:    family,
		Gateway:   attrAddr(family, attrs[unix.RTA_GATEWAY]),
		Metric:    int(attrUint32(attrs[unix.RTA_PRIORITY])),
	}, true
}

func parseAttrs(data []byte) map[uint16][]byte {
	attrs := make(map[uint16][]byte)
	for len(data) >= unix.SizeofRtAttr {
		length := int(binary.NativeEndian.Uint16(data[:2]))
		if length < unix.SizeofRtAttr || length > len(data) {
			break
		}
		aligned := rtaAlign(length)
		if aligned > len(data) {
			break
		}
		attrType := binary.NativeEndian.Uint16(data[2:4])
		attrs[attrType] = data[unix.SizeofRtAttr:length]
		data = data[aligned:]
	}
	return attrs
}

func attrUint32(value []byte) uint32 {
	if len(value) < 4 {
		return 0
	}
	return binary.NativeEndian.Uint32(value[:4])
}

func attrAddr(family int, value []byte) netip.Addr {
	switch family {
	case FamilyIPv4:
		if len(value) < 4 {
			return netip.Addr{}
		}
		var raw [4]byte
		copy(raw[:], value[:4])
		return netip.AddrFrom4(raw)
	case FamilyIPv6:
		if len(value) < 16 {
			return netip.Addr{}
		}
		var raw [16]byte
		copy(raw[:], value[:16])
		return netip.AddrFrom16(raw)
	default:
		return netip.Addr{}
	}
}

func appendAttr(msg []byte, attrType uint16, value []byte) []byte {
	length := unix.SizeofRtAttr + len(value)
	padded := rtaAlign(length)
	start := len(msg)
	msg = append(msg, make([]byte, padded)...)
	binary.NativeEndian.PutUint16(msg[start:start+2], uint16(length))
	binary.NativeEndian.PutUint16(msg[start+2:start+4], attrType)
	copy(msg[start+unix.SizeofRtAttr:start+length], value)
	return msg
}

func rtaAlign(length int) int {
	return (length + unix.RTA_ALIGNTO - 1) & ^(unix.RTA_ALIGNTO - 1)
}

func ipFamilyBytes(addr netip.Addr) (int, []byte, error) {
	if addr.Is4() {
		raw := addr.As4()
		return FamilyIPv4, raw[:], nil
	}
	if addr.Is6() {
		raw := addr.As16()
		return FamilyIPv6, raw[:], nil
	}
	return 0, nil, errors.New("ip address is invalid")
}
