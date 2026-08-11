package euicc

//go:generate curl -L -o ci.json https://euicc-manual.osmocom.org/docs/pki/ci/manifest.json
//go:generate curl -L -o accredited.json https://euicc-manual.osmocom.org/docs/pki/eum/accredited.json

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed ci.json
var certificateIssuerJSON []byte

//go:embed accredited.json
var accreditedSiteJSON []byte

type Accredited struct {
	Version   uint8      `json:"version"`
	Suppliers []Supplier `json:"suppliers"`
}

type Supplier struct {
	Name      string            `json:"name"`
	Abbr      string            `json:"abbr,omitempty"`
	Region    string            `json:"country"`
	EUM       []string          `json:"eum,omitempty"`
	Locations map[string]string `json:"locations"`
}

type CertificateIssuer struct {
	KeyID   string `json:"key-id"`
	Country string `json:"country"`
	Name    string `json:"name"`
}

type SASUP struct {
	Name   string
	Region string
}

var issuers = mustLoadCertificateIssuers(certificateIssuerJSON)

var sites = mustLoadAccreditedSites(accreditedSiteJSON)

func mustLoadCertificateIssuers(data []byte) []CertificateIssuer {
	var issuers []CertificateIssuer
	if err := json.Unmarshal(data, &issuers); err != nil {
		panic(fmt.Sprintf("mustLoadCertificateIssuers() error = %v", err))
	}
	return issuers
}

func mustLoadAccreditedSites(data []byte) Accredited {
	var sites Accredited
	if err := json.Unmarshal(data, &sites); err != nil {
		panic(fmt.Sprintf("mustLoadAccreditedSites() error = %v", err))
	}
	return sites
}

func LookupCertificateIssuer(keyID string) string {
	for _, ci := range issuers {
		if strings.HasPrefix(keyID, ci.KeyID) {
			return ci.Name
		}
	}
	return keyID
}

func LookupSASUP(eid, sasAccreditationNumber string) SASUP {
	if len(eid) < 8 {
		return SASUP{Name: sasAccreditationNumber}
	}

	eum := eid[:8]
	for _, supplier := range sites.Suppliers {
		if slices.Contains(supplier.EUM, eum) {
			if len(sasAccreditationNumber) < 5 {
				return SASUP{Name: supplier.Name, Region: supplier.Region}
			}
			if value, ok := supplier.Locations[sasAccreditationNumber[:5]]; ok {
				return SASUP{Name: supplier.Name, Region: value}
			}
			return SASUP{Name: supplier.Name, Region: supplier.Region}
		}
	}
	return SASUP{Name: sasAccreditationNumber}
}
