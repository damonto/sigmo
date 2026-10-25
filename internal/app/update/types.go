package update

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/damonto/sigmo/internal/pkg/apierror"
)

const (
	ManifestSchemaVersion = 1

	ArtifactCompressionNone = "none"
	ArtifactCompressionGzip = "gzip"
)

type Manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Channel       string     `json:"channel"`
	Version       string     `json:"version"`
	Commit        string     `json:"commit"`
	PublishedAt   time.Time  `json:"publishedAt"`
	Notes         string     `json:"notes"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Artifact struct {
	Target           string `json:"target"`
	Name             string `json:"name"`
	Compression      string `json:"compression"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	ExecutableSize   int64  `json:"executableSize"`
	ExecutableSHA256 string `json:"executableSha256"`
	URL              string `json:"-"`
}

type Release struct {
	Manifest Manifest
	Artifact Artifact
	Verified bool
}

type Source interface {
	Latest(ctx context.Context, channel string, target string) (Release, error)
	Download(ctx context.Context, release Release) (io.ReadCloser, error)
}

type Licensee struct {
	Status       string     `json:"status"`
	DeviceID     string     `json:"deviceId"`
	TelegramID   int64      `json:"telegramId"`
	DisplayName  string     `json:"displayName"`
	Username     string     `json:"username,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	OfflineUntil *time.Time `json:"offlineUntil,omitempty"`
}

type LicenseProvider interface {
	Licensee() *Licensee
}

type codedError interface {
	Code() string
}

// ErrorCode returns the stable business code carried by an update failure.
func ErrorCode(err error) string {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

// snapshotError sanitizes upstream detail in successful polling responses,
// where the HTTP error boundary is not involved.
func snapshotError(err error) (string, string) {
	var remote *apierror.Error
	if errors.As(err, &remote) {
		code := remote.ErrorCode
		title, known := apierror.Title(code)
		if remote.ParseErr != nil || !known {
			code = "upstream_response_invalid"
			title, _ = apierror.Title(code) // This fallback is part of the catalog.
		}
		return code, title
	}
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		code := "upstream_unavailable"
		if requestErr.Timeout() {
			code = "upstream_timeout"
		}
		title, _ := apierror.Title(code) // Both transport errors belong to the catalog.
		return code, title
	}
	return ErrorCode(err), err.Error()
}
