package lpa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	euicclpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

type profileCard struct {
	iccid     sgp22.ICCID
	profiles  []*sgp22.ProfileInfo
	err       error
	reads     int
	afterRead func()
}

func (c *profileCard) Transmit(req bertlv.Marshaler, resp bertlv.Unmarshaler) error {
	c.reads++
	request, err := req.MarshalBERTLV()
	if err != nil {
		return err
	}
	criteria := request.First(bertlv.ContextSpecific.Constructed(0))
	if !bytes.Equal(request.Tag, []byte{0xbf, 0x2d}) || criteria == nil {
		return errors.New("expected GetProfilesInfo with an ICCID filter")
	}
	filter := criteria.First(sgp22.TagICCID)
	if filter == nil || !bytes.Equal(filter.Value, c.iccid) {
		return errors.New("profile query did not filter by the active ICCID")
	}
	if c.afterRead != nil {
		c.afterRead()
	}
	if c.err != nil {
		return c.err
	}
	var profiles []*bertlv.TLV
	for _, profile := range c.profiles {
		profiles = append(profiles, bertlv.NewChildren(bertlv.Private.Constructed(3),
			bertlv.NewValue(sgp22.TagICCID, profile.ICCID),
			bertlv.NewValue(sgp22.TagProfileState, []byte{byte(profile.ProfileState)}),
			bertlv.NewValue(sgp22.TagProfileIcon, profile.Icon),
		))
	}
	return resp.UnmarshalBERTLV(bertlv.NewChildren(bertlv.ContextSpecific.Constructed(45),
		bertlv.NewChildren(bertlv.ContextSpecific.Constructed(0), profiles...),
	))
}

func (*profileCard) TransmitRaw([]byte) ([]byte, error) {
	return nil, errors.New("unexpected raw APDU")
}

func TestPoolActiveProfile(t *testing.T) {
	iccid, err := sgp22.NewICCID("8986000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	active := &sgp22.ProfileInfo{ICCID: iccid, ProfileState: sgp22.ProfileEnabled, Icon: []byte("icon")}
	disabled := &sgp22.ProfileInfo{ICCID: iccid, ProfileState: sgp22.ProfileDisabled}
	readErr := errors.New("card unavailable")
	tests := []struct {
		name             string
		cards            []*profileCard
		switchSIM        bool
		cancelBeforeRead bool
		cancelAfterRead  bool
		wantProfile      bool
		wantReads        []int
		wantErr          error
	}{
		{
			name: "active ICCID", cards: []*profileCard{{profiles: []*sgp22.ProfileInfo{active}}},
			wantProfile: true, wantReads: []int{1},
		},
		{
			name: "another SE", cards: []*profileCard{{}, {profiles: []*sgp22.ProfileInfo{active}}},
			wantProfile: true, wantReads: []int{1, 1},
		},
		{name: "absent", cards: []*profileCard{{}}, wantReads: []int{1}},
		{
			name: "disabled", cards: []*profileCard{{profiles: []*sgp22.ProfileInfo{disabled}}},
			wantReads: []int{1},
		},
		{
			name: "ambiguous", cards: []*profileCard{
				{profiles: []*sgp22.ProfileInfo{active}}, {profiles: []*sgp22.ProfileInfo{active}},
			},
			wantReads: []int{1, 1},
		},
		{name: "read error", cards: []*profileCard{{err: readErr}}, wantErr: readErr, wantReads: []int{1}},
		{
			name: "SIM changed", cards: []*profileCard{{profiles: []*sgp22.ProfileInfo{active}}, {}},
			switchSIM: true, wantReads: []int{1, 0},
		},
		{
			name: "cancelled before read", cards: []*profileCard{{}},
			cancelBeforeRead: true, wantErr: context.Canceled, wantReads: []int{0},
		},
		{
			name: "cancelled during read", cards: []*profileCard{{profiles: []*sgp22.ProfileInfo{active}}, {}},
			cancelAfterRead: true, wantErr: context.Canceled, wantReads: []int{1, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &modem.Modem{PrimarySIMSlot: 1, SIM: &modem.SIM{Slot: 1, Active: true, Identifier: iccid.String()}}
			pool := &Pool{entries: make(map[poolKey]*poolEntry), secureElems: make(map[poolSEKey][]SE)}
			seKey := poolSEKey{modem: device, slot: 1}
			for i, card := range tt.cards {
				card.iccid = iccid
				se := SE{ID: fmt.Sprintf("se-%d", i)}
				pool.secureElems[seKey] = append(pool.secureElems[seKey], se)
				key := poolKey{modem: device, slot: 1, seID: se.ID}
				entry := &poolEntry{
					modem:  device,
					client: &Client{clientView: &clientView{client: &euicclpa.Client{APDU: card}}},
					gate:   make(chan struct{}, 1),
				}
				entry.gate <- struct{}{}
				pool.entries[key] = entry
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelBeforeRead {
				cancel()
			}
			tt.cards[0].afterRead = func() {
				if tt.switchSIM {
					device.SIM = &modem.SIM{Slot: 2, Identifier: "8986000000000000002"}
				}
				if tt.cancelAfterRead {
					cancel()
				}
			}
			got, err := pool.ActiveProfile(ctx, device, device.Snapshot().SIMIdentity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ActiveProfile() error = %v, want %v", err, tt.wantErr)
			}
			if (got != nil) != tt.wantProfile {
				t.Fatalf("ActiveProfile() = %v, want profile %v", got, tt.wantProfile)
			}
			if got != nil && (got.ICCID.String() != iccid.String() || !bytes.Equal(got.Icon, active.Icon)) {
				t.Errorf("active profile = %+v", got)
			}
			for i, card := range tt.cards {
				if card.reads != tt.wantReads[i] {
					t.Errorf("SE %d profile reads = %d, want %d", i, card.reads, tt.wantReads[i])
				}
			}
			for _, entry := range pool.entries {
				if len(entry.gate) != 1 {
					t.Error("profile read did not release its lease")
				}
			}
			// Releasing the gate alone is insufficient: a leaked SIM reservation
			// would also prevent the next request from using any SE on this modem.
			releaseCtx, cancelRelease := context.WithTimeout(t.Context(), time.Second)
			defer cancelRelease()
			release, err := device.ReserveSIMSlot(releaseCtx)
			if err != nil {
				t.Fatalf("SIM reservation after profile read: %v", err)
			}
			release()
		})
	}
}
