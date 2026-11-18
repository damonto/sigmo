package lpa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"testing"

	"github.com/damonto/euicc-go/bertlv"
	euicclpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

func TestPoolCachesCardReads(t *testing.T) {
	device := &modem.Modem{PrimarySIMSlot: 1}
	card := &inventoryCard{eid: 0x89, nickname: "original", freeSpace: 10}
	pool := newInventoryPool()
	pool.addInventoryCard(device, 1, SEIDDefault, card)

	// Concurrent callers share one list read even when the pool starts cold.
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			inventories, err := pool.ProfileInventories(t.Context(), device)
			if err != nil || len(inventories) != 1 || inventories[0].EID != "89" ||
				len(inventories[0].Profiles) != 1 || inventories[0].Profiles[0].ProfileNickname != "original" {
				t.Errorf("ProfileInventories() = %v, %v", inventories, err)
			}
		})
	}
	readers.Wait()
	assertCardReads(t, card, cardReads{eid: 1, profiles: 1})

	profiles := readInventory(t, pool, device).Profiles
	info := readCachedInfo(t, pool, device)
	profiles[0].ProfileNickname = "changed in caller"
	profiles[0].Icon[0] = 0
	profiles[0] = nil
	info.FreeSpace = 0
	info.Certificates[0] = "changed in caller"

	profiles = readInventory(t, pool, device).Profiles
	if len(profiles) != 1 || profiles[0].ProfileNickname != "original" || string(profiles[0].Icon) != "icon" {
		t.Fatalf("profiles after caller edit = %v", profiles)
	}
	info = readCachedInfo(t, pool, device)
	if info.EID != "89" || info.FreeSpace != 10 || info.Certificates[0] == "changed in caller" {
		t.Fatalf("Info() after caller edit = %+v", info)
	}
	assertCardReads(t, card, cardReads{eid: 1, profiles: 1, info: 1})
	client := acquireInventoryLease(t, pool, device, SEIDDefault)
	defer client.Close()
	if _, err := client.ListProfile(nil, nil); err != nil {
		t.Fatal(err)
	}
	assertCardReads(t, card, cardReads{eid: 1, profiles: 2, info: 1})
}

func TestProfileWritesInvalidateCardCache(t *testing.T) {
	iccid := sgp22.ICCID{0x98, 0x76}
	ac := &euicclpa.ActivationCode{SMDP: &url.URL{Scheme: "https", Host: "example.invalid"}, IMEI: "123456789012345"}
	for _, test := range []struct {
		name string
		run  func(context.Context, *Lease) error
		fail bool
	}{
		{name: "rename", run: func(_ context.Context, l *Lease) error { return l.SetNickname(iccid, "updated") }},
		{name: "enable", run: func(_ context.Context, l *Lease) error { return l.EnableProfile(iccid, true) }},
		{name: "enable transport error", run: func(_ context.Context, l *Lease) error { return l.EnableProfile(iccid, true) }, fail: true},
		{name: "delete", run: func(_ context.Context, l *Lease) error { return l.Delete(iccid) }},
		{name: "delete followed by notification error", run: func(_ context.Context, l *Lease) error { return l.Delete(iccid) }, fail: true},
		{name: "download", run: func(ctx context.Context, l *Lease) error { return l.Download(ctx, ac, nil) }, fail: true},
		{name: "transfer download", run: func(ctx context.Context, l *Lease) error {
			_, err := l.DownloadProfile(ctx, ac, nil)
			return err
		}, fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := &modem.Modem{PrimarySIMSlot: 1}
			card := &inventoryCard{eid: 0x89, nickname: "original", freeSpace: 10}
			var wantErr error
			if test.fail {
				wantErr = errInventoryCard
				card.operationErr = wantErr
			}
			pool := newInventoryPool()
			pool.addInventoryCard(device, 1, SEIDDefault, card)
			readInventory(t, pool, device)
			readCachedInfo(t, pool, device)
			client := acquireInventoryLease(t, pool, device, SEIDDefault)
			err := test.run(t.Context(), client)
			if closeErr := client.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("profile operation error = %v, want %v", err, wantErr)
			}
			assertCardReads(t, card, cardReads{eid: 1, profiles: 1, info: 1})

			card.nickname, card.freeSpace = "updated", 12
			if profiles := readInventory(t, pool, device).Profiles; len(profiles) != 1 || profiles[0].ProfileNickname != "updated" {
				t.Fatalf("profiles after write = %v", profiles)
			}
			if info := readCachedInfo(t, pool, device); info.FreeSpace != 12 {
				t.Fatalf("Info() after write = %+v", info)
			}
			assertCardReads(t, card, cardReads{eid: 1, profiles: 2, info: 2})
		})
	}
}

