package mcpserver

import (
	"bytes"
	"encoding/json"
	"testing"

	esimhandler "github.com/damonto/sigmo/internal/app/handler/esim"
	euicchandler "github.com/damonto/sigmo/internal/app/handler/euicc"
	internethandler "github.com/damonto/sigmo/internal/app/handler/internet"
	messagehandler "github.com/damonto/sigmo/internal/app/handler/message"
	modemhandler "github.com/damonto/sigmo/internal/app/handler/modem"
	networkhandler "github.com/damonto/sigmo/internal/app/handler/network"
)

func TestListOutputsEncodeEmptyCollectionsAsArrays(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "authorized modems", value: modemsOutput{Modems: []authorizedModem{}}, want: `"modems":[]`},
		{name: "sim cards", value: simCardsOutput{SIMs: []modemhandler.SlotResponse{}}, want: `"sims":[]`},
		{name: "secure elements", value: euicchandler.SEsResponse{SEs: []euicchandler.SEItemResponse{}}, want: `"ses":[]`},
		{name: "eSIM profiles", value: esimhandler.ProfilesResponse{SEs: []esimhandler.ProfileGroupResponse{}}, want: `"ses":[]`},
		{name: "discovered profiles", value: discoveriesOutput{Profiles: []esimhandler.DiscoverResponse{}}, want: `"profiles":[]`},
		{name: "SMS", value: messagesOutput{Messages: []messagehandler.MessageResponse{}}, want: `"messages":[]`},
		{name: "networks", value: networksOutput{Networks: []networkhandler.NetworkResponse{}}, want: `"networks":[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if !bytes.Contains(data, []byte(tt.want)) {
				t.Fatalf("JSON = %s, want collection %s", data, tt.want)
			}
		})
	}
}

func TestSharedConnectionResponseNormalizesEmptyAddressLists(t *testing.T) {
	response := internethandler.ResponseFromConnection(nil)
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, field := range []string{`"ipv4Addresses":[]`, `"ipv6Addresses":[]`, `"dns":[]`} {
		if !bytes.Contains(data, []byte(field)) {
			t.Errorf("JSON = %s, missing %s", data, field)
		}
	}
}
