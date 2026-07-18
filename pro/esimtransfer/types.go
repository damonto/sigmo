//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/pro/websheet"
)

type SourceType string
type ProfileType string

const (
	SourceModem SourceType = "modem"
	SourceCCID  SourceType = "ccid"

	ProfileESIM     ProfileType = "esim"
	ProfilePhysical ProfileType = "physical"
)

var (
	ErrSourceIMEIRequired = errors.New("source IMEI is required")
	ErrSourceUnsupported  = errors.New("transfer source is unsupported")
	ErrProfileUnsupported = errors.New("transfer profile is unsupported")
	ErrSourceIsTarget     = errors.New("transfer source cannot be target modem")
)

type Config struct {
	Store         *settings.Store
	Registry      *mmodem.Registry
	EnableProfile func(context.Context, *mmodem.Modem, string, sgp22.ICCID) error
	DeleteProfile func(context.Context, *mmodem.Modem, string, sgp22.ICCID) error
	Websheets     *websheet.Broker
}

type SourceResponse struct {
	Type               SourceType `json:"type"`
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Detail             string     `json:"detail,omitempty"`
	RequiresSourceIMEI bool       `json:"requiresSourceImei"`
}

type SourcesResponse struct {
	Sources   []SourceResponse `json:"sources"`
	CCIDError string           `json:"ccidError,omitempty"`
}

type ProfilesRequest struct {
	SourceType SourceType `json:"sourceType"`
	SourceID   string     `json:"sourceId"`
	SourceIMEI string     `json:"sourceImei,omitempty"`
}

type ProfileResponse struct {
	ID                  string               `json:"id"`
	SEID                string               `json:"seId,omitempty"`
	Type                ProfileType          `json:"type"`
	Name                string               `json:"name"`
	ServiceProviderName string               `json:"serviceProviderName,omitempty"`
	ICCID               string               `json:"iccid"`
	ISDPAID             string               `json:"isdPAID,omitempty"`
	Icon                string               `json:"icon,omitempty"`
	ProfileName         string               `json:"profileName,omitempty"`
	ProfileNickname     string               `json:"profileNickname,omitempty"`
	ProfileStateName    string               `json:"profileStateName,omitempty"`
	ProfileClass        string               `json:"profileClass,omitempty"`
	ProfileOwner        ProfileOwnerResponse `json:"profileOwner,omitempty"`
	RegionCode          string               `json:"regionCode,omitempty"`
	Enabled             bool                 `json:"enabled"`
	Supported           bool                 `json:"supported"`
	UnsupportedReason   string               `json:"unsupportedReason,omitempty"`
	CarrierName         string               `json:"carrierName,omitempty"`
}

type ProfileOwnerResponse struct {
	MCC  string `json:"mcc"`
	MNC  string `json:"mnc"`
	GID1 string `json:"gid1,omitempty"`
	GID2 string `json:"gid2,omitempty"`
}
