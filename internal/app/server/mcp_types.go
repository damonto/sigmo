package server

type successOutput struct {
	Success bool `json:"success" jsonschema:"whether Sigmo completed the requested operation"`
}
