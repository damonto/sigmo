package modem

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type activeProfileStub struct {
	read func(context.Context, *mmodem.Modem, mmodem.SIMIdentity) (*sgp22.ProfileInfo, error)
}

func (s activeProfileStub) ActiveProfile(ctx context.Context, device *mmodem.Modem, identity mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
	return s.read(ctx, device, identity)
}

func TestOverviewActiveProfileIcon(t *testing.T) {
	png := sgp22.ProfileIcon{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	euiccATR := []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}
	tests := []struct {
		name      string
		atr       []byte
		locked    bool
		iccid     string
		profile   *sgp22.ProfileInfo
		err       error
		switchSIM bool
		wantRead  bool
		wantIcon  bool
		wantLabel string
	}{
		{name: "active icon", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{Icon: png}, wantRead: true, wantIcon: true},
		{name: "nickname without icon", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{ProfileNickname: "Work", ProfileName: "Carrier"}, wantRead: true, wantLabel: "Work"},
		{name: "profile name", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{ProfileName: "Travel"}, wantRead: true, wantLabel: "Travel"},
		{name: "no icon", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{}, wantRead: true},
		{name: "unknown image", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{Icon: []byte("unknown")}, wantRead: true},
		{name: "no matching profile", atr: euiccATR, iccid: "8986000000000000001", wantRead: true},
		{name: "read error preserves list", atr: euiccATR, iccid: "8986000000000000001", err: errors.New("card unavailable"), wantRead: true},
		{name: "SIM changed during read", atr: euiccATR, iccid: "8986000000000000001", profile: &sgp22.ProfileInfo{Icon: png}, switchSIM: true, wantRead: true},
		{name: "locked", atr: euiccATR, iccid: "8986000000000000001", locked: true},
		{name: "physical", atr: []byte{0x3b, 0x00}, iccid: "8986000000000000001"},
		{name: "unknown SIM", iccid: "8986000000000000001"},
		{name: "missing ICCID", atr: euiccATR},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &mmodem.Modem{
				EquipmentIdentifier: "device-1", PrimarySIMSlot: 1,
				SIM: &mmodem.SIM{Slot: 1, Active: true, Identifier: tt.iccid, ATR: tt.atr},
			}
			if tt.locked {
				device.Status.SIM = wwanmodem.SIMStateLocked
			}
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			reads := 0
			catalog.profiles = activeProfileStub{read: func(ctx context.Context, got *mmodem.Modem, identity mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
				reads++
				if got != device || identity != device.Snapshot().SIMIdentity {
					t.Error("profile query did not use the overview SIM identity")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > profileIconTimeout {
					t.Error("optional profile query has no bounded deadline")
				}
				if tt.switchSIM {
					device.SIM = &mmodem.SIM{Slot: 2, Identifier: "8986000000000000002"}
				}
				return tt.profile, tt.err
			}}
			got, err := catalog.buildListResponse(t.Context(), []*mmodem.Modem{device})
			if err != nil || len(got) != 1 {
				t.Fatalf("buildListResponse() = %v, %v", got, err)
			}
			if (reads == 1) != tt.wantRead {
				t.Errorf("profile reads = %d, want read %v", reads, tt.wantRead)
			}
			want := ""
			if tt.wantIcon {
				want = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
			}
			wantLabel := tt.wantLabel
			if wantLabel == "" && !tt.locked {
				wantLabel = "Unknown"
			}
			if got[0].SIM.Label != wantLabel {
				t.Errorf("label = %q, want %q", got[0].SIM.Label, wantLabel)
			}
			if got[0].SIM.Icon != want {
				t.Errorf("SIM icon = %q, want %q", got[0].SIM.Icon, want)
			}
		})
	}
}

func TestCatalogGetIconCancellation(t *testing.T) {
	tests := []struct {
		name         string
		cancelCaller bool
		readErr      error
		wantErr      error
	}{
		{name: "icon deadline preserves overview", readErr: context.DeadlineExceeded},
		{name: "caller cancellation propagates", cancelCaller: true, readErr: context.Canceled, wantErr: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &mmodem.Modem{
				PrimarySIMSlot: 1,
				SIM: &mmodem.SIM{
					Slot: 1, Active: true, Identifier: "8986000000000000001",
					ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
				},
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			catalog.profiles = activeProfileStub{read: func(context.Context, *mmodem.Modem, mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
				if tt.cancelCaller {
					cancel()
				}
				return nil, tt.readErr
			}}
			got, err := catalog.Get(ctx, device)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (got == nil || got.SIM.Icon != "") {
				t.Fatalf("Get() = %+v, want overview without icon", got)
			}
		})
	}
}

