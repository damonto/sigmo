// Package carrierconfig validates and packages application-owned carrier data.
package carrierconfig

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	imsgo "github.com/damonto/ims-go"
	ts43 "github.com/damonto/ts43-go"
)

const (
	// MaxCompressedSize bounds encrypted downloads before decompression.
	MaxCompressedSize = 4 << 20
	// MaxExpandedSize bounds the JSON catalog after decompression.
	MaxExpandedSize = 32 << 20
	// SignatureContext separates configuration signatures from license leases.
	SignatureContext = "sigmo-carrier-config-v1\n"
)

// Files is the versioned format published to R2. Raw data never reaches clients' UI.
type Files struct {
	SchemaVersion int             `json:"schemaVersion"`
	Entitlement   json.RawMessage `json:"entitlement"`
	WFC           json.RawMessage `json:"wfc"`
	IMS           json.RawMessage `json:"ims"`
}

// Config contains immutable library catalogs that can be shared across clients.
type Config struct {
	Entitlements *ts43.EntitlementConfigs
	WFC          *ts43.WFCConfigs
	IMS          *imsgo.CarrierProfiles
}

// Parse validates all catalogs before any of them can be activated.
func Parse(files Files) (*Config, error) {
	if files.SchemaVersion != 1 {
		return nil, errors.New("unsupported carrier configuration schema")
	}
	var config Config
	config.Entitlements = new(ts43.EntitlementConfigs)
	config.WFC = new(ts43.WFCConfigs)
	config.IMS = new(imsgo.CarrierProfiles)
	if err := json.Unmarshal(files.Entitlement, config.Entitlements); err != nil {
		return nil, fmt.Errorf("decode entitlement configs: %w", err)
	}
	if err := json.Unmarshal(files.WFC, config.WFC); err != nil {
		return nil, fmt.Errorf("decode WFC configs: %w", err)
	}
	if err := json.Unmarshal(files.IMS, config.IMS); err != nil {
		return nil, fmt.Errorf("decode IMS profiles: %w", err)
	}
	return &config, nil
}

// Pack validates and compresses the three files into one immutable revision.
func Pack(files Files) ([]byte, string, error) {
	if _, err := Parse(files); err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(files)
	if err != nil {
		return nil, "", fmt.Errorf("encode carrier files: %w", err)
	}
	if len(data) > MaxExpandedSize {
		return nil, "", errors.New("carrier files exceed size limit")
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return nil, "", fmt.Errorf("compress carrier files: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close compressed carrier files: %w", err)
	}
	if compressed.Len() > MaxCompressedSize {
		return nil, "", errors.New("compressed carrier files exceed size limit")
	}
	return compressed.Bytes(), Revision(compressed.Bytes()), nil
}

// Revision identifies the exact compressed bytes, independently of encryption.
func Revision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Unpack checks the content hash and bounds expansion before parsing the files.
func Unpack(data []byte, revision string) (*Config, error) {
	if len(data) > MaxCompressedSize || Revision(data) != revision {
		return nil, errors.New("invalid carrier configuration revision")
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open compressed carrier files: %w", err)
	}
	defer reader.Close()
	expanded, err := io.ReadAll(io.LimitReader(reader, MaxExpandedSize+1))
	if err != nil {
		return nil, fmt.Errorf("decompress carrier files: %w", err)
	}
	if len(expanded) > MaxExpandedSize {
		return nil, errors.New("expanded carrier files exceed size limit")
	}
	var files Files
	if err := json.Unmarshal(expanded, &files); err != nil {
		return nil, fmt.Errorf("decode carrier files: %w", err)
	}
	return Parse(files)
}
