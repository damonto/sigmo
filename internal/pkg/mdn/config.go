// Package mdn retrieves a SIM's phone number through carrier entitlements.
package mdn

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var (
	// ErrUnsupported means no compatible, enabled carrier configuration matches.
	ErrUnsupported = errors.New("phone-number entitlement is unsupported")
	// ErrAmbiguous means equally specific rules select different carrier services.
	ErrAmbiguous     = errors.New("ambiguous phone-number entitlement configuration")
	digits           = regexp.MustCompile(`^[0-9]+$`)
	imsiPattern      = regexp.MustCompile(`^[0-9xX]{1,15}$`)
	hexDigits        = regexp.MustCompile(`^[0-9A-F]+$`)
	identityVariable = regexp.MustCompile(`\$[A-Za-z][A-Za-z0-9_]*`)
	countryCode      = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

// Identity contains the current SIM identity and the modem's actual IMEI.
type Identity struct {
	MCC, MNC, IMSI, ICCID, IMEI, SPN, GID1, GID2 string
	MNCLength                                    int
}

// Profile describes a carrier service and the SIM identities it applies to.
// Disabled rules prevent a specific MVNO from inheriting a broader service.
type Profile struct {
	CarrierName      string `json:"carrierName,omitempty"`
	MCC              string `json:"mcc"`
	MNC              string `json:"mnc"`
	IMSIPrefix       string `json:"imsiPrefix,omitempty"`
	ICCIDPrefix      string `json:"iccidPrefix,omitempty"`
	SPN              string `json:"spn,omitempty"`
	GID1             string `json:"gid1,omitempty"`
	GID2             string `json:"gid2,omitempty"`
	Disabled         bool   `json:"disabled,omitempty"`
	Server           string `json:"entitlementServer,omitempty"`
	ProtocolVersion  string `json:"protocolVersion,omitempty"`
	IdentityTemplate string `json:"identityTemplate,omitempty"`
	UserAgent        string `json:"userAgent,omitempty"`
	CountryISO       string `json:"countryISO,omitempty"`
	SigningVersion   int    `json:"signingVersion,omitempty"`
}

// Configs is an immutable carrier catalog after successful decoding.
type Configs struct{ profiles []Profile }

// Equal reports whether two catalogs contain identical normalized profiles.
// Reloading the same catalog must not reset a lookup's retry policy.
func (c *Configs) Equal(other *Configs) bool {
	if c == nil {
		return other == nil || len(other.profiles) == 0
	}
	if other == nil {
		return len(c.profiles) == 0
	}
	return slices.Equal(c.profiles, other.profiles)
}

// Empty reports whether the catalog contains no enabled services.
func (c *Configs) Empty() bool {
	if c != nil {
		for _, p := range c.profiles {
			if !p.Disabled {
				return false
			}
		}
	}
	return true
}

// MarshalJSON returns an independent JSON representation of the catalog.
func (c Configs) MarshalJSON() ([]byte, error) {
	if len(c.profiles) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(c.profiles)
}

// UnmarshalJSON validates an array and replaces the catalog only on success.
func (c *Configs) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		return errors.New("decode MDN profiles: expected array")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var profiles []Profile
	if err := decoder.Decode(&profiles); err != nil {
		return fmt.Errorf("decode MDN profiles: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return errors.New("decode MDN profiles: unexpected trailing data")
	}
	seen := make(map[[7]string]Profile)
	for i := range profiles {
		p := &profiles[i]
		p.MCC = strings.TrimSpace(p.MCC)
		p.MNC = strings.TrimSpace(p.MNC)
		p.IMSIPrefix = strings.ToUpper(strings.TrimSpace(p.IMSIPrefix))
		p.ICCIDPrefix = strings.TrimSpace(p.ICCIDPrefix)
		p.SPN = strings.TrimSpace(p.SPN)
		p.GID1 = normalizeGID(p.GID1)
		p.GID2 = normalizeGID(p.GID2)
		if err := p.validate(); err != nil {
			return fmt.Errorf("MDN profile %d: %w", i, err)
		}
		key := [7]string{p.MCC, p.MNC, p.IMSIPrefix, p.ICCIDPrefix, strings.ToUpper(p.SPN), p.GID1, p.GID2}
		if previous, ok := seen[key]; ok && !sameService(previous, *p) {
			return ErrAmbiguous
		}
		seen[key] = *p
	}
	c.profiles = profiles
	return nil
}

func (p Profile) validate() error {
	if len(p.MCC) != 3 || !digits.MatchString(p.MCC) || (len(p.MNC) != 2 && len(p.MNC) != 3) || !digits.MatchString(p.MNC) {
		return errors.New("invalid MCC/MNC")
	}
	if p.IMSIPrefix != "" && !imsiPattern.MatchString(p.IMSIPrefix) {
		return errors.New("invalid IMSI prefix")
	}
	if p.ICCIDPrefix != "" && !digits.MatchString(p.ICCIDPrefix) {
		return errors.New("invalid ICCID prefix")
	}
	for _, gid := range []string{p.GID1, p.GID2} {
		if gid != "" && !hexDigits.MatchString(gid) {
			return errors.New("invalid GID")
		}
	}
	if p.Disabled {
		return nil
	}
	endpoint, err := url.Parse(p.Server)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return errors.New("MDN server must be an HTTPS URL")
	}
	if p.ProtocolVersion != "2" || p.SigningVersion != 2 {
		return errors.New("unsupported MDN protocol or signing version")
	}
	if p.UserAgent == "" || strings.ContainsAny(p.UserAgent, "\r\n$") {
		return errors.New("invalid MDN user agent")
	}
	if !countryCode.MatchString(p.CountryISO) {
		return errors.New("invalid MDN country code")
	}
	if p.IdentityTemplate != "" {
		hasIMSI := false
		for _, variable := range identityVariable.FindAllString(p.IdentityTemplate, -1) {
			switch variable {
			case "$IMSI":
				hasIMSI = true
			case "$MCC", "$MNC":
			default:
				return errors.New("unsupported MDN identity template")
			}
		}
		remaining := identityVariable.ReplaceAllString(p.IdentityTemplate, "")
		if !hasIMSI || strings.ContainsAny(remaining, "$\r\n") {
			return errors.New("unsupported MDN identity template")
		}
	}
	return nil
}

