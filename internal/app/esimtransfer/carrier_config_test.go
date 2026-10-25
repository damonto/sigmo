package esimtransfer

import (
	"testing"

	ts43 "github.com/damonto/ts43-go"
)

func testCarrierConfigs(t *testing.T) *ts43.EntitlementConfigs {
	t.Helper()
	configs := new(ts43.EntitlementConfigs)
	err := configs.UnmarshalJSON([]byte(`[
 {"mccmnc":"20809","channel":"google","carrierName":"test","entitlementServer":"https://carrier.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7},
 {"mccmnc":"20408","channel":"google","carrierName":"test","entitlementServer":"https://carrier.example","supportEsimToEsimTransfer":true,"supportPsimToEsimTransfer":true,"transferTypeEsim":7,"transferTypePsim":7},
 {"mccmnc":"23205","channel":"google","carrierName":"test","entitlementServer":"https://carrier.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7},
 {"mccmnc":"24214","channel":"samsung","carrierName":"test","entitlementServer":"https://carrier.example","samsungAndroidEntitlement":true,"supportEsimToEsimTransfer":true,"transferTypeEsim":7},
 {"mccmnc":"312012","channel":"samsung","carrierName":"test","entitlementServer":"https://carrier.example","samsungAndroidEntitlement":true,"supportEsimToEsimTransfer":true,"transferTypeEsim":7}
]`))
	if err != nil {
		t.Fatal(err)
	}
	return configs
}
