//go:build ims

package ims

import (
	"fmt"
	"net"
)

// An abstract Unix socket makes ownership follow the process and the network
// namespace, which is also the scope of Linux policy routing state.
const imsPolicyRoutingOwnershipAddress = "@sigmo-volte-policy-routing"

func acquireIMSPolicyRoutingOwnership() (*net.UnixListener, error) {
	listener, err := listenIMSPolicyRoutingOwnership(imsPolicyRoutingOwnershipAddress)
	if err != nil {
		return nil, fmt.Errorf("acquire VoLTE policy routing ownership: %w", err)
	}
	return listener, nil
}

func listenIMSPolicyRoutingOwnership(address string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
}
