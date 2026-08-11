package internet

import (
	"errors"
	"slices"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type invalidationLeaseProbe struct {
	closeCalls int
	closeErr   error
}

func (l *invalidationLeaseProbe) Close() error {
	l.closeCalls++
	return l.closeErr
}

func TestInvalidateModemGenerationIgnoresStaleOwner(t *testing.T) {
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() { removeInternetQMAPMuxes = previousRemove })
	removedMuxes := false
	removeInternetQMAPMuxes = func(*mmodem.Modem, ...uint8) error {
		removedMuxes = true
		return nil
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	lease := &invalidationLeaseProbe{}
	tracked := trackedConnection{modemGeneration: 8, prefs: Preferences{APN: "new"}}
	qmap := &qmapConnection{generation: 8, modem: &mmodem.Modem{EquipmentIdentifier: "modem-1"}, muxIDs: []uint8{internetQMAPMuxID}}
	state := qualcomm410State{generation: 8, selected: true, lease: lease}
	connector.connections["modem-1"] = tracked
	connector.qmapConnections["modem-1"] = qmap
	connector.qualcomm410States["modem-1"] = state

	if err := connector.invalidateModemGeneration(t.Context(), "modem-1", 7); err != nil {
		t.Fatalf("invalidateModemGeneration() error = %v", err)
	}
	if err := connector.invalidateModemGeneration(t.Context(), "modem-1", 7); err != nil {
		t.Fatalf("second invalidateModemGeneration() error = %v", err)
	}
	if got := connector.connections["modem-1"]; got.modemGeneration != 8 || got.prefs.APN != "new" {
		t.Fatalf("tracked connection = %+v, want generation 8", got)
	}
	if connector.qmapConnections["modem-1"] != qmap {
		t.Fatal("stale generation removed the replacement QMAP connection")
	}
	gotState := connector.qualcomm410States["modem-1"]
	if gotState.generation != 8 || gotState.lease != lease || !gotState.selected {
		t.Fatalf("Qualcomm 410 state = %+v, want replacement owner", gotState)
	}
	if lease.closeCalls != 0 || removedMuxes {
		t.Fatalf("stale invalidation released replacement resources: lease closes=%d mux removal=%v", lease.closeCalls, removedMuxes)
	}
}

func TestInvalidateModemGenerationReleasesMatchingResources(t *testing.T) {
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() { removeInternetQMAPMuxes = previousRemove })
	var removed []uint8
	removeInternetQMAPMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
		removed = append(removed, muxIDs...)
		return nil
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	prefs := Preferences{APN: "internet", AlwaysOn: true}
	lease := &invalidationLeaseProbe{}
	connector.connections["modem-1"] = trackedConnection{modemGeneration: 7, prefs: prefs}
	connector.qmapConnections["modem-1"] = &qmapConnection{
		generation: 7,
		modem:      &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		muxIDs:     []uint8{internetQMAPMuxID, ipv6QMAPMuxID},
		prefs:      prefs,
	}
	connector.preferences["modem-1"] = prefs
	connector.qualcomm410States["modem-1"] = qualcomm410State{generation: 7, selected: true, lease: lease}

	if err := connector.invalidateModemGeneration(t.Context(), "modem-1", 7); err != nil {
		t.Fatalf("invalidateModemGeneration() error = %v", err)
	}
	if _, ok := connector.connections["modem-1"]; ok {
		t.Fatal("matching tracked connection was not removed")
	}
	if _, ok := connector.qmapConnections["modem-1"]; ok {
		t.Fatal("matching QMAP connection was not removed")
	}
	if got := connector.preferences["modem-1"]; got != prefs {
		t.Fatalf("preferences = %+v, want %+v", got, prefs)
	}
	if lease.closeCalls != 1 {
		t.Fatalf("Qualcomm 410 lease close calls after repeated invalidation = %d, want 1", lease.closeCalls)
	}
	state := connector.qualcomm410States["modem-1"]
	if !state.selected || state.lease != nil || !state.reloadPending || !state.reconnectPending || state.reconnectPreferences != prefs {
		t.Fatalf("Qualcomm 410 replacement state = %+v", state)
	}
	if !slices.Equal(removed, []uint8{internetQMAPMuxID, ipv6QMAPMuxID}) {
		t.Fatalf("removed muxes = %v, want Internet muxes", removed)
	}
}

func TestInvalidateModemGenerationJoinsReleaseErrors(t *testing.T) {
	errLink := errors.New("lease close")
	errMux := errors.New("mux removal")
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() { removeInternetQMAPMuxes = previousRemove })
	removeInternetQMAPMuxes = func(*mmodem.Modem, ...uint8) error { return errMux }

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	connector.qmapConnections["modem-1"] = &qmapConnection{
		generation: 7,
		modem:      &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		muxIDs:     []uint8{internetQMAPMuxID},
	}
	connector.qualcomm410States["modem-1"] = qualcomm410State{
		generation: 7,
		selected:   true,
		lease:      &invalidationLeaseProbe{closeErr: errLink},
	}

	err = connector.invalidateModemGeneration(t.Context(), "modem-1", 7)
	if !errors.Is(err, errLink) || !errors.Is(err, errMux) {
		t.Fatalf("invalidateModemGeneration() error = %v, want lease and mux errors", err)
	}
}
