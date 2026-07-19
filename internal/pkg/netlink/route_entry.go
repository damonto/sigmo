package netlink

import "golang.org/x/sys/unix"

// RouteEntry contains the route metadata needed to allocate policy tables
// without assuming that a table only contains default routes.
type RouteEntry struct {
	Family   int
	Table    uint32
	Protocol int
	Default  bool
}

// RouteEntries returns one metadata entry for every IPv4 and IPv6 route.
func RouteEntries() ([]RouteEntry, error) {
	var entries []RouteEntry
	for _, family := range []int{FamilyIPv4, FamilyIPv6} {
		messages, err := routeDump(family)
		if err != nil {
			return nil, err
		}
		for _, msg := range messages {
			if msg.Header.Type != unix.RTM_NEWROUTE {
				continue
			}
			entry, ok := parseRouteEntry(msg.Data)
			if ok {
				entries = append(entries, entry)
			}
		}
	}
	return entries, nil
}

func parseRouteEntry(data []byte) (RouteEntry, bool) {
	if len(data) < unix.SizeofRtMsg {
		return RouteEntry{}, false
	}
	family := int(data[0])
	if family != FamilyIPv4 && family != FamilyIPv6 {
		return RouteEntry{}, false
	}
	attrs := parseAttrs(data[unix.SizeofRtMsg:])
	table := attrUint32(attrs[unix.RTA_TABLE])
	if table == 0 {
		table = uint32(data[4])
	}
	return RouteEntry{
		Family:   family,
		Table:    table,
		Protocol: int(data[5]),
		Default:  data[1] == 0 && data[7] == unix.RTN_UNICAST,
	}, true
}
