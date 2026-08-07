package stk

import (
	"errors"
	"path/filepath"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestOpenCardRejectsATOnlyModem(t *testing.T) {
	port := filepath.Join(t.TempDir(), "missing-at-port")
	modem := &mmodem.Modem{
		PrimaryPort: port,
		Ports: []mmodem.ModemPort{
			{PortType: wwanmodem.PortAT, Device: port},
		},
	}

	_, err := OpenCard(t.Context(), modem)
	if !errors.Is(err, wwan.ErrUnsupported) {
		t.Fatalf("OpenCard() error = %v, want %v", err, wwan.ErrUnsupported)
	}
}
