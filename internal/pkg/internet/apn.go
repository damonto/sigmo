package internet

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed apns.json
var apnJSON []byte

var defaultAPNs = mustDefaultAPNs(apnJSON)

type apnEntry struct {
	MCC      string `json:"mcc"`
	MNC      string `json:"mnc"`
	GID1     string `json:"gid1"`
	SPN      string `json:"spn"`
	ICCID    string `json:"iccid"`
	IMSI     string `json:"imsi"`
	APN      string `json:"apn"`
	Protocol string `json:"protocol"`
	User     string `json:"user"`
	Password string `json:"pass"`
	AuthType *int   `json:"authType"`
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
	GID1               string
	SPN                string
	ICCID              string
	IMSI               string
	DefaultAPNs        []apnRecord
}

type apnCriteria struct {
	GID1  string
	SPN   string
	ICCID string
	IMSI  string
}

type apnRecord struct {
	OperatorIdentifier string
	Criteria           apnCriteria
	Profile            apnProfile
}

func mustDefaultAPNs(data []byte) []apnRecord {
	apns, err := defaultAPNsFromJSON(data)
	if err != nil {
		panic(err)
	}
	return apns
}

func defaultAPNsFromJSON(data []byte) ([]apnRecord, error) {
	var entries []apnEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse apn json: %w", err)
	}

	apns := make([]apnRecord, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		apn := strings.TrimSpace(entry.APN)
		mcc := strings.TrimSpace(entry.MCC)
		mnc := strings.TrimSpace(entry.MNC)
		if apn == "" || mcc == "" || mnc == "" {
			continue
		}
		key := apnKey(mcc+mnc, apnCriteria{
			GID1:  entry.GID1,
			SPN:   entry.SPN,
			ICCID: entry.ICCID,
			IMSI:  entry.IMSI,
		})
		if seen[key] {
			continue
		}
		seen[key] = true
		apns = append(apns, apnRecord{
			OperatorIdentifier: mcc + mnc,
			Criteria: apnCriteria{
				GID1:  normalizeGID1(entry.GID1),
				SPN:   normalizeSPN(entry.SPN),
				ICCID: normalizeICCID(entry.ICCID),
				IMSI:  normalizeIMSI(entry.IMSI),
			},
			Profile: apnProfile{
				APN:      apn,
				IPType:   androidProtocol(entry.Protocol),
				Username: strings.TrimSpace(entry.User),
				Password: entry.Password,
				Auth:     androidAuthType(entry.AuthType),
			},
		})
	}
	return apns, nil
}

func selectAPN(selection apnSelection) string {
	if apn := firstAPN(selection.Requested, selection.Bearer, selection.Remembered); apn != "" {
		return apn
	}
	return defaultAPNFrom(selection.DefaultAPNs, selection.OperatorIdentifier, apnCriteria{
		GID1:  selection.GID1,
		SPN:   selection.SPN,
		ICCID: selection.ICCID,
		IMSI:  selection.IMSI,
	})
}

func defaultAPNFrom(apns []apnRecord, operatorIdentifier string, criteria apnCriteria) string {
	return defaultAPNProfileFrom(apns, operatorIdentifier, criteria).APN
}

func defaultAPNProfileFrom(apns []apnRecord, operatorIdentifier string, criteria apnCriteria) apnProfile {
	operatorIdentifier = strings.TrimSpace(operatorIdentifier)
	criteria = cleanAPNCriteria(criteria)
	bestScore := -1
	var best apnProfile
	for _, record := range apns {
		if record.OperatorIdentifier != operatorIdentifier {
			continue
		}
		score, ok := apnMatchScore(record.Criteria, criteria)
		if !ok || score <= bestScore {
			continue
		}
		bestScore = score
		best = record.Profile
	}
	return cleanAPNProfile(best)
}

func cleanAPNProfile(profile apnProfile) apnProfile {
	profile.APN = strings.TrimSpace(profile.APN)
	profile.IPType = strings.ToLower(strings.TrimSpace(profile.IPType))
	profile.Username = strings.TrimSpace(profile.Username)
	profile.Auth = strings.ToLower(strings.TrimSpace(profile.Auth))
	return profile
}

func apnKey(operatorIdentifier string, criteria apnCriteria) string {
	operatorIdentifier = strings.TrimSpace(operatorIdentifier)
	criteria = cleanAPNCriteria(criteria)
	parts := []string{operatorIdentifier}
	if criteria.GID1 != "" {
		parts = append(parts, "gid1="+criteria.GID1)
	}
	if criteria.SPN != "" {
		parts = append(parts, "spn="+criteria.SPN)
	}
	if criteria.ICCID != "" {
		parts = append(parts, "iccid="+criteria.ICCID)
	}
	if criteria.IMSI != "" {
		parts = append(parts, "imsi="+criteria.IMSI)
	}
	return strings.Join(parts, "|")
}

func cleanAPNCriteria(criteria apnCriteria) apnCriteria {
	return apnCriteria{
		GID1:  normalizeGID1(criteria.GID1),
		SPN:   normalizeSPN(criteria.SPN),
		ICCID: normalizeICCID(criteria.ICCID),
		IMSI:  normalizeIMSI(criteria.IMSI),
	}
}

func apnMatchScore(want, got apnCriteria) (int, bool) {
	score := 0
	if want.GID1 != "" {
		if got.GID1 != want.GID1 {
			return 0, false
		}
		score += 2000 + len(want.GID1)
	}
	if want.SPN != "" {
		if got.SPN != want.SPN {
			return 0, false
		}
		score += 1000 + len(want.SPN)
	}
	if want.ICCID != "" {
		if got.ICCID == "" || !strings.HasPrefix(got.ICCID, want.ICCID) {
			return 0, false
		}
		score += 4000 + len(want.ICCID)
	}
	if want.IMSI != "" {
		if got.IMSI == "" || !imsiPatternMatches(want.IMSI, got.IMSI) {
			return 0, false
		}
		score += 3000 + imsiSpecificity(want.IMSI)
	}
	return score, true
}

func imsiPatternMatches(pattern, imsi string) bool {
	pattern = normalizeIMSI(pattern)
	imsi = normalizeIMSI(imsi)
	if pattern == "" || len(pattern) > len(imsi) {
		return false
	}
	for i := range pattern {
		if pattern[i] == 'X' {
			continue
		}
		if pattern[i] != imsi[i] {
			return false
		}
	}
	return true
}

func imsiSpecificity(pattern string) int {
	score := len(pattern)
	for _, ch := range pattern {
		if ch != 'X' {
			score++
		}
	}
	return score
}

func normalizeGID1(gid1 string) string {
	return strings.ToUpper(strings.TrimSpace(gid1))
}

func normalizeSPN(spn string) string {
	return strings.ToUpper(strings.TrimSpace(spn))
}

func normalizeICCID(iccid string) string {
	return strings.TrimSpace(iccid)
}

func normalizeIMSI(imsi string) string {
	return strings.ToUpper(strings.TrimSpace(imsi))
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

func androidAuthType(value *int) string {
	if value == nil {
		return ""
	}
	switch *value {
	case 0:
		return "none"
	case 1:
		return "pap"
	case 2:
		return "chap"
	case 3:
		return "pap|chap"
	default:
		return ""
	}
}
