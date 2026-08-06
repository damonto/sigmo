package modem

import (
	"context"
	"errors"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestRunIsolatedNetworkScanSelectsProtocolClient(t *testing.T) {
	tests := []struct {
		name     string
		protocol wwanmodem.Protocol
		wantQMI  bool
	}{
		{name: "QMI", protocol: wwanmodem.ProtocolQMI, wantQMI: true},
		{name: "MBIM", protocol: wwanmodem.ProtocolMBIM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &stubNetworkScanner{
				networks: []wwanmodem.Operator{{ID: "00101"}},
			}
			var qmiCalled, mbimCalled bool
			openQMI := func(_ context.Context, path string, slot uint8) (networkScanner, error) {
				qmiCalled = true
				if path != "/dev/cdc-wdm0" || slot != 2 {
					t.Fatalf("QMI opener = (%q, %d), want control path and slot 2", path, slot)
				}
				return scanner, nil
			}
			openMBIM := func(_ context.Context, path string, slot uint8) (networkScanner, error) {
				mbimCalled = true
				if path != "/dev/cdc-wdm0" || slot != 2 {
					t.Fatalf("MBIM opener = (%q, %d), want control path and slot 2", path, slot)
				}
				return scanner, nil
			}

			networks, err := runIsolatedNetworkScan(t.Context(), isolatedNetworkScanConfig{
				protocol: tt.protocol,
				portPath: " /dev/cdc-wdm0 ",
				slot:     2,
				openQMI:  openQMI,
				openMBIM: openMBIM,
			})
			if err != nil {
				t.Fatalf("runIsolatedNetworkScan() error = %v", err)
			}
			if len(networks) != 1 || networks[0].ID != "00101" {
				t.Fatalf("runIsolatedNetworkScan() = %+v", networks)
			}
			if qmiCalled != tt.wantQMI || mbimCalled == tt.wantQMI {
				t.Fatalf("openers called = (QMI %t, MBIM %t), want QMI %t", qmiCalled, mbimCalled, tt.wantQMI)
			}
			if scanner.scanCalls != 1 || scanner.closeCalls != 1 {
				t.Fatalf("scanner calls = (scan %d, close %d), want one each", scanner.scanCalls, scanner.closeCalls)
			}
		})
	}
}

func TestRunIsolatedNetworkScanDoesNotFallbackWhenOpenFails(t *testing.T) {
	errOpen := errors.New("client IDs exhausted")
	_, err := runIsolatedNetworkScan(t.Context(), isolatedNetworkScanConfig{
		protocol: wwanmodem.ProtocolQMI,
		portPath: "/dev/cdc-wdm0",
		slot:     1,
		openQMI: func(context.Context, string, uint8) (networkScanner, error) {
			return nil, errOpen
		},
	})
	if !errors.Is(err, errOpen) {
		t.Fatalf("runIsolatedNetworkScan() error = %v, want open error", err)
	}
}

func TestRunIsolatedNetworkScanKeepsScanResultWhenCloseFails(t *testing.T) {
	errClose := errors.New("release client ID")
	scanner := &stubNetworkScanner{
		networks: []wwanmodem.Operator{{ID: "00101"}},
		closeErr: errClose,
	}
	networks, err := runIsolatedNetworkScan(t.Context(), isolatedNetworkScanConfig{
		protocol: wwanmodem.ProtocolMBIM,
		portPath: "/dev/cdc-wdm0",
		slot:     1,
		openMBIM: func(context.Context, string, uint8) (networkScanner, error) {
			return scanner, nil
		},
	})
	if err != nil {
		t.Fatalf("runIsolatedNetworkScan() error = %v, want successful scan", err)
	}
	if len(networks) != 1 || networks[0].ID != "00101" {
		t.Fatalf("runIsolatedNetworkScan() = %+v", networks)
	}
}

func TestRunIsolatedNetworkScanReturnsScanErrorInsteadOfCloseError(t *testing.T) {
	errScan := errors.New("firmware scan")
	errClose := errors.New("release client ID")
	scanner := &stubNetworkScanner{scanErr: errScan, closeErr: errClose}
	_, err := runIsolatedNetworkScan(t.Context(), isolatedNetworkScanConfig{
		protocol: wwanmodem.ProtocolQMI,
		portPath: "/dev/cdc-wdm0",
		slot:     1,
		openQMI: func(context.Context, string, uint8) (networkScanner, error) {
			return scanner, nil
		},
	})
	if !errors.Is(err, errScan) {
		t.Fatalf("runIsolatedNetworkScan() error = %v, want scan error", err)
	}
	if errors.Is(err, errClose) {
		t.Fatalf("runIsolatedNetworkScan() error = %v, unexpectedly joined close error", err)
	}
}

func TestRunIsolatedNetworkScanRejectsMissingControlPort(t *testing.T) {
	_, err := runIsolatedNetworkScan(t.Context(), isolatedNetworkScanConfig{
		protocol: wwanmodem.ProtocolQMI,
		portPath: " ",
		openQMI: func(context.Context, string, uint8) (networkScanner, error) {
			t.Fatal("opener called without a control port")
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("runIsolatedNetworkScan() error = nil, want missing port error")
	}
}

type stubNetworkScanner struct {
	networks   []wwanmodem.Operator
	scanErr    error
	closeErr   error
	scanCalls  int
	closeCalls int
}

func (s *stubNetworkScanner) ScanNetworks(context.Context) ([]wwanmodem.Operator, error) {
	s.scanCalls++
	return s.networks, s.scanErr
}

func (s *stubNetworkScanner) Close() error {
	s.closeCalls++
	return s.closeErr
}
