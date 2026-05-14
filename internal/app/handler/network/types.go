package network

type NetworkResponse struct {
	Status             string   `json:"status"`
	OperatorName       string   `json:"operatorName"`
	OperatorShortName  string   `json:"operatorShortName"`
	OperatorCode       string   `json:"operatorCode"`
	AccessTechnologies []string `json:"accessTechnologies"`
}

type ModesResponse struct {
	Supported []ModeResponse `json:"supported"`
	Current   ModeResponse   `json:"current"`
}

type ModeResponse struct {
	Allowed        uint32 `json:"allowed"`
	Preferred      uint32 `json:"preferred"`
	AllowedLabel   string `json:"allowedLabel"`
	PreferredLabel string `json:"preferredLabel"`
	Current        bool   `json:"current"`
}

type SetCurrentModesRequest struct {
	Allowed   uint32 `json:"allowed"`
	Preferred uint32 `json:"preferred"`
}

type BandsResponse struct {
	Supported []BandResponse `json:"supported"`
	Current   []uint32       `json:"current"`
}

type BandResponse struct {
	Value   uint32 `json:"value"`
	Label   string `json:"label"`
	Current bool   `json:"current"`
}

type SetCurrentBandsRequest struct {
	Bands []uint32 `json:"bands"`
}
