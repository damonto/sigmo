package link

import (
	"errors"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

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