func TestRemoveNotificationInvalidatesOnlyEUICCInfo(t *testing.T) {
	device := &modem.Modem{PrimarySIMSlot: 1}
	card := &inventoryCard{eid: 0x89, freeSpace: 10, operationErr: errInventoryCard}
	pool := newInventoryPool()
	pool.addInventoryCard(device, 1, SEIDDefault, card)
	readInventory(t, pool, device)
	if info := readCachedInfo(t, pool, device); info.FreeSpace != 10 {
		t.Fatalf("initial free space = %d, want 10", info.FreeSpace)
	}

	client := acquireInventoryLease(t, pool, device, SEIDDefault)
	err := client.RemoveNotificationFromList(1)
	if closeErr := client.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(err, errInventoryCard) {
		t.Fatalf("RemoveNotificationFromList() error = %v, want %v", err, errInventoryCard)
	}
	assertCardReads(t, card, cardReads{eid: 1, profiles: 1, info: 1})

	readInventory(t, pool, device)
	if info := readCachedInfo(t, pool, device); info.FreeSpace != 11 {
		t.Fatalf("free space after notification removal = %d, want 11", info.FreeSpace)
	}
	assertCardReads(t, card, cardReads{eid: 1, profiles: 1, info: 2})
}

func TestCardCacheRetriesFailedAndInterruptedReads(t *testing.T) {
	device := &modem.Modem{PrimarySIMSlot: 1}
	card := &inventoryCard{eid: 0x89, readErr: errInventoryCard}
	pool := newInventoryPool()
	pool.addInventoryCard(device, 1, SEIDDefault, card)

	if _, err := pool.ProfileInventories(t.Context(), device); !errors.Is(err, errInventoryCard) {
		t.Fatalf("ProfileInventories() error = %v, want %v", err, errInventoryCard)
	}
	if _, err := pool.EUICCInfo(t.Context(), device); !errors.Is(err, errInventoryCard) {
		t.Fatalf("Info() error = %v, want %v", err, errInventoryCard)
	}
	card.readErr = nil
	card.afterProfileRead = device.InvalidateSIMActivation
	if got, err := pool.ProfileInventories(t.Context(), device); got != nil || !errors.Is(err, errSIMSlotChanged) {
		t.Fatalf("ProfileInventories() during SIM change = %v, %v", got, err)
	}
	card.afterProfileRead = nil
	card.eid = 0x90
	for range 2 {
		inventory := readInventory(t, pool, device)
		if inventory.EID != "90" || len(inventory.Profiles) != 0 {
			t.Fatalf("inventory after SIM change = %+v", inventory)
		}
		if info := readCachedInfo(t, pool, device); info.EID != "90" {
			t.Fatalf("Info() after SIM change = %+v", info)
		}
	}
	assertCardReads(t, card, cardReads{eid: 2, profiles: 3, info: 2})
}

