package euicc

type SASUPResponse struct {
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
}

type EuiccResponse struct {
	EID          string        `json:"eid"`
	FreeSpace    int32         `json:"freeSpace"`
	SASUP        SASUPResponse `json:"sasUp"`
	Certificates []string      `json:"certificates"`
}
