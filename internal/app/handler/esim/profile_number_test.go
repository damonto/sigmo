package esim

import (
	"path/filepath"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestProfileNumbers(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, id := range []string{"active", "inactive"} {
		if err := store.RecordSIMProfile(t.Context(), storage.SIMProfile{ProfileID: id, ModemID: "modem", Number: "+100"}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name   string
		iccid  string
		number string
		absent bool
		want   string
	}{
		{name: "current number wins", iccid: "active", number: " +200 ", want: "+200"},
		{name: "temporarily missing number", iccid: "active", want: "+100"},
		{name: "other active SIM", iccid: "other", number: "+200", want: "+100"},
		{name: "absent SIM retains stale metadata", iccid: "active", number: "+200", absent: true, want: "+100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{SIM: &mmodem.SIM{Identifier: tt.iccid}, Number: tt.number}
			if tt.absent {
				modem.Status.SIM = wwanmodem.SIMStateAbsent
			}
			response := &ProfilesResponse{SEs: []ProfileGroupResponse{
				{ID: "se-a", Profiles: []ProfileResponse{{ICCID: "active", ProfileState: 1}, {ICCID: "inactive"}}},
				{ID: "se-b", Profiles: []ProfileResponse{{ICCID: "unknown"}}},
			}}
			p := &profile{history: store}
			if err := p.populateNumbers(t.Context(), modem.Snapshot(), response); err != nil {
				t.Fatal(err)
			}
			if got := response.SEs[0].Profiles[0].Number; got != tt.want {
				t.Fatalf("active number = %q, want %q", got, tt.want)
			}
			if got := response.SEs[0].Profiles[1].Number; got != "+100" {
				t.Fatalf("inactive number = %q", got)
			}
			if got := response.SEs[1].Profiles[0].Number; got != "" {
				t.Fatalf("unknown number = %q", got)
			}
		})
	}
}