func TestCardCacheIsScopedToModemSlotAndSecureElement(t *testing.T) {
	device := &modem.Modem{EquipmentIdentifier: "test-modem"}
	pool := newInventoryPool()
	pool.addInventoryCard(device, 1, SEID0, &inventoryCard{eid: 0x89})
	pool.addInventoryCard(device, 1, SEID1, &inventoryCard{eid: 0x90})
	pool.addInventoryCard(device, 2, SEID0, &inventoryCard{eid: 0x91})

	assertEIDs := func(want ...string) {
		t.Helper()
		infos, err := pool.EUICCInfo(t.Context(), device)
		if err != nil || len(infos) != len(want) {
			t.Fatalf("EUICCInfo() = %v, %v, want EIDs %v", infos, err, want)
		}
		for i, card := range infos {
			if card.Info.EID != want[i] {
				t.Errorf("EUICCInfo() for %s: EID = %q, want %q", card.SE.ID, card.Info.EID, want[i])
			}
		}
	}
	assertEIDs("89", "90")
	if err := pool.invalidateSIMSlots(t.Context(), device, 0, 0); err != nil {
		t.Fatal(err)
	}
	device.PrimarySIMSlot = 2
	assertEIDs("91")
	device.PrimarySIMSlot = 1
	pool.addInventoryCard(device, 1, SEID0, &inventoryCard{eid: 0x92})
	assertEIDs("92")

	pool.retire(t.Context(), device)
	if _, err := pool.Acquire(t.Context(), device, SEID0); !errors.Is(err, ErrModemRetired) {
		t.Fatalf("Acquire() on retired modem error = %v, want %v", err, ErrModemRetired)
	}
	device = &modem.Modem{PrimarySIMSlot: 1, EquipmentIdentifier: "test-modem"}
	pool.addInventoryCard(device, 1, SEID0, &inventoryCard{eid: 0x93})
	assertEIDs("93")
}

func TestProfileInventoriesRejectsPartialResultAfterSIMChange(t *testing.T) {
	device := &modem.Modem{PrimarySIMSlot: 1}
	pool := newInventoryPool()
	first := &inventoryCard{eid: 0x89, nickname: "first"}
	second := &inventoryCard{eid: 0x90, nickname: "second", afterProfileRead: device.InvalidateSIMActivation}
	pool.addInventoryCard(device, 1, SEID0, first)
	pool.addInventoryCard(device, 1, SEID1, second)

	if got, err := pool.ProfileInventories(t.Context(), device); got != nil || !errors.Is(err, errSIMSlotChanged) {
		t.Fatalf("ProfileInventories() during SIM change = %v, %v", got, err)
	}
	assertCardReads(t, first, cardReads{eid: 1, profiles: 1})
	assertCardReads(t, second, cardReads{eid: 1, profiles: 1})

	second.afterProfileRead = nil
	inventories, err := pool.ProfileInventories(t.Context(), device)
	if err != nil || len(inventories) != 2 {
		t.Fatalf("ProfileInventories() after SIM change = %v, %v", inventories, err)
	}
	assertCardReads(t, first, cardReads{eid: 2, profiles: 2})
	assertCardReads(t, second, cardReads{eid: 2, profiles: 2})
}

type cardReads struct {
	eid, profiles, info int
}

var errInventoryCard = errors.New("test card transport error")

type inventoryCard struct {
	eid              byte
	nickname         string
	freeSpace        byte
	reads            cardReads
	readErr          error
	operationErr     error
	afterProfileRead func()
}

