//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	usimcard "github.com/damonto/uicc-go/usim/card"
)

func TestATReaderPortsPreferPrimaryThenFallbackPorts(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  []mmodem.ModemPort
	}{
		{
			name: "keeps AT fallback ports after Device primary",
			modem: &mmodem.Modem{
				PrimaryPort:    "/dev/cdc-wdm1",
				PrimarySimSlot: 1,
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
				},
			},
			want: []mmodem.ModemPort{
				{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
				{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
			},
		},
		{
			name: "unknown primary uses AT ports only",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyGPS0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeGps, Device: "/dev/ttyGPS0"},
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
		{
			name: "MBIM primary keeps AT fallback",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
		{
			name: "deduplicates primary port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyUSB2",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := atReaderPorts(tt.modem)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("atReaderPorts() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOpenReaderFallsBackAfterDeviceFailure(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort:    "/dev/cdc-wdm1",
		PrimarySimSlot: 2,
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
		},
	}
	var atAttempts []string
	var deviceCalled bool
	reader, err := openReaderWith(
		context.Background(),
		modem,
		func(_ context.Context, got *mmodem.Modem) (usimcard.Reader, error) {
			deviceCalled = true
			if got != modem {
				t.Fatalf("OpenDevice modem = %p, want %p", got, modem)
			}
			return nil, errors.New("device unavailable")
		},
		func(_ context.Context, port mmodem.ModemPort) (usimcard.Reader, error) {
			atAttempts = append(atAttempts, port.Device)
			if port.Device != "/dev/ttyUSB7" {
				return nil, errors.New("AT reader unavailable")
			}
			return fakeUSIMReader{}, nil
		},
	)
	if err != nil {
		t.Fatalf("openReaderWith() error = %v", err)
	}
	if reader == nil {
		t.Fatal("openReaderWith() reader is nil")
	}
	if !deviceCalled {
		t.Fatal("device open called = false, want true")
	}
	want := []string{"/dev/ttyUSB6", "/dev/ttyUSB7"}
	if !slices.Equal(atAttempts, want) {
		t.Fatalf("AT attempts = %+v, want %+v", atAttempts, want)
	}
}

func TestOpenReaderReturnsJoinedReaderErrors(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/cdc-wdm1",
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
		},
	}
	_, err := openReaderWith(
		context.Background(),
		modem,
		func(context.Context, *mmodem.Modem) (usimcard.Reader, error) {
			return nil, errors.New("device unavailable")
		},
		func(context.Context, mmodem.ModemPort) (usimcard.Reader, error) {
			return nil, errors.New("AT unavailable")
		},
	)
	if err == nil {
		t.Fatal("openReaderWith() error = nil, want error")
	}
	for _, want := range []string{"open device reader", "open AT reader on /dev/ttyUSB6"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

type fakeUSIMReader struct{}

func (fakeUSIMReader) ListApplications(context.Context) ([]usimcard.Application, error) {
	return nil, nil
}

func (fakeUSIMReader) FileAttributes(context.Context, usimcard.FileRef) (usimcard.FileAttributes, error) {
	return usimcard.FileAttributes{}, nil
}

func (fakeUSIMReader) ReadTransparent(context.Context, usimcard.TransparentRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) ReadRecord(context.Context, usimcard.RecordRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) Authenticate3G(context.Context, usimcard.AuthenticateRequest) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) SMSPPDownload(context.Context, usimcard.SMSPPDownloadRequest) (usimcard.SMSPPDownloadResponse, error) {
	return usimcard.SMSPPDownloadResponse{}, nil
}

func (fakeUSIMReader) Close() error {
	return nil
}
