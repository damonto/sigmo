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

func TestReaderCandidatesPreferPrimaryThenFallbackPorts(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  []readerCandidate
	}{
		{
			name: "qmi primary falls back to at",
			modem: &mmodem.Modem{
				PrimaryPort:    "/dev/cdc-wdm1",
				PrimarySimSlot: 1,
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
				},
			},
			want: []readerCandidate{
				{portType: mmodem.ModemPortTypeQmi, device: "/dev/cdc-wdm1"},
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB6"},
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB7"},
			},
		},
		{
			name: "unknown primary uses supported ports",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyGPS0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeGps, Device: "/dev/ttyGPS0"},
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []readerCandidate{
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB2"},
			},
		},
		{
			name: "mbim primary uses at port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []readerCandidate{
				{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB2"},
			},
		},
		{
			name: "deduplicates primary port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyUSB2",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []readerCandidate{{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readerCandidates(tt.modem)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("readerCandidates() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOpenReaderFallsBackAfterPrimaryFailure(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort:    "/dev/cdc-wdm1",
		PrimarySimSlot: 2,
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
		},
	}
	var attempts []readerCandidate
	reader, err := openReaderWith(context.Background(), modem, func(_ context.Context, candidate readerCandidate, slot int) (usimcard.Reader, error) {
		attempts = append(attempts, candidate)
		if slot != 2 {
			t.Fatalf("slot = %d, want 2", slot)
		}
		if candidate.device != "/dev/ttyUSB7" {
			return nil, errors.New("reader unavailable")
		}
		return fakeUSIMReader{}, nil
	})
	if err != nil {
		t.Fatalf("openReaderWith() error = %v", err)
	}
	if reader == nil {
		t.Fatal("openReaderWith() reader is nil")
	}
	want := []readerCandidate{
		{portType: mmodem.ModemPortTypeQmi, device: "/dev/cdc-wdm1"},
		{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB6"},
		{portType: mmodem.ModemPortTypeAt, device: "/dev/ttyUSB7"},
	}
	if !slices.Equal(attempts, want) {
		t.Fatalf("attempts = %+v, want %+v", attempts, want)
	}
}

func TestOpenReaderReturnsJoinedCandidateErrors(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/cdc-wdm1",
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
		},
	}
	_, err := openReaderWith(context.Background(), modem, func(_ context.Context, candidate readerCandidate, _ int) (usimcard.Reader, error) {
		return nil, errors.New(readerPortTypeName(candidate.portType) + " unavailable")
	})
	if err == nil {
		t.Fatal("openReaderWith() error = nil, want error")
	}
	for _, want := range []string{"open QMI reader on /dev/cdc-wdm1", "open AT reader on /dev/ttyUSB6"} {
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
