package euicc

type SASUPResponse struct {
	Name   string `json:"name" jsonschema:"name of the eSIM security accreditation scheme reported by the secure element"`
	Region string `json:"region" jsonschema:"region associated with the security accreditation; empty when unspecified"`
}

type SEsResponse struct {
	SEs []SEItemResponse `json:"ses" jsonschema:"secure elements available for eSIM operations"`
}

type SEItemResponse struct {
	ID           string        `json:"id" jsonschema:"Sigmo secure-element identifier; use this exact value as seId in eSIM tools"`
	Label        string        `json:"label" jsonschema:"human-readable secure-element label"`
	AID          string        `json:"aid" jsonschema:"hexadecimal application identifier used to address the eUICC application"`
	EID          string        `json:"eid" jsonschema:"eUICC identifier; empty when it cannot be read"`
	FreeSpace    int32         `json:"freeSpace" jsonschema:"free eUICC profile storage in bytes"`
	SASUP        SASUPResponse `json:"sasUp" jsonschema:"security accreditation information reported by the eUICC"`
	Certificates []string      `json:"certificates" jsonschema:"eUICC certificate identifiers"`
}
