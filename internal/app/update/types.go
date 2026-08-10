package update

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	ManifestSchemaVersion = 1

	ArtifactCompressionNone = "none"
	ArtifactCompressionGzip = "gzip"
)

type Manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Edition       string     `json:"edition"`
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

func ErrorCode(err error) string {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}
