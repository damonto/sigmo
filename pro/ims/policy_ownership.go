//go:build ims

package ims

import (
	"fmt"
	"net"
)

// An abstract Unix socket makes ownership follow the process and the network
// namespace, which is also the scope of Linux policy routing state.
const imsPolicyRoutingOwnershipAddress = "@sigmo-volte-policy-routing"

func acquireIMSPolicyRoutingOwnership(address string) (*net.UnixListener, error) {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("acquire VoLTE policy routing ownership: %w", err)
	}
	return listener, nil
}
