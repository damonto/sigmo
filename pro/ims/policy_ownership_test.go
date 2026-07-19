//go:build ims

package ims

import (
	"fmt"
	"os"
	"testing"
)

func TestIMSPolicyRoutingOwnership(t *testing.T) {
	address := fmt.Sprintf("@sigmo-volte-policy-routing-test-%d", os.Getpid())
	first, err := acquireIMSPolicyRoutingOwnership(address)
	if err != nil {
		t.Fatalf("first ownership listener: %v", err)
	}
	duplicate, err := acquireIMSPolicyRoutingOwnership(address)
	if err == nil {
		duplicateCloseErr := duplicate.Close()
		firstCloseErr := first.Close()
		t.Fatalf("second ownership listener error = nil, want address-in-use error (close errors: %v, %v)", duplicateCloseErr, firstCloseErr)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first ownership listener: %v", err)
	}

	second, err := acquireIMSPolicyRoutingOwnership(address)
	if err != nil {
		t.Fatalf("ownership after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second ownership listener: %v", err)
	}
}
