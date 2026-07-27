package modemstatus

import (
	"context"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Fields struct {
	InternetConnected    bool `json:"internetConnected" jsonschema:"whether the modem currently has an active Sigmo-managed Internet connection"`
	WiFiCallingEnabled   bool `json:"wifiCallingEnabled" jsonschema:"whether Wi-Fi Calling is enabled in Sigmo settings"`
	WiFiCallingConnected bool `json:"wifiCallingConnected" jsonschema:"whether the modem currently has an active Wi-Fi Calling IMS connection"`
}

type Extension func(context.Context, *mmodem.Modem, *Fields) error