func (c *inventoryCard) Transmit(req bertlv.Marshaler, resp bertlv.Unmarshaler) error {
	switch request := req.(type) {
	case *sgp22.GetEuiccDataRequest:
		c.reads.eid++
		return resp.UnmarshalBERTLV(bertlv.NewChildren(bertlv.ContextSpecific.Constructed(62),
			bertlv.NewValue(bertlv.Application.Primitive(26), []byte{c.eid})))
	case *sgp22.ProfileInfoListRequest:
		c.reads.profiles++
		if c.readErr != nil {
			return c.readErr
		}
		if c.afterProfileRead != nil {
			c.afterProfileRead()
		}
		list := bertlv.NewChildren(bertlv.ContextSpecific.Constructed(0))
		if c.nickname != "" {
			list.Children = append(list.Children, bertlv.NewChildren(bertlv.Private.Constructed(3),
				bertlv.NewValue(sgp22.TagNickname, []byte(c.nickname)),
				bertlv.NewValue(sgp22.TagProfileIcon, []byte("icon"))))
		}
		return resp.UnmarshalBERTLV(bertlv.NewChildren(bertlv.ContextSpecific.Constructed(45), list))
	case *sgp22.GetEuiccInfoRequest:
		c.reads.info++
		if c.readErr != nil {
			return c.readErr
		}
		freeSpace, err := bertlv.NewValue(bertlv.ContextSpecific.Primitive(2), []byte{c.freeSpace}).MarshalBinary()
		if err != nil {
			return err
		}
		return resp.UnmarshalBERTLV(bertlv.NewChildren(bertlv.ContextSpecific.Constructed(34),
			bertlv.NewValue(bertlv.Universal.Primitive(12), []byte("2.4.0")),
			bertlv.NewChildren(bertlv.ContextSpecific.Constructed(10),
				bertlv.NewValue(bertlv.Universal.Primitive(4), []byte("issuer"))),
			bertlv.NewValue(bertlv.ContextSpecific.Primitive(4), freeSpace)))
	case *sgp22.SetNicknameRequest:
		return resp.UnmarshalBERTLV(successfulProfileWrite(bertlv.ContextSpecific.Constructed(41)))
	case *sgp22.ProfileOperationRequest:
		if request.Operation == sgp22.EnableProfile && c.operationErr != nil {
			return c.operationErr
		}
		return resp.UnmarshalBERTLV(successfulProfileWrite(bertlv.ContextSpecific.Constructed(uint64(request.Operation))))
	case *sgp22.ListNotificationRequest:
		if request.Filter[sgp22.NotificationEventDelete] && c.operationErr != nil {
			return c.operationErr
		}
		return resp.UnmarshalBERTLV(bertlv.NewChildren(bertlv.ContextSpecific.Constructed(40),
			bertlv.NewChildren(bertlv.ContextSpecific.Constructed(0))))
	case *sgp22.NotificationSentRequest:
		c.freeSpace++
		if c.operationErr != nil {
			return c.operationErr
		}
		return resp.UnmarshalBERTLV(successfulProfileWrite(bertlv.ContextSpecific.Constructed(48)))
	case *sgp22.GetEuiccChallengeRequest:
		// End a download attempt before it contacts the SM-DP+ server.
		return errInventoryCard
	default:
		return fmt.Errorf("unexpected card request %T", req)
	}
}

func successfulProfileWrite(tag bertlv.Tag) *bertlv.TLV {
	return bertlv.NewChildren(tag, bertlv.NewValue(bertlv.ContextSpecific.Primitive(0), []byte{0}))
}

func (*inventoryCard) TransmitRaw([]byte) ([]byte, error) {
	return nil, errors.New("unexpected raw APDU")
}

func newInventoryPool() *Pool {
	return &Pool{entries: make(map[poolKey]*poolEntry), secureElems: make(map[poolSEKey][]SE)}
}

func (p *Pool) addInventoryCard(device *modem.Modem, slot uint8, seID string, card *inventoryCard) {
	key := poolKey{modem: device, slot: slot, seID: seID}
	entry := newPoolEntry(p, key, &Client{
		clientView: &clientView{client: &euicclpa.Client{APDU: card}},
		logger:     slog.Default(),
		shutdown:   func() error { return nil },
	}, "")
	entry.gate <- struct{}{}
	p.entries[key] = entry
	seKey := poolSEKey{modem: device, slot: slot}
	p.secureElems[seKey] = append(p.secureElems[seKey], SE{ID: seID})
}

func acquireInventoryLease(t *testing.T, pool *Pool, device *modem.Modem, seID string) *Lease {
	t.Helper()
	client, err := pool.Acquire(t.Context(), device, seID)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func readInventory(t *testing.T, pool *Pool, device *modem.Modem) ProfileInventory {
	t.Helper()
	inventories, err := pool.ProfileInventories(t.Context(), device)
	if err != nil || len(inventories) != 1 {
		t.Fatalf("ProfileInventories() = %v, %v, want one secure element", inventories, err)
	}
	return inventories[0]
}

func readCachedInfo(t *testing.T, pool *Pool, device *modem.Modem) Info {
	t.Helper()
	infos, err := pool.EUICCInfo(t.Context(), device)
	if err != nil || len(infos) != 1 {
		t.Fatalf("EUICCInfo() = %v, %v, want one secure element", infos, err)
	}
	return infos[0].Info
}

func assertCardReads(t *testing.T, card *inventoryCard, want cardReads) {
	t.Helper()
	if card.reads != want {
		t.Fatalf("card reads = %+v, want %+v", card.reads, want)
	}
}
