//go:build esim_transfer

package esimtransfer

type SourceResponse struct {
	Type               SourceType `json:"type"`
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Detail             string     `json:"detail,omitempty"`
	RequiresSourceIMEI bool       `json:"requiresSourceImei"`
}

type SourcesResponse struct {
	Sources   []SourceResponse `json:"sources"`
	CCIDError string           `json:"ccidError,omitempty"`
}

type ProfilesRequest struct {
	SourceType SourceType `json:"sourceType"`
	SourceID   string     `json:"sourceId"`
	SourceIMEI string     `json:"sourceImei,omitempty"`
}

type ProfileResponse struct {
	ID                  string      `json:"id"`
	Type                ProfileType `json:"type"`
	Name                string      `json:"name"`
	ServiceProviderName string      `json:"serviceProviderName,omitempty"`
	ICCID               string      `json:"iccid"`
	Icon                string      `json:"icon,omitempty"`
	RegionCode          string      `json:"regionCode,omitempty"`
	Enabled             bool        `json:"enabled"`
	Supported           bool        `json:"supported"`
	UnsupportedReason   string      `json:"unsupportedReason,omitempty"`
	CarrierName         string      `json:"carrierName,omitempty"`
}

type clientMessage struct {
	Type       string     `json:"type"`
	SourceType SourceType `json:"sourceType,omitempty"`
	SourceID   string     `json:"sourceId,omitempty"`
	ProfileID  string     `json:"profileId,omitempty"`
	SourceIMEI string     `json:"sourceImei,omitempty"`
	Accept     *bool      `json:"accept,omitempty"`
	Response   string     `json:"response,omitempty"`
}

type serverMessage struct {
	Type    string                  `json:"type"`
	Stage   string                  `json:"stage,omitempty"`
	Message string                  `json:"message,omitempty"`
	ICCID   string                  `json:"iccid,omitempty"`
	Input   *userInputMessage       `json:"input,omitempty"`
	Profile *downloadProfilePreview `json:"profile,omitempty"`
}

type userInputMessage struct {
	Text         string `json:"text"`
	AcceptLabel  string `json:"acceptLabel,omitempty"`
	RejectLabel  string `json:"rejectLabel,omitempty"`
	FreeText     bool   `json:"freeText"`
	FreeTextHint string `json:"freeTextHint,omitempty"`
}

type downloadProfilePreview struct {
	ICCID               string `json:"iccid"`
	ServiceProviderName string `json:"serviceProviderName"`
	ProfileName         string `json:"profileName"`
	ProfileNickname     string `json:"profileNickname,omitempty"`
	ProfileState        string `json:"profileState"`
	Icon                string `json:"icon,omitempty"`
	RegionCode          string `json:"regionCode,omitempty"`
}
