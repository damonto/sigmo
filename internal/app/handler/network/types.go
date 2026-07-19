package network

type NetworkResponse struct {
	Status             string   `json:"status" jsonschema:"network availability state reported by the modem, such as available, current, or forbidden"`
	OperatorName       string   `json:"operatorName" jsonschema:"long operator name advertised by the network"`
	OperatorShortName  string   `json:"operatorShortName" jsonschema:"short operator name advertised by the network"`
	OperatorCode       string   `json:"operatorCode" jsonschema:"operator code, typically the MCC and MNC; use this exact value with register_network"`
	AccessTechnologies []string `json:"accessTechnologies" jsonschema:"radio access technologies advertised for this network"`
}

type ModesResponse struct {
	Supported []ModeResponse `json:"supported" jsonschema:"network mode combinations accepted by this modem"`
	Current   ModeResponse   `json:"current" jsonschema:"currently configured network mode combination"`
}

type ModeResponse struct {
	Allowed        uint32 `json:"allowed" jsonschema:"numeric modem mode bitmask allowed by this combination"`
	Preferred      uint32 `json:"preferred" jsonschema:"numeric preferred modem mode within the allowed bitmask; zero means no preference"`
	AllowedLabel   string `json:"allowedLabel" jsonschema:"human-readable label for the allowed mode bitmask"`
	PreferredLabel string `json:"preferredLabel" jsonschema:"human-readable label for the preferred mode"`
	Current        bool   `json:"current" jsonschema:"whether this supported combination is currently configured"`
}

type SetCurrentModesRequest struct {
	Allowed   uint32 `json:"allowed"`
	Preferred uint32 `json:"preferred"`
}

type BandsResponse struct {
	Supported []BandResponse `json:"supported" jsonschema:"bands accepted by this modem"`
	Current   []uint32       `json:"current" jsonschema:"numeric values of the currently configured bands"`
}

type BandResponse struct {
	Value   uint32 `json:"value" jsonschema:"numeric modem band value used when configuring bands"`
	Label   string `json:"label" jsonschema:"human-readable modem band label"`
	Current bool   `json:"current" jsonschema:"whether this band is currently configured"`
}

type SetCurrentBandsRequest struct {
	Bands []uint32 `json:"bands"`
}

type AirplaneModeResponse struct {
	Supported bool `json:"supported" jsonschema:"whether Sigmo can control airplane mode for this modem"`
	Enabled   bool `json:"enabled" jsonschema:"whether airplane mode is currently enabled"`
}

type SetAirplaneModeRequest struct {
	Enabled bool `json:"enabled"`
}
