//go:build wifi_calling

package features

import (
	"slices"
	"testing"
)

func TestListWiFiCallingBuildIncludesFeature(t *testing.T) {
	t.Parallel()

	if got := List(); !slices.Contains(got, WiFiCalling) {
		t.Fatalf("List() = %v, want %q", got, WiFiCalling)
	}
}
