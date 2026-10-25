package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGitHubSourceLatest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: 1, Channel: "stable", Version: "v1.2.3",
		Commit: testCommit, PublishedAt: time.Now().UTC(), Notes: "signed notes",
		Artifacts: []Artifact{{
			Target: "linux-amd64", Name: "sigmo-linux-amd64", Compression: ArtifactCompressionNone,
			Size: 6, SHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
			ExecutableSize: 6, ExecutableSHA256: "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd",
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestData))
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := ""
		switch r.URL.Path {
		case "/latest":
			data, marshalErr := json.Marshal(map[string]any{
				"tag_name": "v1.2.3", "body": "GitHub release notes",
				"assets": []map[string]string{
					{"name": manifestAssetName, "browser_download_url": "https://updates.example/manifest"},
					{"name": signatureAssetName, "browser_download_url": "https://updates.example/signature"},
					{"name": "sigmo-linux-amd64", "browser_download_url": "https://updates.example/binary"},
				},
			})
			if marshalErr != nil {
				return nil, marshalErr
			}
			body = string(data)
		case "/manifest":
			body = string(manifestData)
		case "/signature":
			body = signature
		case "/binary":
			body = "binary"
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	source := NewGitHubSource(client, "https://updates.example/latest", base64.RawStdEncoding.EncodeToString(publicKey))
	release, err := source.Latest(t.Context(), "stable", "linux-amd64")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !release.Verified || release.Manifest.Notes != "GitHub release notes" || release.Artifact.URL != "https://updates.example/binary" {
		t.Fatalf("Latest() = %+v", release)
	}
	body, err := source.Download(t.Context(), release)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	defer body.Close()
	if release.Artifact.Size != 6 {
		t.Fatalf("artifact size = %d", release.Artifact.Size)
	}
}

func TestGitHubSourceDevelopmentChannel(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: 1, Channel: "dev", Version: "dev-" + testCommit[:8], Commit: testCommit,
		PublishedAt: time.Now().UTC(), Artifacts: []Artifact{{
			Target: "linux-amd64", Name: "sigmo-linux-amd64.gz", Compression: ArtifactCompressionGzip,
			Size: 3, SHA256: strings.Repeat("0", 64), ExecutableSize: 4, ExecutableSHA256: strings.Repeat("1", 64),
		}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, data))
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/releases/tags/dev":
			release, err := json.Marshal(githubRelease{TagName: "dev", Assets: []githubAsset{
				{Name: manifestAssetName, BrowserDownloadURL: "https://updates.example/manifest"},
				{Name: signatureAssetName, BrowserDownloadURL: "https://updates.example/signature"},
				{Name: "sigmo-linux-amd64.gz", BrowserDownloadURL: "https://updates.example/binary"},
			}})
			if err != nil {
				return nil, err
			}
			body = string(release)
		case "/manifest":
			body = string(data)
		case "/signature":
			body = signature
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	source := NewGitHubSource(client, "https://updates.example/releases/latest", base64.RawStdEncoding.EncodeToString(publicKey))
	release, err := source.Latest(t.Context(), "dev", "linux-amd64")
	if err != nil || !release.Verified || release.Manifest.Version != manifest.Version {
		t.Fatalf("development release = %+v, %v", release, err)
	}
}
