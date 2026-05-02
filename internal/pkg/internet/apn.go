package internet

//go:generate sh -c "curl -fsSL https://android.googlesource.com/device/sample/+/main/etc/apns-full-conf.xml?format=TEXT | base64 -d > apn.xml"

import (
	_ "embed"
	"encoding/xml"
	"fmt"
	"strings"
)

//go:embed apn.xml
var apnXML []byte

var defaultAPNs = mustDefaultAPNs(apnXML)

type apnDocument struct {
	XMLName xml.Name   `xml:"apns"`
	APNs    []apnEntry `xml:"apn"`
}

type apnEntry struct {
	MCC  string `xml:"mcc,attr"`
	MNC  string `xml:"mnc,attr"`
	APN  string `xml:"apn,attr"`
	Type string `xml:"type,attr"`
}

type apnSelection struct {
	Requested          string
	Bearer             string
	Remembered         string
	OperatorIdentifier string
	DefaultAPNs        map[string]string
}

func mustDefaultAPNs(data []byte) map[string]string {
	apns, err := defaultAPNsFromXML(data)
	if err != nil {
		panic(err)
	}
	return apns
}

func defaultAPNsFromXML(data []byte) (map[string]string, error) {
	var document apnDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse apn xml: %w", err)
	}

	apns := make(map[string]string)
	for _, entry := range document.APNs {
		apn := strings.TrimSpace(entry.APN)
		operatorIdentifier := strings.TrimSpace(entry.MCC) + strings.TrimSpace(entry.MNC)
		if apn == "" || operatorIdentifier == "" || !hasAPNType(entry.Type, "default") {
			continue
		}
		if _, exists := apns[operatorIdentifier]; exists {
			continue
		}
		apns[operatorIdentifier] = apn
	}
	return apns, nil
}

func selectAPN(selection apnSelection) string {
	if apn := firstAPN(selection.Requested, selection.Bearer, selection.Remembered); apn != "" {
		return apn
	}
	return defaultAPNFrom(selection.DefaultAPNs, selection.OperatorIdentifier)
}

func defaultAPNFrom(apns map[string]string, operatorIdentifier string) string {
	return strings.TrimSpace(apns[strings.TrimSpace(operatorIdentifier)])
}

func firstAPN(values ...string) string {
	for _, value := range values {
		if apn := strings.TrimSpace(value); apn != "" {
			return apn
		}
	}
	return ""
}

func hasAPNType(value, want string) bool {
	for apnType := range strings.SplitSeq(value, ",") {
		if strings.TrimSpace(apnType) == want {
			return true
		}
	}
	return false
}
