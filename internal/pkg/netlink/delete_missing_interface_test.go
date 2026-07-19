package netlink

import (
	"net/netip"
	"testing"
)

func TestDeleteResourcesFromMissingInterface(t *testing.T) {
	interfaceName := "sigmo-missing-interface"
	tests := []struct {
		name   string
		delete func() error
	}{
		{
			name: "address",
			delete: func() error {
				return DeleteAddress(interfaceName, netip.MustParsePrefix("192.0.2.10/32"))
			},
		},
		{
			name: "point-to-point address",
			delete: func() error {
				return DeletePointToPointAddress(interfaceName, netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.1"))
			},
		},
		{
			name: "default route",
			delete: func() error {
				return DeleteDefaultRoute(DefaultRoute{Interface: interfaceName, Family: FamilyIPv4, Table: 20_000})
			},
		},
		{
			name: "host route",
			delete: func() error {
				return DeleteHostRoute(interfaceName, netip.MustParseAddr("192.0.2.20"))
			},
		},
		{name: "link", delete: func() error { return DeleteLink(interfaceName) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.delete(); err != nil {
				t.Fatalf("delete missing-interface resource: %v", err)
			}
		})
	}
}
