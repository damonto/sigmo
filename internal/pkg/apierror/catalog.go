package apierror

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

// The embedded catalog is immutable after startup. A corrupt checked-in catalog
// is a build defect, not a recoverable request error.
var titles = func() map[string]string {
	var result map[string]string
	if err := json.Unmarshal(catalogJSON, &result); err != nil {
		panic("invalid HTTP problem catalog: " + err.Error())
	}
	return result
}()

// Title returns the stable title assigned to an application problem type.
func Title(code string) (string, bool) {
	title, ok := titles[code]
	return title, ok
}

// CodeFromType recognizes only exact registered URIs, never a URI's final path
// segment or an untrusted error_code claiming to identify another type.
func CodeFromType(problemType string) (string, bool) {
	code, ok := strings.CutPrefix(problemType, TypePrefix)
	if !ok {
		return "", false
	}
	_, ok = titles[code]
	return code, ok
}
