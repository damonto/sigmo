package mdn

import (
	"encoding/json"
	"errors"
	"testing"
)

func decodeProfiles(t *testing.T, profiles ...Profile) *Configs {
	t.Helper()
	data, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	var configs Configs
	if err := json.Unmarshal(data, &configs); err != nil {
		t.Fatal(err)
	}
	return &configs
}
func TestCarrierSelection(t *testing.T) {
	base := testProfile()
	specific := base
	specific.GID1 = "0xaB"
	specific.ICCIDPrefix = "8901"
	specific.Server = "https://mvno.example/mdn"
	disabled := Profile{MCC: "001", MNC: "01", GID1: "AB12", Disabled: true}
	configs := decodeProfiles(t, base, specific, disabled)
	id := testIdentity()
	id.MNC = "001"
	id.MNCLength = 2
	id.ICCID = "8901000000"
	id.GID1 = "AB34FF"
	got, err := configs.Select(id)
	if err != nil || got.Server != specific.Server {
		t.Fatalf("selection = %+v, %v", got, err)
	}
	id.GID1 = "AB12FF"
	id.ICCID = "other"
	if _, err := configs.Select(id); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("disabled MVNO fell through: %v", err)
	}
	id.MNCLength = 3
	if configs.HasPLMN(id) {
		t.Fatal("matched different three-digit MNC")
	}
}
func TestSelectionConsidersLaterSpecificRule(t *testing.T) {
	base := testProfile()
	a, b, exact := base, base, base
	a.SPN = "test"
	b.GID1 = "AABB"
	b.Server = "https://other.example"
	exact.SPN = "test"
	exact.GID1 = "AABB"
	configs := decodeProfiles(t, a, b, exact)
	id := testIdentity()
	id.SPN = "TEST"
	id.GID1 = "aabbcc"
	if _, err := configs.Select(id); err != nil {
		t.Fatal(err)
	}
	configs = decodeProfiles(t, a, b)
	if _, err := configs.Select(id); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("conflicting match = %v", err)
	}
}
func TestInvalidCatalog(t *testing.T) {
	for _, data := range []string{`null`, `{}`, `[] []`, `[{"mcc":"001","mnc":"01","unknown":true}]`, `[{"mcc":"001","mnc":"01","disabled":true,"gid1":"ZZ"}]`} {
		var configs Configs
		if err := json.Unmarshal([]byte(data), &configs); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	a := testProfile()
	b := a
	b.Server = "https://different.example"
	data, _ := json.Marshal([]Profile{a, b})
	var configs Configs
	if err := json.Unmarshal(data, &configs); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate service = %v", err)
	}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.Server = "http://carrier.example" },
		func(p *Profile) { p.ProtocolVersion = "1" },
		func(p *Profile) { p.SigningVersion = 3 },
		func(p *Profile) { p.IdentityTemplate = "$unknown" },
		func(p *Profile) { p.IdentityTemplate = "$IMSIX" },
		func(p *Profile) { p.UserAgent = "Entitlement/$version" },
		func(p *Profile) { p.CountryISO = "12" },
	} {
		p := testProfile()
		change(&p)
		data, _ := json.Marshal([]Profile{p})
		if err := json.Unmarshal(data, &configs); err == nil {
			t.Fatal("accepted unsupported service")
		}
	}
}
