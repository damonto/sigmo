package carrierconfig

import (
	"encoding/json"
	"testing"

	ts43 "github.com/damonto/ts43-go"
)

func TestPackUnpack(t *testing.T) {
	files := Files{SchemaVersion: 1,
		Entitlement: json.RawMessage(`[{"mccmnc":"00101","carrierName":"test","entitlementServer":"https://carrier.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7}]`),
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
	for _, name := range []string{"schema", "entitlement", "wfc", "ims", "mdn", "empty mdn"} {
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
			case "mdn":
				files.MDN = json.RawMessage(`null`)
			case "empty mdn":
				files.MDN = json.RawMessage{}
			}
			if _, _, err := Pack(files); err == nil {
				t.Fatal("accepted invalid files")
			}
		})
	}
}

func TestMDNCatalogCompatibility(t *testing.T) {
	files := Files{SchemaVersion: 1, Entitlement: json.RawMessage(`[]`), WFC: json.RawMessage(`[]`), IMS: json.RawMessage(`[]`)}
	old, err := Parse(files)
	if err != nil || old.MDN == nil || !old.MDN.Empty() {
		t.Fatalf("legacy catalog = %+v, %v", old, err)
	}
	files.MDN = json.RawMessage(`[{"mcc":"001","mnc":"01","entitlementServer":"https://carrier.example/mdn","protocolVersion":"2","signingVersion":2,"userAgent":"test","countryISO":"GB"}]`)
	data, rev, err := Pack(files)
	if err != nil {
		t.Fatal(err)
	}
	current, err := Unpack(data, rev)
	if err != nil || current.MDN.Empty() {
		t.Fatalf("MDN catalog lost in packaging: %v", err)
	}
}
