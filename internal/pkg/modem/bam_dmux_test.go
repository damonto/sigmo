package modem

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestValidateQualcomm410LayoutChecksEveryEndpoint(t *testing.T) {
	missingDevice := errors.New("missing device")
	missingInterface := errors.New("missing interface")
	var devices []string
	var interfaces []string
	err := validateQualcomm410Layout(qualcomm410LayoutProbe{
		device: func(path string) error {
			devices = append(devices, path)
			if path == Qualcomm410IMSQMI {
				return missingDevice
			}
			return nil
		},
		interfaceByName: func(name string) error {
			interfaces = append(interfaces, name)
			if name == Qualcomm410IMSInterface {
				return missingInterface
			}
			return nil
		},
	})
	if !errors.Is(err, missingDevice) || !errors.Is(err, missingInterface) {
		t.Fatalf("validateQualcomm410Layout() error = %v, want both probe errors", err)
	}
	if want := []string{Qualcomm410InternetQMI, Qualcomm410IMSQMI}; !slices.Equal(devices, want) {
		t.Fatalf("device probes = %v, want %v", devices, want)
	}
	if want := []string{Qualcomm410InternetInterface, Qualcomm410IMSInterface}; !slices.Equal(interfaces, want) {
		t.Fatalf("interface probes = %v, want %v", interfaces, want)
	}
}

func TestValidateQualcomm410ModemLayoutRequiresPrimaryOnDATA5(t *testing.T) {
	compareErr := errors.New("compare devices")
	tests := []struct {
		name        string
		modem       *Modem
		match       bool
		compareErr  error
		wantErr     error
		wantMessage string
	}{
		{name: "nil modem", wantMessage: "modem is required"},
		{name: "missing primary", modem: &Modem{}, wantMessage: "primary QMI control port is missing"},
		{name: "device comparison fails", modem: &Modem{PrimaryPort: "/dev/wwan0qmi1"}, compareErr: compareErr, wantErr: compareErr},
		{name: "primary uses DATA6", modem: &Modem{PrimaryPort: "/dev/wwan0qmi0"}, wantMessage: "does not resolve to Qualcomm 410 DATA5"},
		{name: "primary uses DATA5", modem: &Modem{PrimaryPort: "/dev/wwan0qmi1"}, match: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := qualcomm410LayoutProbe{
				device:          func(string) error { return nil },
				interfaceByName: func(string) error { return nil },
				sameDevice: func(stable, primary string) (bool, error) {
					if stable != Qualcomm410InternetQMI || primary != tt.modem.PrimaryPort {
						t.Fatalf("sameDevice(%q, %q)", stable, primary)
					}
					return tt.match, tt.compareErr
				},
			}
			err := validateQualcomm410ModemLayout(tt.modem, probe)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), tt.wantMessage)) {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want message %q", err, tt.wantMessage)
			}
			if tt.wantErr == nil && tt.wantMessage == "" && err != nil {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want nil", err)
			}
		})
	}
}

func TestIsBAMDMUXRawIP(t *testing.T) {
	tests := []struct {
		name   string
		format qcom.WDADataFormat
		want   bool
	}{
		{name: "unknown"},
		{name: "Ethernet", format: qcom.WDADataFormat{LinkLayerProtocolKnown: true, LinkLayerProtocol: qcom.WDALinkLayerEthernet}},
		{name: "raw IP", format: qcom.WDADataFormat{LinkLayerProtocolKnown: true, LinkLayerProtocol: qcom.WDALinkLayerRawIP}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBAMDMUXRawIP(tt.format); got != tt.want {
				t.Fatalf("isBAMDMUXRawIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBAMDMUXLinkCloseDetachesResourcesAfterFailure(t *testing.T) {
	pdnFailed := &qcom.PDNSession{}
	pdnClosed := &qcom.PDNSession{}
	client := &qcom.Client{}
	pdnErr := errors.New("stop PDN")
	clientErr := errors.New("close client")
	pdnAttempts := make(map[*qcom.PDNSession]int)
	clientAttempts := 0
	ops := bamDMUXCloseOps{
		pdn: func(pdn *qcom.PDNSession) error {
			pdnAttempts[pdn]++
			if pdn == pdnFailed && pdnAttempts[pdn] == 1 {
				return pdnErr
			}
			return nil
		},
		client: func(*qcom.Client) error {
			clientAttempts++
			if clientAttempts == 1 {
				return clientErr
			}
			return nil
		},
	}

	link := &BAMDMUXLink{client: client, pdns: []*qcom.PDNSession{pdnFailed, pdnClosed}}
	if err := link.closeWith(ops); !errors.Is(err, pdnErr) || !errors.Is(err, clientErr) {
		t.Fatalf("first Close() error = %v, want PDN and client errors", err)
	}
	if len(link.pdns) != 0 {
		t.Fatalf("remaining PDNs = %p, want none", link.pdns)
	}
	if link.client != nil {
		t.Fatal("client was retained after terminal Close")
	}
	if pdnAttempts[pdnFailed] != 1 || pdnAttempts[pdnClosed] != 1 {
		t.Fatalf("PDN close attempts = %v, want one attempt each", pdnAttempts)
	}
	if clientAttempts != 1 {
		t.Fatalf("client close attempts = %d, want 1", clientAttempts)
	}
	if err := link.closeWith(ops); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if clientAttempts != 1 || pdnAttempts[pdnFailed] != 1 {
		t.Fatal("second Close retried detached resources")
	}
}
