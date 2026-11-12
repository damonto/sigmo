package esim

import "github.com/damonto/sigmo/internal/pkg/reminder"

// Inventory groups installed profiles by secure element.
type Inventory struct {
	SEs []SecureElement `json:"ses" jsonschema:"secure elements and their installed eSIM profiles"`
}

// SecureElement describes one eUICC application and its installed profiles.
type SecureElement struct {
	ID       string    `json:"id" jsonschema:"Sigmo secure-element identifier; use this exact value as seId in eSIM tools"`
	Label    string    `json:"label" jsonschema:"human-readable secure-element label"`
	AID      string    `json:"aid" jsonschema:"hexadecimal application identifier used to address the eUICC application"`
	EID      string    `json:"eid" jsonschema:"eUICC identifier; empty when it cannot be read"`
	Profiles []Profile `json:"profiles" jsonschema:"profiles installed on this secure element"`
}

// Profile combines installed eSIM metadata with the last known number for its ICCID.
type Profile struct {
	Number              string            `json:"number" jsonschema:"last known phone number for this ICCID; empty when unknown"`
	SEID                string            `json:"seId" jsonschema:"secure-element identifier containing this profile"`
	SELabel             string            `json:"seLabel" jsonschema:"human-readable label of the containing secure element"`
	EID                 string            `json:"seEid" jsonschema:"EID of the containing eUICC; empty when unavailable"`
	Name                string            `json:"name" jsonschema:"best available display name for the profile"`
	ServiceProviderName string            `json:"serviceProviderName" jsonschema:"service provider name stored in the profile"`
	ICCID               string            `json:"iccid" jsonschema:"profile ICCID; use this exact value in profile management tools"`
	ISDPAID             string            `json:"isdPAID" jsonschema:"hexadecimal application identifier of the profile ISD-P"`
	Icon                string            `json:"icon" jsonschema:"profile icon encoded as a data URL; empty when no icon is available"`
	ProfileName         string            `json:"profileName" jsonschema:"profile name supplied by the service provider"`
	ProfileNickname     string            `json:"profileNickname" jsonschema:"user-assigned profile nickname; empty when none is set"`
	ProfileState        uint8             `json:"profileState" jsonschema:"numeric profile state reported by the eUICC"`
	ProfileStateName    string            `json:"profileStateName" jsonschema:"human-readable profile state, such as enabled or disabled"`
	ProfileClass        string            `json:"profileClass" jsonschema:"profile class reported by the eUICC, such as operational, test, or provisioning"`
	ProfileOwner        ProfileOwner      `json:"profileOwner" jsonschema:"mobile network identity encoded in the profile policy rules"`
	RegionCode          string            `json:"regionCode" jsonschema:"carrier region code inferred for the profile; empty when unknown"`
	Reminder            *reminder.Details `json:"reminder" jsonschema:"reminder attached to this profile; null when none is configured"`
}

// ProfileOwner identifies the mobile network encoded by a profile.
type ProfileOwner struct {
	MCC  string `json:"mcc" jsonschema:"mobile country code; empty when not encoded by the profile"`
	MNC  string `json:"mnc" jsonschema:"mobile network code; empty when not encoded by the profile"`
	GID1 string `json:"gid1" jsonschema:"optional level-1 group identifier encoded by the profile; empty when absent"`
	GID2 string `json:"gid2" jsonschema:"optional level-2 group identifier encoded by the profile; empty when absent"`
}

// Discovery identifies a profile advertised by the discovery service.
type Discovery struct {
	EventID string `json:"eventId" jsonschema:"discovery event identifier supplied by the eSIM discovery service"`
	Address string `json:"address" jsonschema:"SM-DP+ address associated with the discovered profile"`
}
