package modemstatus

import (
	"context"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Fields contains connection state included in modem list and detail responses.
type Fields struct {
	InternetConnected    bool `json:"internetConnected" jsonschema:"whether the modem currently has an active Sigmo-managed Internet connection"`
	WiFiCallingEnabled   bool `json:"wifiCallingEnabled" jsonschema:"whether Wi-Fi Calling is enabled in Sigmo settings"`
	WiFiCallingConnected bool `json:"wifiCallingConnected" jsonschema:"whether the modem currently has an active Wi-Fi Calling IMS connection"`
	VoLTEEnabled         bool `json:"volteEnabled" jsonschema:"whether VoLTE is enabled in Sigmo settings"`
	VoLTEConnected       bool `json:"volteConnected" jsonschema:"whether the modem currently has an active VoLTE IMS connection"`
}

// Extension adds edition-specific connection state to a modem response.
type Extension func(context.Context, *mmodem.Modem, *Fields) error
