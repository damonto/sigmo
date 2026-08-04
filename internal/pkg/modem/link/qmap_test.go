package link

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

func TestQMAPStopError(t *testing.T) {
	errStop := errors.New("stop rejected")
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "already stopped", err: qcom.QMIErrorNoEffect},
		{name: "other error", err: errStop, wantErr: errStop},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := qmapStopError(tt.err); !errors.Is(err, tt.wantErr) {
				t.Fatalf("qmapStopError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNonQMAPLinkLayerForRawIP(t *testing.T) {
	tests := []struct {
		name    string
		rawIP   string
		want    qcom.WDALinkLayerProtocol
		wantErr bool
	}{
		{name: "current Ethernet still prefers raw IP", rawIP: "N\n", want: qcom.WDALinkLayerRawIP},
		{name: "raw IP framing", rawIP: "Y\n", want: qcom.WDALinkLayerRawIP},
		{name: "invalid state", rawIP: "maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nonQMAPLinkLayerForRawIP(tt.rawIP)
			if (err != nil) != tt.wantErr {
				t.Fatalf("nonQMAPLinkLayerForRawIP() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("nonQMAPLinkLayerForRawIP() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNonQMAPLinkLayerForFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags net.Flags
		want  qcom.WDALinkLayerProtocol
	}{
		{name: "point-to-point fallback", flags: net.FlagPointToPoint | net.FlagUp, want: qcom.WDALinkLayerRawIP},
		{name: "Ethernet fallback", flags: net.FlagBroadcast, want: qcom.WDALinkLayerEthernet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonQMAPLinkLayerForFlags(tt.flags); got != tt.want {
				t.Fatalf("nonQMAPLinkLayerForFlags() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSyncNonQMAPHostDataFormat(t *testing.T) {
	tests := []struct {
		name        string
		linkLayer   qcom.WDALinkLayerProtocol
		passThrough bool
		wantRawIP   string
	}{
		{name: "raw IP disables pass-through", linkLayer: qcom.WDALinkLayerRawIP, passThrough: true, wantRawIP: "Y"},
		{name: "Ethernet without pass-through attribute", linkLayer: qcom.WDALinkLayerEthernet, wantRawIP: "N"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qmiDir := t.TempDir()
			rawIPPath := filepath.Join(qmiDir, "raw_ip")
			if err := os.WriteFile(rawIPPath, []byte("N"), 0o644); err != nil {
				t.Fatalf("create raw_ip: %v", err)
			}
			passThroughPath := filepath.Join(qmiDir, "pass_through")
			if tt.passThrough {
				if err := os.WriteFile(passThroughPath, []byte("Y"), 0o644); err != nil {
					t.Fatalf("create pass_through: %v", err)
				}
			}

			if err := syncNonQMAPHostDataFormat(qmiDir, tt.linkLayer); err != nil {
				t.Fatalf("syncNonQMAPHostDataFormat() error = %v", err)
			}
			rawIP, err := os.ReadFile(rawIPPath)
			if err != nil {
				t.Fatalf("read raw_ip: %v", err)
			}
			if got := string(rawIP); got != tt.wantRawIP {
				t.Fatalf("raw_ip = %q, want %q", got, tt.wantRawIP)
			}
			if tt.passThrough {
				passThrough, err := os.ReadFile(passThroughPath)
				if err != nil {
					t.Fatalf("read pass_through: %v", err)
				}
				if got := string(passThrough); got != "N" {
					t.Fatalf("pass_through = %q, want N", got)
				}
			}
		})
	}
}

func TestSelectQMIDevicePortPrefersPrimaryPort(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/wwan0qmi1",
		Ports: []mmodem.ModemPort{
			{PortType: wwanmodem.PortQMI, Device: "/dev/wwan0qmi0"},
			{PortType: wwanmodem.PortQMI, Device: "/dev/wwan0qmi1"},
		},
	}

	got, err := selectQMIDevicePort(modem)
	if err != nil {
		t.Fatalf("selectQMIDevicePort() error = %v", err)
	}
	if got.Device != modem.PrimaryPort {
		t.Fatalf("selectQMIDevicePort() device = %q, want %q", got.Device, modem.PrimaryPort)
	}
}

func TestSelectQMIDevicePortRejectsMissingPrimaryPort(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/ttyUSB0",
		Ports: []mmodem.ModemPort{
			{PortType: wwanmodem.PortQMI, Device: "/dev/wwan0qmi0"},
			{PortType: wwanmodem.PortQMI, Device: "/dev/wwan0qmi1"},
		},
	}

	if _, err := selectQMIDevicePort(modem); !errors.Is(err, wwan.ErrUnsupported) {
		t.Fatalf("selectQMIDevicePort() error = %v, want %v", err, wwan.ErrUnsupported)
	}
}

func TestRestoreNonQMAPWDADataFormat(t *testing.T) {
	endpoint := &qcom.DataEndpoint{Type: qcom.DataEndpointHSUSB, InterfaceID: 4}
	rawIP := testWDADataFormat(qcom.WDALinkLayerRawIP, qcom.WDAAggregationDisabled)
	ethernet := testWDADataFormat(qcom.WDALinkLayerEthernet, qcom.WDAAggregationDisabled)

	tests := []struct {
		name             string
		endpoint         *qcom.DataEndpoint
		defaultResults   []testWDAResult
		endpointResults  []testWDAResult
		setResults       []testWDAResult
		wantSetEndpoints []bool
		wantErr          bool
	}{
		{
			name:           "already configured",
			defaultResults: []testWDAResult{{format: rawIP}},
		},
		{
			name:             "sets and verifies default endpoint",
			defaultResults:   []testWDAResult{{format: ethernet}, {format: rawIP}},
			setResults:       []testWDAResult{{format: rawIP}},
			wantSetEndpoints: []bool{false},
		},
		{
			name:             "get retries with endpoint",
			endpoint:         endpoint,
			defaultResults:   []testWDAResult{{err: qcom.QMIErrorMissingArgument}},
			endpointResults:  []testWDAResult{{format: ethernet}, {format: rawIP}},
			setResults:       []testWDAResult{{format: rawIP}},
			wantSetEndpoints: []bool{true},
		},
		{
			name:            "set retries with endpoint",
			endpoint:        endpoint,
			defaultResults:  []testWDAResult{{format: ethernet}},
			endpointResults: []testWDAResult{{format: rawIP}},
			setResults: []testWDAResult{
				{err: qcom.QMIErrorMissingArgument},
				{format: rawIP},
			},
			wantSetEndpoints: []bool{false, true},
		},
		{
			name:             "rejects verification mismatch",
			defaultResults:   []testWDAResult{{format: ethernet}, {format: ethernet}},
			setResults:       []testWDAResult{{format: rawIP}},
			wantSetEndpoints: []bool{false},
			wantErr:          true,
		},
		{
			name:           "missing endpoint remains an error",
			defaultResults: []testWDAResult{{err: qcom.QMIErrorMissingArgument}},
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &testWDADataFormatter{
				defaultResults:  tt.defaultResults,
				endpointResults: tt.endpointResults,
				setResults:      tt.setResults,
			}
			err := restoreNonQMAPWDADataFormat(context.Background(), client, qcom.WDALinkLayerRawIP, tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Fatalf("restoreNonQMAPWDADataFormat() error = %v, wantErr %t", err, tt.wantErr)
			}
			if len(client.setConfigs) != len(tt.wantSetEndpoints) {
				t.Fatalf("SetWDADataFormat() calls = %d, want %d", len(client.setConfigs), len(tt.wantSetEndpoints))
			}
			for i, wantEndpoint := range tt.wantSetEndpoints {
				gotEndpoint := client.setConfigs[i].Endpoint != nil
				if gotEndpoint != wantEndpoint {
					t.Fatalf("SetWDADataFormat() call %d endpoint = %t, want %t", i, gotEndpoint, wantEndpoint)
				}
			}
		})
	}
}

type testWDAResult struct {
	format qcom.WDADataFormat
	err    error
}

type testWDADataFormatter struct {
	defaultResults  []testWDAResult
	endpointResults []testWDAResult
	setResults      []testWDAResult
	setConfigs      []qcom.WDADataFormatConfig
}

func (f *testWDADataFormatter) WDADataFormat(context.Context) (qcom.WDADataFormat, error) {
	return popTestWDAResult(&f.defaultResults)
}

func (f *testWDADataFormatter) WDADataFormatForEndpoint(context.Context, *qcom.DataEndpoint) (qcom.WDADataFormat, error) {
	return popTestWDAResult(&f.endpointResults)
}

func (f *testWDADataFormatter) SetWDADataFormat(_ context.Context, config qcom.WDADataFormatConfig) (qcom.WDADataFormat, error) {
	f.setConfigs = append(f.setConfigs, config)
	return popTestWDAResult(&f.setResults)
}

func popTestWDAResult(results *[]testWDAResult) (qcom.WDADataFormat, error) {
	if len(*results) == 0 {
		return qcom.WDADataFormat{}, errors.New("unexpected WDA call")
	}
	result := (*results)[0]
	*results = (*results)[1:]
	return result.format, result.err
}

func testWDADataFormat(linkLayer qcom.WDALinkLayerProtocol, aggregation qcom.WDAAggregationProtocol) qcom.WDADataFormat {
	return qcom.WDADataFormat{
		LinkLayerProtocol:        linkLayer,
		LinkLayerProtocolKnown:   true,
		UplinkAggregation:        aggregation,
		UplinkAggregationKnown:   true,
		DownlinkAggregation:      aggregation,
		DownlinkAggregationKnown: true,
	}
}

func TestMatchQMAPMuxInterface(t *testing.T) {
	tests := []struct {
		name       string
		muxID      uint8
		ids        []uint8
		interfaces []qmapMuxInterface
		want       string
		wantErr    bool
	}{
		{
			name:  "dense mux IDs",
			muxID: 2,
			ids:   []uint8{1, 2, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
				{name: "qmimux2", index: 12},
			},
			want: "qmimux1",
		},
		{
			name:  "sparse mux IDs use creation order",
			muxID: 3,
			ids:   []uint8{1, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
			},
			want: "qmimux1",
		},
		{
			name:  "missing mux",
			muxID: 2,
			ids:   []uint8{1, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
			},
			wantErr: true,
		},
		{
			name:       "mismatched counts",
			muxID:      1,
			ids:        []uint8{1, 3},
			interfaces: []qmapMuxInterface{{name: "qmimux0", index: 10}},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchQMAPMuxInterface(tt.muxID, tt.ids, tt.interfaces)
			if tt.wantErr {
				if err == nil {
					t.Fatal("matchQMAPMuxInterface() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("matchQMAPMuxInterface() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("matchQMAPMuxInterface() = %q, want %q", got, tt.want)
			}
		})
	}
}
