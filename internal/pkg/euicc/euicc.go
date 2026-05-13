package euicc

//go:generate curl -L -o ci.json https://euicc-manual.osmocom.org/docs/pki/ci/manifest.json
//go:generate curl -L -o accredited.json https://euicc-manual.osmocom.org/docs/pki/eum/accredited.json

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
)

//go:embed ci.json
var ci []byte

//go:embed accredited.json
var accredited []byte

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

var (
	issuers []CertificateIssuer
	sites   Accredited
)

func init() {
	if err := json.Unmarshal(ci, &issuers); err != nil {
		slog.Error("failed to unmarshal certificate issuers", "error", err)
	}
	if err := json.Unmarshal(accredited, &sites); err != nil {
		slog.Error("failed to unmarshal accredited", "error", err)
	}
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
