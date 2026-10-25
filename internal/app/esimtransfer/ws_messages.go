package esimtransfer

import "github.com/damonto/sigmo/internal/app/websheet"

type wsClientMessage struct {
	Version    int        `json:"version,omitempty"`
	Card       *cardReply `json:"card,omitempty"`
	Type       string     `json:"type"`
	SEID       string     `json:"seId,omitempty"`
	SourceType SourceType `json:"sourceType,omitempty"`
	SourceID   string     `json:"sourceId,omitempty"`
	ProfileID  string     `json:"profileId,omitempty"`
	SourceIMEI string     `json:"sourceImei,omitempty"`
	Accept     *bool      `json:"accept,omitempty"`
	Response   string     `json:"response,omitempty"`
}

type wsServerMessage struct {
	Version  int                `json:"version,omitempty"`
	Card     *cardRequest       `json:"card,omitempty"`
	Profiles []ProfileResponse  `json:"profiles,omitempty"`
	Type     string             `json:"type"`
	Stage    string             `json:"stage,omitempty"`
	Message  string             `json:"message,omitempty"`
	ICCID    string             `json:"iccid,omitempty"`
	Input    *wsUserInputPrompt `json:"input,omitempty"`
	Profile  *downloadPreview   `json:"profile,omitempty"`
	Websheet *websheet.Info     `json:"websheet,omitempty"`
}

type wsUserInputPrompt struct {
	Text         string `json:"text"`
	AcceptLabel  string `json:"acceptLabel,omitempty"`
	RejectLabel  string `json:"rejectLabel,omitempty"`
	FreeText     bool   `json:"freeText"`
	FreeTextHint string `json:"freeTextHint,omitempty"`
}

type downloadPreview struct {
	ICCID               string               `json:"iccid"`
	ServiceProviderName string               `json:"serviceProviderName"`
	ProfileName         string               `json:"profileName"`
	ProfileNickname     string               `json:"profileNickname,omitempty"`
	ProfileState        string               `json:"profileState"`
	ProfileOwner        ProfileOwnerResponse `json:"profileOwner"`
	Icon                string               `json:"icon,omitempty"`
	RegionCode          string               `json:"regionCode,omitempty"`
}
