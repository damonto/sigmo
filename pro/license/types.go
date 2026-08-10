package license

import (
	"encoding/json"
	"time"
)

const leaseSchemaVersion = 1

type Lease struct {
	SchemaVersion        int        `json:"schemaVersion"`
	DeviceID             string     `json:"deviceId"`
	TelegramID           int64      `json:"telegramId"`
	Status               string     `json:"status"`
	DisplayName          string     `json:"displayName"`
	Username             string     `json:"username,omitempty"`
	IssuedAt             time.Time  `json:"issuedAt"`
	RefreshAfter         time.Time  `json:"refreshAfter"`
	ExpiresAt            time.Time  `json:"expiresAt"`
	EntitlementExpiresAt *time.Time `json:"entitlementExpiresAt,omitempty"`
}

type signedLease struct {
	Lease     json.RawMessage `json:"lease"`
	Signature string          `json:"signature"`
}

type pairing struct {
	ID            string       `json:"id"`
	PollToken     string       `json:"pollToken,omitempty"`
	ActivationURL string       `json:"activationUrl"`
	Status        string       `json:"status"`
	ExpiresAt     time.Time    `json:"expiresAt"`
	Lease         *signedLease `json:"lease,omitempty"`
}

type pairingSession struct {
	PollToken     string
	ActivationURL string
	ExpiresAt     time.Time
}

type challenge struct {
	Challenge string    `json:"challenge"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type releaseResponse struct {
	Manifest    string `json:"manifest"`
	Signature   string `json:"signature"`
	DownloadURL string `json:"downloadUrl"`
}

type errorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

type serviceError struct {
	StatusCode int
	ErrorCode  string
	Message    string
}

func (e *serviceError) Error() string {
	return e.Message
}

func (e *serviceError) Code() string {
	return e.ErrorCode
}
