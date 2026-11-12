package esim

import appesim "github.com/damonto/sigmo/internal/app/esim"

type UpdateNicknameRequest struct {
	Nickname string `json:"nickname"`
}

type downloadClientMessage struct {
	Type             string `json:"type"`
	SEID             string `json:"seId,omitempty"`
	SMDP             string `json:"smdp,omitempty"`
	ActivationCode   string `json:"activationCode,omitempty"`
	ConfirmationCode string `json:"confirmationCode,omitempty"`
	Accept           *bool  `json:"accept,omitempty"`
	Code             string `json:"code,omitempty"`
}

type downloadServerMessage struct {
	Type    string                  `json:"type"`
	Stage   string                  `json:"stage,omitempty"`
	Profile *downloadProfilePreview `json:"profile,omitempty"`
	Message string                  `json:"message,omitempty"`
}

type downloadProfilePreview struct {
	ICCID               string               `json:"iccid"`
	ServiceProviderName string               `json:"serviceProviderName"`
	ProfileName         string               `json:"profileName"`
	ProfileNickname     string               `json:"profileNickname,omitempty"`
	ProfileState        string               `json:"profileState"`
	ProfileOwner        appesim.ProfileOwner `json:"profileOwner"`
	Icon                string               `json:"icon,omitempty"`
	RegionCode          string               `json:"regionCode,omitempty"`
}
