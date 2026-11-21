package update

import (
	"bytes"
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

func TestGitHubSourceDownloadsSignedReleases(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact, archive := testGzipArtifact(t, []byte("test executable"))
	artifact.Target = "linux-amd64"
	artifact.Name = "sigmo-linux-amd64.gz"

	tests := []struct {
		channel     string
		releasePath string
		tag         string
		version     string
	}{
		{channel: "stable", releasePath: "/releases/latest", tag: "v1.2.3", version: "v1.2.3"},
		{channel: "dev", releasePath: "/releases/tags/dev", tag: "dev", version: "dev-" + testCommit[:8]},
	}
	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			const origin = "https://updates.example"
			manifest := Manifest{
				SchemaVersion: ManifestSchemaVersion,
				Channel:       tt.channel,
				Version:       tt.version,
				Commit:        testCommit,
				PublishedAt:   time.Now().UTC(),
				Notes:         "signed notes",
				Artifacts:     []Artifact{artifact},
			}
			manifestData, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestData))
			releaseData, err := json.Marshal(githubRelease{
				TagName: tt.tag,
				Body:    "GitHub release notes",
				Assets: []githubAsset{
					{Name: manifestAssetName, BrowserDownloadURL: origin + "/manifest"},
					{Name: signatureAssetName, BrowserDownloadURL: origin + "/signature"},
					{Name: artifact.Name, BrowserDownloadURL: origin + "/binary"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				for header := range r.Header {
					if strings.EqualFold(header, "Authorization") || strings.HasPrefix(strings.ToLower(header), "x-sigmo-") {
						t.Errorf("request to %s includes authorization header %q", r.URL.Path, header)
					}
				}
				var data []byte
				switch r.URL.Path {
				case tt.releasePath:
					data = releaseData
				case "/manifest":
					data = manifestData
				case "/signature":
					data = []byte(signature)
				case "/binary":
					data = archive
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
				}, nil
			})}

			source := NewGitHubSource(client, origin+"/releases/latest", base64.RawStdEncoding.EncodeToString(publicKey))
			release, err := source.Latest(t.Context(), tt.channel, artifact.Target)
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if !release.Verified || release.Manifest.Version != tt.version || release.Manifest.Notes != "GitHub release notes" {
				t.Fatalf("Latest() = %+v", release)
			}
			wantArtifact := artifact
			wantArtifact.URL = origin + "/binary"
			if release.Artifact != wantArtifact {
				t.Fatalf("artifact = %+v, want %+v", release.Artifact, wantArtifact)
			}

			body, err := source.Download(t.Context(), release)
			if err != nil {
				t.Fatalf("Download() error = %v", err)
			}
			defer body.Close()
			got, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, archive) {
				t.Fatal("downloaded artifact differs from the published archive")
			}
		})
	}
}
