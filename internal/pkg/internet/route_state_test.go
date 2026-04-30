package internet

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

func saveOwnerlessRouteState(path string, interfaceName string, preferred []netlink.DefaultRoute, changes []defaultRouteChange) error {
	return saveRouteStateForModem(path, "", interfaceName, preferred, changes)
}

func loadRouteState(path string, interfaceName string) ([]defaultRouteChange, bool, error) {
	return loadRouteStateMatching(path, "", interfaceName, false)
}

func TestRouteState(t *testing.T) {
	t.Parallel()

	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	changes := []defaultRouteChange{
		{
			Original: netlink.DefaultRoute{
				Interface: "ens18",
				Family:    netlink.FamilyIPv4,
				Protocol:  4,
				Gateway:   netip.MustParseAddr("10.10.10.201"),
				Metric:    0,
			},
			Replacement: netlink.DefaultRoute{
				Interface: "ens18",
				Family:    netlink.FamilyIPv4,
				Protocol:  4,
				Gateway:   netip.MustParseAddr("10.10.10.201"),
				Metric:    defaultRouteMetric + 1,
			},
		},
	}

	tests := []struct {
		name string
		run  func(t *testing.T, path string)
	}{
		{
			name: "save load and delete",
			run: func(t *testing.T, path string) {
				if err := saveOwnerlessRouteState(path, "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteState() error = %v", err)
				}
				got, ok, err := loadRouteState(path, "wws27u1i4")
				if err != nil {
					t.Fatalf("loadRouteState() error = %v", err)
				}
				if !ok {
					t.Fatal("loadRouteState() ok = false, want true")
				}
				if !reflect.DeepEqual(got, changes) {
					t.Fatalf("loadRouteState() = %#v, want %#v", got, changes)
				}
				if err := deleteRouteState(path, "wws27u1i4"); err != nil {
					t.Fatalf("deleteRouteState() error = %v", err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("state file exists after delete, err = %v", err)
				}
			},
		},
		{
			name: "delete preserves other interfaces",
			run: func(t *testing.T, path string) {
				if err := saveOwnerlessRouteState(path, "wws0", preferred, changes); err != nil {
					t.Fatalf("saveRouteState(wws0) error = %v", err)
				}
				if err := saveOwnerlessRouteState(path, "wws1", preferred, changes); err != nil {
					t.Fatalf("saveRouteState(wws1) error = %v", err)
				}
				if err := deleteRouteState(path, "wws0"); err != nil {
					t.Fatalf("deleteRouteState() error = %v", err)
				}
				if _, ok, err := loadRouteState(path, "wws0"); err != nil || ok {
					t.Fatalf("loadRouteState(wws0) = ok %t, err %v; want false, nil", ok, err)
				}
				if got, ok, err := loadRouteState(path, "wws1"); err != nil || !ok || !reflect.DeepEqual(got, changes) {
					t.Fatalf("loadRouteState(wws1) = %#v, ok %t, err %v; want %#v, true, nil", got, ok, err, changes)
				}
			},
		},
		{
			name: "missing file",
			run: func(t *testing.T, path string) {
				got, ok, err := loadRouteState(path, "wws27u1i4")
				if err != nil {
					t.Fatalf("loadRouteState() error = %v", err)
				}
				if ok {
					t.Fatal("loadRouteState() ok = true, want false")
				}
				if got != nil {
					t.Fatalf("loadRouteState() = %#v, want nil", got)
				}
			},
		},
		{
			name: "stores modem owner",
			run: func(t *testing.T, path string) {
				if err := saveRouteStateForModem(path, "modem-1", "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteStateForModem() error = %v", err)
				}
				entries, err := loadAllRouteStates(path)
				if err != nil {
					t.Fatalf("loadAllRouteStates() error = %v", err)
				}
				if got := entries["wws27u1i4"].ModemID; got != "modem-1" {
					t.Fatalf("ModemID = %q, want modem-1", got)
				}
			},
		},
		{
			name: "load for modem accepts matching owner",
			run: func(t *testing.T, path string) {
				if err := saveRouteStateForModem(path, "modem-1", "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteStateForModem() error = %v", err)
				}
				got, ok, err := loadRouteStateForModem(path, "modem-1", "wws27u1i4")
				if err != nil {
					t.Fatalf("loadRouteStateForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadRouteStateForModem() ok = false, want true")
				}
				if !reflect.DeepEqual(got, changes) {
					t.Fatalf("loadRouteStateForModem() = %#v, want %#v", got, changes)
				}
			},
		},
		{
			name: "load for modem rejects different owner",
			run: func(t *testing.T, path string) {
				if err := saveRouteStateForModem(path, "modem-1", "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteStateForModem() error = %v", err)
				}
				got, ok, err := loadRouteStateForModem(path, "modem-2", "wws27u1i4")
				if err != nil {
					t.Fatalf("loadRouteStateForModem() error = %v", err)
				}
				if ok {
					t.Fatal("loadRouteStateForModem() ok = true, want false")
				}
				if got != nil {
					t.Fatalf("loadRouteStateForModem() = %#v, want nil", got)
				}
			},
		},
		{
			name: "load for modem accepts ownerless legacy state",
			run: func(t *testing.T, path string) {
				if err := saveOwnerlessRouteState(path, "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteState() error = %v", err)
				}
				got, ok, err := loadRouteStateForModem(path, "modem-1", "wws27u1i4")
				if err != nil {
					t.Fatalf("loadRouteStateForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadRouteStateForModem() ok = false, want true")
				}
				if !reflect.DeepEqual(got, changes) {
					t.Fatalf("loadRouteStateForModem() = %#v, want %#v", got, changes)
				}
			},
		},
		{
			name: "rejects overwrite",
			run: func(t *testing.T, path string) {
				if err := saveOwnerlessRouteState(path, "wws27u1i4", preferred, changes); err != nil {
					t.Fatalf("saveRouteState() error = %v", err)
				}
				if err := saveOwnerlessRouteState(path, "wws27u1i4", preferred, changes); err == nil {
					t.Fatal("saveRouteState() error = nil, want error")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.run(t, filepath.Join(t.TempDir(), "internet-routes.json"))
		})
	}
}

func TestLoadRouteStateRejectsBadGateway(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "bad original gateway",
			content: `{
  "version": 1,
  "interfaces": {
    "wws0": {
      "changes": [
        {
          "original": {
            "interface": "ens18",
            "family": 2,
            "gateway": "not-an-ip",
            "metric": 0
          },
          "replacement": {
            "interface": "ens18",
            "family": 2,
            "gateway": "10.10.10.201",
            "metric": 11
          }
        }
      ]
    }
  }
}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			if _, _, err := loadRouteState(path, "wws0"); err == nil {
				t.Fatal("loadRouteState() error = nil, want error")
			}
		})
	}
}

func TestWriteRouteStateRemovesTempFileOnWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "temp path is cleaned"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
			if err := os.Mkdir(tempPath, 0o700); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}

			err := writeRouteState(path, routeStateFile{})
			if err == nil {
				t.Fatal("writeRouteState() error = nil, want error")
			}
			if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
				t.Fatalf("temp file stat error = %v, want not exist", err)
			}
		})
	}
}