// HasPLMN cheaply determines whether a SIM may match an enabled service.
func (c *Configs) HasPLMN(identity Identity) bool {
	if c == nil {
		return false
	}
	for _, p := range c.profiles {
		if !p.Disabled && samePLMN(p, identity) {
			return true
		}
	}
	return false
}

// Select returns the most specific matching profile, rejecting conflicting ties.
func (c *Configs) Select(identity Identity) (Profile, error) {
	if c == nil {
		return Profile{}, ErrUnsupported
	}
	var best Profile
	var bestScore [3]int
	found := false
	ambiguous := false
	for _, p := range c.profiles {
		if !matches(p, identity) {
			continue
		}
		score := matchScore(p)
		comparison := slices.Compare(score[:], bestScore[:])
		if !found || comparison > 0 {
			best = p
			bestScore = score
			found = true
			ambiguous = false
			continue
		}
		if comparison == 0 && !sameService(best, p) {
			ambiguous = true
		}
	}
	if ambiguous {
		return Profile{}, ErrAmbiguous
	}
	if !found || best.Disabled {
		return Profile{}, ErrUnsupported
	}
	return best, nil
}

func samePLMN(p Profile, id Identity) bool {
	if p.MCC != id.MCC {
		return false
	}
	return p.MNC == id.MNC || (id.MNCLength == 2 && len(p.MNC) == 2 && len(id.MNC) == 3 && id.MNC[0] == '0' && p.MNC == id.MNC[1:])
}

func matches(p Profile, id Identity) bool {
	if !samePLMN(p, id) || !strings.HasPrefix(id.ICCID, p.ICCIDPrefix) || len(p.IMSIPrefix) > len(id.IMSI) {
		return false
	}
	for i := range len(p.IMSIPrefix) {
		if p.IMSIPrefix[i] != 'X' && p.IMSIPrefix[i] != id.IMSI[i] {
			return false
		}
	}
	if p.SPN != "" && !strings.EqualFold(p.SPN, strings.TrimSpace(id.SPN)) {
		return false
	}
	return strings.HasPrefix(normalizeGID(id.GID1), p.GID1) && strings.HasPrefix(normalizeGID(id.GID2), p.GID2)
}

func normalizeGID(value string) string {
	return strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "0x"), "0X"))
}

func matchScore(p Profile) [3]int {
	var score [3]int
	for _, value := range []string{p.IMSIPrefix, p.ICCIDPrefix, p.SPN, p.GID1, p.GID2} {
		if value != "" {
			score[0]++
			score[1] += len(value)
			score[2] += len(value)
		}
	}
	score[1] -= strings.Count(p.IMSIPrefix, "X")
	return score
}

func sameService(a, b Profile) bool {
	return a.Disabled == b.Disabled && a.Server == b.Server &&
		a.ProtocolVersion == b.ProtocolVersion && a.IdentityTemplate == b.IdentityTemplate &&
		a.UserAgent == b.UserAgent && a.CountryISO == b.CountryISO && a.SigningVersion == b.SigningVersion
}
