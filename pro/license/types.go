package license

import (
	"encoding/json"
	"time"
)

const leaseSchemaVersion = 1

type Lease struct {
	SchemaVersion        int        `json:"schemaVersion"`
	DeviceID             string     `json:"deviceId"`
	SessionID            string     `json:"sessionId"`
	Generation           int64      `json:"generation"`
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
	PollToken           string
	ActivationURL       string
	InitialRefreshToken string
	ExpiresAt           time.Time
}

type storedSession struct {
	SessionID    string           `json:"sessionId"`
	Generation   int64            `json:"generation"`
	RefreshToken string           `json:"refreshToken"`
	Pending      *pendingRotation `json:"pending,omitempty"`
}

type pendingRotation struct {
	ID               string `json:"id"`
	NextRefreshToken string `json:"nextRefreshToken"`
}

type challenge struct {
	Challenge string    `json:"challenge"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type leaseChallengeRequest struct {
	DeviceID   string `json:"deviceId"`
	SessionID  string `json:"sessionId"`
	Generation int64  `json:"generation"`
}

type leaseRotationRequest struct {
	DeviceID             string `json:"deviceId"`
	SessionID            string `json:"sessionId"`
	Generation           int64  `json:"generation"`
	Challenge            string `json:"challenge"`
	RefreshToken         string `json:"refreshToken"`
	NextRefreshTokenHash string `json:"nextRefreshTokenHash"`
	RotationID           string `json:"rotationId"`
	FingerprintHash      string `json:"fingerprintHash"`
	Signature            string `json:"signature"`
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
