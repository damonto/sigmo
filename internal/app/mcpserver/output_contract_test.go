package mcpserver

import (
	"bytes"
	"encoding/json"
	"testing"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	appeuicc "github.com/damonto/sigmo/internal/app/euicc"
	internethandler "github.com/damonto/sigmo/internal/app/handler/internet"
	messagehandler "github.com/damonto/sigmo/internal/app/handler/message"
	appmodem "github.com/damonto/sigmo/internal/app/modem"
	appnetwork "github.com/damonto/sigmo/internal/app/network"
)

func TestListOutputsEncodeEmptyCollectionsAsArrays(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "authorized modems", value: modemsOutput{Modems: []authorizedModem{}}, want: `"modems":[]`},
		{name: "sim cards", value: simCardsOutput{SIMs: []appmodem.SlotResponse{}}, want: `"sims":[]`},
		{name: "secure elements", value: appeuicc.SEsResponse{SEs: []appeuicc.SEItemResponse{}}, want: `"ses":[]`},
		{name: "eSIM profiles", value: appesim.Inventory{SEs: []appesim.SecureElement{}}, want: `"ses":[]`},
		{name: "discovered profiles", value: discoveriesOutput{Profiles: []appesim.Discovery{}}, want: `"profiles":[]`},
		{name: "SMS", value: messagesOutput{Messages: []messagehandler.MessageResponse{}}, want: `"messages":[]`},
		{name: "networks", value: networksOutput{Networks: []appnetwork.NetworkResponse{}}, want: `"networks":[]`},
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
