package call

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

const procNetRoutePath = "/proc/net/route"
const procNetIPv6RoutePath = "/proc/net/ipv6_route"

func defaultRouteInterfaceNames() ([]string, error) {
	names := map[string]struct{}{}

	var errs []error
	data, err := os.ReadFile(procNetRoutePath)
	if err != nil {
		errs = append(errs, fmt.Errorf("read IPv4 default route table: %w", err))
	} else {
		addInterfaceNames(names, parseIPv4DefaultRouteInterfaceNames(string(data)))
	}

	data, err = os.ReadFile(procNetIPv6RoutePath)
	if err != nil {
		errs = append(errs, fmt.Errorf("read IPv6 default route table: %w", err))
	} else {
		addInterfaceNames(names, parseIPv6DefaultRouteInterfaceNames(string(data)))
	}

	if len(errs) == 2 {
		return nil, fmt.Errorf("read default route tables: %w", errors.Join(errs...))
	}
	return sortedInterfaceNames(names), nil
}

func parseIPv4DefaultRouteInterfaceNames(data string) []string {
	const routeFlagUp = 0x1

	minMetric := int(^uint(0) >> 1)
	names := map[string]struct{}{}
	for line := range strings.Lines(data) {
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseInt(fields[3], 16, 64)
		if err != nil || flags&routeFlagUp == 0 {
			continue
		}
		metric, err := strconv.Atoi(fields[6])
		if err != nil {
			continue
		}
		if metric < minMetric {
			minMetric = metric
			clear(names)
		}
		if metric == minMetric {
			names[fields[0]] = struct{}{}
		}
	}
	return sortedInterfaceNames(names)
}

func parseIPv6DefaultRouteInterfaceNames(data string) []string {
	const routeFlagUp = 0x1
	const defaultIPv6Destination = "00000000000000000000000000000000"

	minMetric := uint64(^uint64(0))
	names := map[string]struct{}{}
	for line := range strings.Lines(data) {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[0] != defaultIPv6Destination || fields[1] != "00" {
			continue
		}
		metric, err := strconv.ParseUint(fields[5], 16, 64)
		if err != nil {
			continue
		}
		flags, err := strconv.ParseUint(fields[8], 16, 64)
		if err != nil || flags&routeFlagUp == 0 {
			continue
		}
		if metric < minMetric {
			minMetric = metric
			clear(names)
		}
		if metric == minMetric {
			names[fields[9]] = struct{}{}
		}
	}
	return sortedInterfaceNames(names)
}

func interfaceNameSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func addInterfaceNames(dst map[string]struct{}, names []string) {
	for _, name := range names {
		dst[name] = struct{}{}
	}
}

func sortedInterfaceNames(names map[string]struct{}) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
