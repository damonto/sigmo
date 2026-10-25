package carrierconfig

import (
	"encoding/json"
	"testing"

	ts43 "github.com/damonto/ts43-go"
)

func TestPackUnpack(t *testing.T) {
	files := Files{SchemaVersion: 1,
		Entitlement: json.RawMessage(`[{"mccmnc":"00101","channel":"google","carrierName":"test","entitlementServer":"https://carrier.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7}]`),
		WFC:         json.RawMessage(`[]`), IMS: json.RawMessage(`[]`)}
	compressed, revision, err := Pack(files)
	if err != nil {
		t.Fatal(err)
	}
	config, err := Unpack(compressed, revision)
	if err != nil {
		t.Fatal(err)
	}
	entitlement, err := config.Entitlements.Discover(ts43.Identity{MCC: "001", MNC: "01"}, ts43.SIMTypeESIM)
	if err != nil || entitlement.Server != "https://carrier.example" {
		t.Fatalf("discovery = %+v, %v", entitlement, err)
	}
	compressed[len(compressed)/2] ^= 1
	if _, err := Unpack(compressed, revision); err == nil {
		t.Fatal("accepted altered snapshot")
	}
}

func TestPackValidatesAllFiles(t *testing.T) {
	for _, name := range []string{"schema", "entitlement", "wfc", "ims"} {
		t.Run(name, func(t *testing.T) {
			files := Files{SchemaVersion: 1, Entitlement: json.RawMessage(`[]`), WFC: json.RawMessage(`[]`), IMS: json.RawMessage(`[]`)}
			switch name {
			case "schema":
				files.SchemaVersion = 2
			case "entitlement":
				files.Entitlement = json.RawMessage(`null`)
			case "wfc":
				files.WFC = json.RawMessage(`{}`)
			case "ims":
				files.IMS = json.RawMessage(`[{"unknown":true}]`)
			}
			if _, _, err := Pack(files); err == nil {
				t.Fatal("accepted invalid files")
			}
		})
	}
}
