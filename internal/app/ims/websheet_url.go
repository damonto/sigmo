package ims

import (
	"net/url"
	"strings"
)

func wfcUserActionData(rawData string) string {
	data := strings.TrimLeft(strings.TrimSpace(rawData), "?&")
	values, ok := parseWFCUserActionData(data)
	if !ok {
		return data
	}
	return values.Encode()
}

func parseWFCUserActionData(data string) (url.Values, bool) {
	values, err := url.ParseQuery(data)
	if err == nil && !isEncodedQueryBlob(values) {
		return values, true
	}
	decoded := decodeWFCQueryDelimiters(data)
	if decoded == data {
		return nil, false
	}
	values, err = url.ParseQuery(strings.TrimLeft(decoded, "?&"))
	if err != nil {
		return nil, false
	}
	return values, true
}

func isEncodedQueryBlob(values url.Values) bool {
	if len(values) != 1 {
		return false
	}
	for key, items := range values {
		return len(items) == 1 && items[0] == "" && strings.ContainsAny(key, "=&")
	}
	return false
}

func decodeWFCQueryDelimiters(data string) string {
	var b strings.Builder
	changed := false
	for i := 0; i < len(data); i++ {
		if data[i] == '%' && i+2 < len(data) {
			if delimiter, ok := encodedQueryDelimiter(data[i+1], data[i+2]); ok {
				b.WriteByte(delimiter)
				i += 2
				changed = true
				continue
			}
		}
		b.WriteByte(data[i])
	}
	if !changed {
		return data
	}
	return b.String()
}

func encodedQueryDelimiter(hi, lo byte) (byte, bool) {
	value, ok := hexByte(hi, lo)
	if !ok {
		return 0, false
	}
	switch value {
	case '=', '&', '?':
		return value, true
	default:
		return 0, false
	}
}

func hexByte(hi, lo byte) (byte, bool) {
	h, ok := hexNibble(hi)
	if !ok {
		return 0, false
	}
	l, ok := hexNibble(lo)
	if !ok {
		return 0, false
	}
	return h<<4 | l, true
}

func hexNibble(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}
