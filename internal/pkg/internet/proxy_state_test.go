package internet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, path string)
	}{
		{
			name: "save load and delete",
			run: func(t *testing.T, path string) {
				if err := saveProxyStateForModem(path, "modem-1", "wws27u1i4"); err != nil {
					t.Fatalf("saveProxyStateForModem() error = %v", err)
				}
				got, ok, err := loadProxyStateForModem(path, "modem-1", "wws27u1i4")
				if err != nil {
					t.Fatalf("loadProxyStateForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadProxyStateForModem() ok = false, want true")
				}
				if !got {
					t.Fatal("loadProxyStateForModem() = false, want true")
				}
				if _, ok, err := loadProxyStateForModem(path, "modem-2", "wws27u1i4"); err != nil || ok {
					t.Fatalf("loadProxyStateForModem(other modem) = ok %t, err %v; want false, nil", ok, err)
				}
				if err := deleteProxyState(path, "wws27u1i4"); err != nil {
					t.Fatalf("deleteProxyState() error = %v", err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("proxy state file exists after delete, err = %v", err)
				}
			},
		},
		{
			name: "delete preserves other interfaces",
			run: func(t *testing.T, path string) {
				if err := saveProxyStateForModem(path, "modem-1", "wws0"); err != nil {
					t.Fatalf("saveProxyStateForModem(wws0) error = %v", err)
				}
				if err := saveProxyStateForModem(path, "modem-1", "wws1"); err != nil {
					t.Fatalf("saveProxyStateForModem(wws1) error = %v", err)
				}
				if err := deleteProxyState(path, "wws0"); err != nil {
					t.Fatalf("deleteProxyState() error = %v", err)
				}
				if _, ok, err := loadProxyStateForModem(path, "modem-1", "wws0"); err != nil || ok {
					t.Fatalf("loadProxyStateForModem(wws0) = ok %t, err %v; want false, nil", ok, err)
				}
				if got, ok, err := loadProxyStateForModem(path, "modem-1", "wws1"); err != nil || !ok || !got {
					t.Fatalf("loadProxyStateForModem(wws1) = %v, ok %t, err %v; want true, true, nil", got, ok, err)
				}
			},
		},
		{
			name: "save replaces stale interface for same modem",
			run: func(t *testing.T, path string) {
				if err := saveProxyStateForModem(path, "modem-1", "wws-old"); err != nil {
					t.Fatalf("saveProxyStateForModem(old) error = %v", err)
				}
				if err := saveProxyStateForModem(path, "modem-2", "wws-other"); err != nil {
					t.Fatalf("saveProxyStateForModem(other) error = %v", err)
				}
				if err := saveProxyStateForModem(path, "modem-1", "wws-new"); err != nil {
					t.Fatalf("saveProxyStateForModem(new) error = %v", err)
				}

				if _, ok, err := loadProxyStateForModem(path, "modem-1", "wws-old"); err != nil || ok {
					t.Fatalf("loadProxyStateForModem(old) = ok %t, err %v; want false, nil", ok, err)
				}
				if got, ok, err := loadProxyStateForModem(path, "modem-1", "wws-new"); err != nil || !ok || !got {
					t.Fatalf("loadProxyStateForModem(new) = %v, ok %t, err %v; want true, true, nil", got, ok, err)
				}
				if got, ok, err := loadProxyStateForModem(path, "modem-2", "wws-other"); err != nil || !ok || !got {
					t.Fatalf("loadProxyStateForModem(other) = %v, ok %t, err %v; want true, true, nil", got, ok, err)
				}
			},
		},
		{
			name: "lists current interface by modem",
			run: func(t *testing.T, path string) {
				if err := saveProxyStateForModem(path, "modem-1", "wws1"); err != nil {
					t.Fatalf("saveProxyStateForModem(wws1) error = %v", err)
				}
				if err := saveProxyStateForModem(path, "modem-1", "wws0"); err != nil {
					t.Fatalf("saveProxyStateForModem(wws0) error = %v", err)
				}
				if err := saveProxyStateForModem(path, "modem-2", "wws2"); err != nil {
					t.Fatalf("saveProxyStateForModem(wws2) error = %v", err)
				}

				got, err := proxyInterfacesForModem(path, "modem-1")
				if err != nil {
					t.Fatalf("proxyInterfacesForModem() error = %v", err)
				}
				want := []string{"wws0"}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Fatalf("proxyInterfacesForModem() = %#v, want %#v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.run(t, filepath.Join(t.TempDir(), "internet-proxies.json"))
		})
	}
}
