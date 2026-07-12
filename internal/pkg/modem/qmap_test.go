package modem

import (
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestLegacyQMAPDataPort(t *testing.T) {
	tests := []struct {
		name    string
		muxID   uint8
		want    qcom.WDSSIOPort
		wantErr bool
	}{
		{name: "mux 1", muxID: 1, want: qcom.WDSSIOPortA2MuxRMNET0},
		{name: "IMS mux 2", muxID: 2, want: qcom.WDSSIOPortA2MuxRMNET1},
		{name: "mux 8", muxID: 8, want: qcom.WDSSIOPortA2MuxRMNET7},
		{name: "zero", wantErr: true},
		{name: "outside range", muxID: 9, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := legacyQMAPDataPort(tt.muxID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("legacyQMAPDataPort() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("legacyQMAPDataPort() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("legacyQMAPDataPort() = %d, want %d", got, tt.want)
			}
		})
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
