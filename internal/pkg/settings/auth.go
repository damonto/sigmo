package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Auth configures one explicit trust boundary for the instance.
type Auth struct {
	Mode             string           `json:"mode"`
	SessionMaxDays   int              `json:"sessionMaxDays"`
	OTP              OTP              `json:"otp"`
	OIDC             OIDC             `json:"oidc"`
	CloudflareAccess CloudflareAccess `json:"cloudflareAccess"`
}

// OTP references the same delivery configuration used by business notifications.
type OTP struct {
	Channels []string `json:"channels"`
}

// OIDC holds a confidential server client's configuration and explicit admission list.
type OIDC struct {
	Issuer          string   `json:"issuer"`
	ClientID        string   `json:"clientId"`
	ClientSecret    string   `json:"clientSecret"`
	AllowedSubjects []string `json:"allowedSubjects"`
}

// CloudflareAccess pins the issuer and audience of the required edge assertion.
type CloudflareAccess struct {
	TeamDomain      string   `json:"teamDomain"`
	Audience        string   `json:"audience"`
	AllowedSubjects []string `json:"allowedSubjects"`
}

// ValidationOptions relax rules that are temporarily incomplete during an authentication test.
type ValidationOptions struct {
	AllowEmptyAccessSubjects bool
}

// HasMethod reports whether the selected mode permits this login method.
func (a Auth) HasMethod(method string) bool {
	return (method == "otp" || method == "oidc") && a.Mode == method
}

// ApplyDefaults fills only absent values; unknown modes never become anonymous.
func (a *Auth) ApplyDefaults() {
	if a.Mode == "" {
		a.Mode = "none"
	}
	if a.SessionMaxDays == 0 {
		a.SessionMaxDays = DefaultSessionMaxDays
	}
	if a.OTP.Channels == nil {
		a.OTP.Channels = []string{}
	}
	if a.OIDC.AllowedSubjects == nil {
		a.OIDC.AllowedSubjects = []string{}
	}
	if a.CloudflareAccess.AllowedSubjects == nil {
		a.CloudflareAccess.AllowedSubjects = []string{}
	}
}

// Clone keeps settings snapshots independent of later edits.
func (a Auth) Clone() Auth {
	a.OTP.Channels = slices.Clone(a.OTP.Channels)
	a.OIDC.AllowedSubjects = slices.Clone(a.OIDC.AllowedSubjects)
	a.CloudflareAccess.AllowedSubjects = slices.Clone(a.CloudflareAccess.AllowedSubjects)
	return a
}

// PolicyID invalidates sessions and login transactions when authentication policy changes.
func (a Auth) PolicyID() string {
	data, _ := json.Marshal(a) // Auth contains only JSON-compatible concrete fields.
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Validate checks references before settings become visible to concurrent requests.
func (s Settings) Validate() error {
	return s.ValidateWithOptions(ValidationOptions{})
}

// ValidateWithOptions checks policy references under an explicit draft-validation policy.
func (s Settings) ValidateWithOptions(options ValidationOptions) error {
	a := s.Auth
	if a.SessionMaxDays < 1 || a.SessionMaxDays > MaxSessionMaxDays {
		return errors.New("session max days must be between 1 and 180")
	}
	for _, targets := range [][]string{s.Notifications.Channels, a.OTP.Channels} {
		seen := make(map[string]bool, len(targets))
		for _, name := range targets {
			switch name {
			case "telegram", "http", "email", "bark", "gotify", "sc3", "lark", "wecom":
			default:
				return fmt.Errorf("unsupported channel %q", name)
			}
			if name == "" || seen[name] {
				return fmt.Errorf("invalid or duplicate channel %q", name)
			}
			if _, ok := s.Channels[name]; !ok {
				return fmt.Errorf("channel %q is not configured", name)
			}
			seen[name] = true
		}
	}
	switch a.Mode {
	case "none":
	case "otp":
		if len(a.OTP.Channels) == 0 {
			return errors.New("configure OTP: a delivery channel is required")
		}
	case "oidc":
		if !secureURL(a.OIDC.Issuer) {
			return errors.New("configure OIDC: issuer must be an HTTPS URL without query or fragment")
		}
		if a.OIDC.ClientID == "" || a.OIDC.ClientSecret == "" || !validSubjectList(a.OIDC.AllowedSubjects, true) {
			return errors.New("configure OIDC: client credentials are required and allowed subjects cannot contain blanks")
		}
	case "cloudflare_access":
		u, err := url.Parse(a.CloudflareAccess.TeamDomain)
		if err != nil || !secureURL(a.CloudflareAccess.TeamDomain) || u.Path != "" || !strings.HasSuffix(u.Hostname(), ".cloudflareaccess.com") || u.Port() != "" {
			return errors.New("Access team domain must be https://<team>.cloudflareaccess.com")
		}
		if a.CloudflareAccess.Audience == "" || !validSubjectList(a.CloudflareAccess.AllowedSubjects, options.AllowEmptyAccessSubjects) {
			return errors.New("Access audience and allowed subjects are required")
		}
	default:
		return errors.New("unknown authentication mode")
	}
	return nil
}

func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func validSubjectList(subjects []string, allowEmpty bool) bool {
	if len(subjects) == 0 {
		return allowEmpty
	}
	return !slices.ContainsFunc(subjects, func(s string) bool { return strings.TrimSpace(s) == "" })
}
