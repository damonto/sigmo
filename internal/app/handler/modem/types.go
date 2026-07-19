package modem

import (
	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

type SlotResponse struct {
	Active             bool              `json:"active" jsonschema:"whether this SIM slot is currently selected"`
	OperatorName       string            `json:"operatorName" jsonschema:"display name of the SIM operator; empty when unknown"`
	OperatorIdentifier string            `json:"operatorIdentifier" jsonschema:"SIM operator identifier, typically the MCC and MNC; empty when unknown"`
	RegionCode         string            `json:"regionCode" jsonschema:"carrier region code; empty when unknown"`
	Identifier         string            `json:"identifier" jsonschema:"SIM ICCID used to identify and select this slot"`
	Reminder           *reminder.Details `json:"reminder" jsonschema:"reminder attached to this SIM; null when none is configured"`
}

type RegisteredOperatorResponse struct {
	Name string `json:"name" jsonschema:"display name of the network on which the modem is registered; empty when not registered"`
	Code string `json:"code" jsonschema:"registered network operator code, typically the MCC and MNC; empty when not registered"`
}

type UpdateMSISDNRequest struct {
	Number string `json:"number" validate:"required"`
}

type UnlockSIMRequest struct {
	PIN string `json:"pin"`
}

type UpdateModemSettingsRequest struct {
	Alias string `json:"alias"`
	MSS   int    `json:"mss" validate:"gte=64,lte=254"`
}

type ModemSettingsResponse struct {
	Alias string `json:"alias"`
	MSS   int    `json:"mss"`
}

type ModemResponse struct {
	modemstatus.Fields

	Manufacturer       string                     `json:"manufacturer" jsonschema:"modem manufacturer reported by the device"`
	ID                 string                     `json:"id" jsonschema:"modem IMEI; use this exact value as modemId in other tools"`
	FirmwareRevision   string                     `json:"firmwareRevision" jsonschema:"firmware revision reported by the modem"`
	HardwareRevision   string                     `json:"hardwareRevision" jsonschema:"hardware revision reported by the modem"`
	Name               string                     `json:"name" jsonschema:"configured modem alias, or the modem model when no alias is set"`
	Number             string                     `json:"number" jsonschema:"locally stored MSISDN for the active SIM; it may be empty or unverified"`
	State              string                     `json:"state" jsonschema:"current modem lifecycle state such as locked, registered, connected, or disabled"`
	UnlockRequired     string                     `json:"unlockRequired" jsonschema:"lock type currently preventing modem use; none when no unlock is required"`
	UnlockSupported    bool                       `json:"unlockSupported" jsonschema:"whether Sigmo can unlock the current SIM PIN lock"`
	SIM                SlotResponse               `json:"sim" jsonschema:"currently selected SIM; fields are empty when no SIM is available"`
	Slots              []SlotResponse             `json:"slots" jsonschema:"all SIM slots reported by the modem"`
	AccessTechnology   string                     `json:"accessTechnology" jsonschema:"highest-priority active radio access technology, such as lte or 5gnr; empty when unavailable"`
	RegistrationState  string                     `json:"registrationState" jsonschema:"current 3GPP network registration state; empty while unavailable or in airplane mode"`
	RegisteredOperator RegisteredOperatorResponse `json:"registeredOperator" jsonschema:"network operator on which the modem is currently registered"`
	SignalQuality      uint32                     `json:"signalQuality" jsonschema:"signal quality percentage from 0 to 100"`
	AirplaneMode       bool                       `json:"airplaneMode" jsonschema:"whether radio transmission is disabled for this modem"`
	SupportsEsim       bool                       `json:"supportsEsim" jsonschema:"whether Sigmo detected usable eSIM hardware on this modem"`
}
