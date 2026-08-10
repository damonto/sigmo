package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultGitHubLatestURL = "https://api.github.com/repos/damonto/sigmo/releases/latest"
	manifestAssetName      = "sigmo-update.json"
	signatureAssetName     = "sigmo-update.json.sig"
)

type GitHubSource struct {
	client    *http.Client
	latestURL string
	publicKey string
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewGitHubSource(client *http.Client, latestURL string, publicKey string) *GitHubSource {
	if client == nil {
		client = &http.Client{}
	}
	if strings.TrimSpace(latestURL) == "" {
		latestURL = defaultGitHubLatestURL
	}
	return &GitHubSource{client: client, latestURL: latestURL, publicKey: publicKey}
}

func (s *GitHubSource) Latest(ctx context.Context, channel string, target string) (Release, error) {
	if channel != "stable" {
		return Release{}, errors.New("community update channel must be stable")
	}
	var release githubRelease
	if err := s.getJSON(ctx, s.latestURL, &release); err != nil {
		return Release{}, fmt.Errorf("get latest GitHub release: %w", err)
	}
	manifestURL, ok := githubAssetURL(release.Assets, manifestAssetName)
	if !ok {
		return Release{}, fmt.Errorf("latest GitHub release has no %s", manifestAssetName)
	}
	signatureURL, ok := githubAssetURL(release.Assets, signatureAssetName)
	if !ok {
		return Release{}, fmt.Errorf("latest GitHub release has no %s", signatureAssetName)
	}
	manifestData, err := s.getBytes(ctx, manifestURL, 1<<20)
	if err != nil {
		return Release{}, fmt.Errorf("get GitHub update manifest: %w", err)
	}
	signature, err := s.getBytes(ctx, signatureURL, 4096)
	if err != nil {
		return Release{}, fmt.Errorf("get GitHub update signature: %w", err)
	}
	manifest, verified, err := ParseManifest(manifestData, string(signature), s.publicKey)
	if err != nil {
		return Release{}, err
	}
	if manifest.Edition != "community" || manifest.Channel != channel || manifest.Version != release.TagName {
		return Release{}, fmt.Errorf("%w: GitHub release metadata does not match manifest", ErrInvalidManifest)
	}
	artifact, err := FindArtifact(manifest, target)
	if err != nil {
		return Release{}, err
	}
	artifact.URL, ok = githubAssetURL(release.Assets, artifact.Name)
	if !ok {
		return Release{}, fmt.Errorf("GitHub release has no artifact %q", artifact.Name)
	}
	manifest.Notes = release.Body
	return Release{Manifest: manifest, Artifact: artifact, Verified: verified}, nil
}

func (s *GitHubSource) Download(ctx context.Context, release Release) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.Artifact.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create GitHub artifact request: %w", err)
	}
	req.Header.Set("User-Agent", "sigmo-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download GitHub artifact: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() // The HTTP status is the primary download error.
		return nil, fmt.Errorf("download GitHub artifact: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *GitHubSource) getJSON(ctx context.Context, url string, dst any) error {
	data, err := s.getBytes(ctx, url, 4<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func (s *GitHubSource) getBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sigmo-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return data, nil
}

func githubAssetURL(assets []githubAsset, name string) (string, bool) {
	for _, asset := range assets {
		if asset.Name == name && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, true
		}
	}
	return "", false
}
