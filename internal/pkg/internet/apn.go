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
	MCC      string `xml:"mcc,attr"`
	MNC      string `xml:"mnc,attr"`
	APN      string `xml:"apn,attr"`
	Type     string `xml:"type,attr"`
	Protocol string `xml:"protocol,attr"`
	User     string `xml:"user,attr"`
	Password string `xml:"password,attr"`
	AuthType string `xml:"authtype,attr"`
}

type apnProfile struct {
	APN      string
	IPType   string
	Username string
	Password string
	Auth     string
}

type apnSelection struct {
	Requested          string
	Bearer             string
	Remembered         string
	OperatorIdentifier string
	DefaultAPNs        map[string]apnProfile
}

func mustDefaultAPNs(data []byte) map[string]apnProfile {
	apns, err := defaultAPNsFromXML(data)
	if err != nil {
		panic(err)
	}
	return apns
}

func defaultAPNsFromXML(data []byte) (map[string]apnProfile, error) {
	var document apnDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse apn xml: %w", err)
	}

	apns := make(map[string]apnProfile)
	for _, entry := range document.APNs {
		apn := strings.TrimSpace(entry.APN)
		operatorIdentifier := strings.TrimSpace(entry.MCC) + strings.TrimSpace(entry.MNC)
		if apn == "" || operatorIdentifier == "" || !hasAPNType(entry.Type, "default") {
			continue
		}
		if _, exists := apns[operatorIdentifier]; exists {
			continue
		}
		apns[operatorIdentifier] = apnProfile{
			APN:      apn,
			IPType:   androidProtocol(entry.Protocol),
			Username: strings.TrimSpace(entry.User),
			Password: entry.Password,
			Auth:     androidAuthType(entry.AuthType),
		}
	}
	return apns, nil
}

func selectAPN(selection apnSelection) string {
	if apn := firstAPN(selection.Requested, selection.Bearer, selection.Remembered); apn != "" {
		return apn
	}
	return defaultAPNFrom(selection.DefaultAPNs, selection.OperatorIdentifier)
}

func defaultAPNFrom(apns map[string]apnProfile, operatorIdentifier string) string {
	return defaultAPNProfileFrom(apns, operatorIdentifier).APN
}

func defaultAPNProfileFrom(apns map[string]apnProfile, operatorIdentifier string) apnProfile {
	profile := apns[strings.TrimSpace(operatorIdentifier)]
	profile.APN = strings.TrimSpace(profile.APN)
	profile.IPType = strings.ToLower(strings.TrimSpace(profile.IPType))
	profile.Username = strings.TrimSpace(profile.Username)
	profile.Auth = strings.ToLower(strings.TrimSpace(profile.Auth))
	return profile
}

func androidProtocol(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "IP":
		return "ipv4"
	case "IPV6":
		return "ipv6"
	case "IPV4V6":
		return "ipv4v6"
	default:
		return ""
	}
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

func androidAuthType(value string) string {
	switch strings.TrimSpace(value) {
	case "0":
		return "none"
	case "1":
		return "pap"
	case "2":
		return "chap"
	case "3":
		return "pap|chap"
	default:
		return ""
	}
}
