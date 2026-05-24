//go:build !esim_transfer && !wifi_calling

package features

import "testing"

func TestListDefaultBuildHasNoPrivateFeatures(t *testing.T) {
	t.Parallel()

	if got := List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
}
