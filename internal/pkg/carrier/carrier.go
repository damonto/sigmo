package carrier

//go:generate curl -L -o carrier.json https://mno-list.harded.org/unified.json

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed carrier.json
var carrierJSON []byte

type CarrierDataset struct {
	Brand       string              `json:"brand,omitempty"`
	Operator    string              `json:"operator,omitempty"`
	MCCMNCTuple map[string][]string `json:"mccmnc_tuple,omitempty"`
}

type Carrier struct {
	Name   string `json:"name"`
	Region string `json:"region"`
	MCCMNC string `json:"mccmnc"`
}

var dictionary = mustLoadDictionary(carrierJSON)

func mustLoadDictionary(data []byte) map[string]Carrier {
	var datasets []CarrierDataset
	if err := json.Unmarshal(data, &datasets); err != nil {
		panic(fmt.Sprintf("mustLoadDictionary() error = %v", err))
	}
	dictionary := make(map[string]Carrier)
	for _, dataset := range datasets {
		name := dataset.Brand
		if name == "" {
			name = dataset.Operator
		}
		for region, tuple := range dataset.MCCMNCTuple {
			for _, mccmnc := range tuple {
				dictionary[mccmnc] = Carrier{
					Name:   name,
					Region: region,
					MCCMNC: mccmnc,
				}
			}
		}
	}
	return dictionary
}

func Lookup(mccmnc string) Carrier {
	if operator, ok := dictionary[mccmnc]; ok {
		return operator
	}
	return Carrier{
		Name:   "Unknown",
		Region: "UN",
		MCCMNC: mccmnc,
	}
}
