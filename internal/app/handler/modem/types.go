package modem

type SlotResponse struct {
	Active             bool   `json:"active"`
	OperatorName       string `json:"operatorName"`
	OperatorIdentifier string `json:"operatorIdentifier"`
	RegionCode         string `json:"regionCode"`
	Identifier         string `json:"identifier"`
}

type RegisteredOperatorResponse struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type UpdateMSISDNRequest struct {
	Number string `json:"number" validate:"required"`
}

type UpdateModemSettingsRequest struct {
	Alias      string `json:"alias"`
	Compatible *bool  `json:"compatible" validate:"required"`
	MSS        int    `json:"mss" validate:"gte=64,lte=254"`
}

type ModemSettingsResponse struct {
	Alias      string `json:"alias"`
	Compatible bool   `json:"compatible"`
	MSS        int    `json:"mss"`
}

type UpdateWiFiCallingSettingsRequest struct {
	Enabled   bool `json:"enabled"`
	Preferred bool `json:"preferred"`
}

type WiFiCallingSettingsResponse struct {
	Enabled   bool `json:"enabled"`
	Preferred bool `json:"preferred"`
}

type ModemResponse struct {
	Manufacturer         string                     `json:"manufacturer"`
	ID                   string                     `json:"id"`
	FirmwareRevision     string                     `json:"firmwareRevision"`
	HardwareRevision     string                     `json:"hardwareRevision"`
	Name                 string                     `json:"name"`
	Number               string                     `json:"number,omitempty"`
	SIM                  SlotResponse               `json:"sim"`
	Slots                []SlotResponse             `json:"slots"`
	AccessTechnology     string                     `json:"accessTechnology"`
	RegistrationState    string                     `json:"registrationState"`
	RegisteredOperator   RegisteredOperatorResponse `json:"registeredOperator"`
	SignalQuality        uint32                     `json:"signalQuality"`
	SupportsEsim         bool                       `json:"supportsEsim"`
	WiFiCallingEnabled   bool                       `json:"wifiCallingEnabled"`
	WiFiCallingPreferred bool                       `json:"wifiCallingPreferred"`
	WiFiCallingConnected bool                       `json:"wifiCallingConnected"`
}