func TestOverviewPreservesCachedLabelWhenProfileUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		profile   *sgp22.ProfileInfo
		err       error
		wantLabel string
	}{
		{name: "busy card", err: errors.New("card unavailable"), wantLabel: "Personal"},
		{name: "missing profile", wantLabel: "Personal"},
		{name: "confirmed rename", profile: &sgp22.ProfileInfo{ProfileNickname: "Work"}, wantLabel: "Work"},
		{name: "confirmed nickname removal", profile: &sgp22.ProfileInfo{ProfileName: "Carrier"}, wantLabel: "Carrier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			device := &mmodem.Modem{
				EquipmentIdentifier: "device-1", PrimarySIMSlot: 1,
				SIM: &mmodem.SIM{Slot: 1, Active: true, Identifier: "8986000000000000001", ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
			}
			if err := store.RecordSIMProfile(t.Context(), storage.SIMProfile{
				ProfileID: device.SIM.Identifier, ModemID: device.EquipmentIdentifier, Label: "Personal",
			}); err != nil {
				t.Fatal(err)
			}
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			catalog.storage = store
			catalog.profiles = activeProfileStub{read: func(context.Context, *mmodem.Modem, mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
				return tt.profile, tt.err
			}}
			if _, err := catalog.Get(t.Context(), device); err != nil {
				t.Fatal(err)
			}
			lines, err := store.SIMLines(t.Context())
			if err != nil || len(lines) != 1 {
				t.Fatalf("SIMLines() = %v, %v", lines, err)
			}
			if lines[0].Label != tt.wantLabel {
				t.Errorf("cached label = %q, want %q", lines[0].Label, tt.wantLabel)
			}
		})
	}
}

func TestOverviewReusesPersistedProfileDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sigmo.db")
	store, err := storage.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	device := &mmodem.Modem{
		EquipmentIdentifier: "device-1", PrimarySIMSlot: 1,
		SIM: &mmodem.SIM{Slot: 1, Active: true, Identifier: "8986000000000000001", ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
	}
	catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
	catalog.storage = store
	reads := 0
	nickname := "Personal"
	catalog.profiles = activeProfileStub{read: func(context.Context, *mmodem.Modem, mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
		reads++
		return &sgp22.ProfileInfo{ProfileNickname: nickname}, nil
	}}
	for range 3 {
		got, err := catalog.Get(t.Context(), device)
		if err != nil || got.SIM.Label != nickname {
			t.Fatalf("Get() = %+v, %v", got, err)
		}
	}
	if reads != 1 {
		t.Fatalf("profile reads = %d, want 1", reads)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	catalog.storage = store
	if _, err := catalog.Get(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("profile cache lost on reopen: reads = %d", reads)
	}
	if err := store.InvalidateSIMDisplay(t.Context(), device.SIM.Identifier); err != nil {
		t.Fatal(err)
	}
	nickname = "Work"
	got, err := catalog.Get(t.Context(), device)
	if err != nil || got.SIM.Label != nickname || reads != 2 {
		t.Fatalf("Get() after nickname change = %+v, %v; reads = %d", got, err, reads)
	}
}

func TestOverviewDoesNotCacheDisplayInvalidatedDuringCardRead(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	device := &mmodem.Modem{
		EquipmentIdentifier: "device-1", PrimarySIMSlot: 1,
		SIM: &mmodem.SIM{Slot: 1, Active: true, Identifier: "8986000000000000001", ATR: []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}},
	}
	catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
	catalog.storage = store
	catalog.profiles = activeProfileStub{read: func(ctx context.Context, _ *mmodem.Modem, _ mmodem.SIMIdentity) (*sgp22.ProfileInfo, error) {
		// Model a nickname mutation while an overview still holds the old response.
		if err := store.InvalidateSIMDisplay(ctx, device.SIM.Identifier); err != nil {
			return nil, err
		}
		return &sgp22.ProfileInfo{ProfileNickname: "Old"}, nil
	}}
	if _, err := catalog.Get(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if display, found, err := store.ReadSIMDisplay(t.Context(), device.SIM.Identifier); err != nil || found {
		t.Fatalf("stale display cached: %+v, %v, %v", display, found, err)
	}
}
