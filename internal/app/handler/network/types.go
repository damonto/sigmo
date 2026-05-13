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

type CellInfoResponse struct {
	Type            string   `json:"type"`
	TypeValue       uint32   `json:"typeValue"`
	Serving         bool     `json:"serving"`
	OperatorID      string   `json:"operatorId,omitempty"`
	LAC             string   `json:"lac,omitempty"`
	TAC             string   `json:"tac,omitempty"`
	CellID          string   `json:"cellId,omitempty"`
	PhysicalCellID  string   `json:"physicalCellId,omitempty"`
	ARFCN           *uint32  `json:"arfcn,omitempty"`
	UARFCN          *uint32  `json:"uarfcn,omitempty"`
	EARFCN          *uint32  `json:"earfcn,omitempty"`
	NRARFCN         *uint32  `json:"nrarfcn,omitempty"`
	RSRP            *float64 `json:"rsrp,omitempty"`
	RSRQ            *float64 `json:"rsrq,omitempty"`
	SINR            *float64 `json:"sinr,omitempty"`
	TimingAdvance   *uint32  `json:"timingAdvance,omitempty"`
	Bandwidth       *uint32  `json:"bandwidth,omitempty"`
	ServingCellType *uint32  `json:"servingCellType,omitempty"`
}
