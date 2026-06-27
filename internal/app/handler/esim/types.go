package esim

type ProfileResponse struct {
	Name                string               `json:"name"`
	ServiceProviderName string               `json:"serviceProviderName"`
	ICCID               string               `json:"iccid"`
	ISDPAID             string               `json:"isdPAID,omitempty"`
	Icon                string               `json:"icon"`
	ProfileName         string               `json:"profileName"`
	ProfileNickname     string               `json:"profileNickname,omitempty"`
	ProfileState        uint8                `json:"profileState"`
	ProfileStateName    string               `json:"profileStateName"`
	ProfileClass        string               `json:"profileClass"`
	ProfileOwner        ProfileOwnerResponse `json:"profileOwner"`
	RegionCode          string               `json:"regionCode,omitempty"`
}

type ProfileOwnerResponse struct {
	MCC  string `json:"mcc"`
	MNC  string `json:"mnc"`
	GID1 string `json:"gid1,omitempty"`
	GID2 string `json:"gid2,omitempty"`
}

type DiscoverResponse struct {
	EventID string `json:"eventId"`
	Address string `json:"address"`
}

type UpdateNicknameRequest struct {
	Nickname string `json:"nickname"`
}

type downloadClientMessage struct {
	Type             string `json:"type"`
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
	ProfileOwner        ProfileOwnerResponse `json:"profileOwner"`
	Icon                string               `json:"icon,omitempty"`
	RegionCode          string               `json:"regionCode,omitempty"`
}
