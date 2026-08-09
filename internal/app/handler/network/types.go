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
	Allowed        uint64 `json:"allowed" jsonschema:"wwan-go radio technology bitmask allowed by this combination"`
	Preferred      uint64 `json:"preferred" jsonschema:"preferred wwan-go radio technology within the allowed bitmask; zero means no preference"`
	AllowedLabel   string `json:"allowedLabel" jsonschema:"human-readable label for the allowed mode bitmask"`
	PreferredLabel string `json:"preferredLabel" jsonschema:"human-readable label for the preferred mode"`
	Current        bool   `json:"current" jsonschema:"whether this supported combination is currently configured"`
}

type SetCurrentModesRequest struct {
	Allowed   uint64 `json:"allowed"`
	Preferred uint64 `json:"preferred"`
}

type BandsResponse struct {
	Supported []BandResponse `json:"supported" jsonschema:"bands accepted by this modem"`
	Current   []BandValue    `json:"current" jsonschema:"currently configured semantic radio bands"`
}

type BandResponse struct {
	Value   BandValue `json:"value" jsonschema:"semantic radio technology and band number"`
	Label   string    `json:"label" jsonschema:"human-readable modem band label"`
	Current bool      `json:"current" jsonschema:"whether this band is currently configured"`
}

type BandValue struct {
	Technology uint64 `json:"technology" jsonschema:"wwan-go radio technology value"`
	Number     uint16 `json:"number" jsonschema:"3GPP or modem band number"`
}

type SetCurrentBandsRequest struct {
	Bands []BandValue `json:"bands"`
}

type AirplaneModeResponse struct {
	Supported bool `json:"supported" jsonschema:"whether Sigmo can control airplane mode for this modem"`
	Enabled   bool `json:"enabled" jsonschema:"whether airplane mode is currently enabled"`
}

type SetAirplaneModeRequest struct {
	Enabled bool `json:"enabled"`
}
