//go:build ims

package main

type proSuccessOutput struct {
	Success bool `json:"success" jsonschema:"whether Sigmo completed the requested operation"`
}
