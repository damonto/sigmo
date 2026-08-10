package license

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	appupdate "github.com/damonto/sigmo/internal/app/update"
)

func (c *Controller) Latest(ctx context.Context, channel string, target string) (appupdate.Release, error) {
	if channel != "stable" && channel != "dev" {
		return appupdate.Release{}, errors.New("Pro update channel must be stable or dev")
	}
	path := "/v1/release-channels/" + channel + "/releases/latest?target=" + url.QueryEscape(target)
	metadataCtx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
	defer cancel()
	req, err := c.signedRequest(metadataCtx, http.MethodGet, path)
	if err != nil {
		return appupdate.Release{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return appupdate.Release{}, fmt.Errorf("get Pro release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return appupdate.Release{}, c.authenticatedResponseError(metadataCtx, resp, "get Pro release")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return appupdate.Release{}, fmt.Errorf("read Pro release: %w", err)
	}
	if len(data) > maxResponseSize {
		return appupdate.Release{}, errors.New("Pro release response exceeds size limit")
	}
	var remote releaseResponse
	if err := json.Unmarshal(data, &remote); err != nil {
		return appupdate.Release{}, fmt.Errorf("decode Pro release: %w", err)
	}
	manifest, verified, err := appupdate.ParseManifest([]byte(remote.Manifest), remote.Signature, c.releasePublicKey)
	if err != nil {
		return appupdate.Release{}, err
	}
	if manifest.Edition != "pro" || manifest.Channel != channel {
		return appupdate.Release{}, fmt.Errorf("%w: Pro release metadata mismatch", appupdate.ErrInvalidManifest)
	}
	if channel == "stable" {
		if notes, err := c.stableReleaseNotes(ctx, manifest.Version); err == nil {
			manifest.Notes = notes
		} else {
			slog.Debug("get stable release notes", "version", manifest.Version, "error", err)
		}
	}
	artifact, err := appupdate.FindArtifact(manifest, target)
	if err != nil {
		return appupdate.Release{}, err
	}
	artifact.URL = remote.DownloadURL
	return appupdate.Release{Manifest: manifest, Artifact: artifact, Verified: verified}, nil
}

func (c *Controller) stableReleaseNotes(ctx context.Context, version string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/damonto/sigmo/releases/tags/"+url.PathEscape(version), nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sigmo-pro-updater")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get GitHub release notes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get GitHub release notes: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return "", fmt.Errorf("read GitHub release notes: %w", err)
	}
	if len(data) > maxResponseSize {
		return "", errors.New("GitHub release response exceeds size limit")
	}
	var release struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("decode GitHub release notes: %w", err)
	}
	return release.Body, nil
}

func (c *Controller) Download(ctx context.Context, release appupdate.Release) (io.ReadCloser, error) {
	req, err := c.signedRequest(ctx, http.MethodGet, release.Artifact.URL)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download Pro update: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		defer resp.Body.Close()
		return nil, c.authenticatedResponseError(ctx, resp, "download Pro update")
	}
	return resp.Body, nil
}

func (c *Controller) authenticatedResponseError(ctx context.Context, resp *http.Response, action string) error {
	err := readServiceResponseError(resp, action)
	err = c.classifyAuthorizationError(err)
	if errors.Is(err, errExplicitUnauthorized) {
		c.clearLease(ctx)
		c.restartHealthy()
	}
	return err
}

func readServiceResponseError(resp *http.Response, action string) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("%s: read response: %w", action, err)
	}
	if len(data) > maxResponseSize {
		return fmt.Errorf("%s: response exceeds size limit", action)
	}
	remote := errorResponse{
		ErrorCode: "authorization_service_error",
		Message:   http.StatusText(resp.StatusCode),
	}
	if err := json.Unmarshal(data, &remote); err != nil {
		remote = errorResponse{
			ErrorCode: "authorization_service_error",
			Message:   http.StatusText(resp.StatusCode),
		}
	}
	if remote.ErrorCode == "" {
		remote.ErrorCode = "authorization_service_error"
	}
	if remote.Message == "" {
		remote.Message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s: %w", action, &serviceError{
		StatusCode: resp.StatusCode,
		ErrorCode:  remote.ErrorCode,
		Message:    remote.Message,
	})
}
