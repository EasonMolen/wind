// Package update checks a release manifest. Downloading and applying a release
// is deliberately a separate concern from checking.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("update manifest URL is not configured")

type Manifest struct {
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	SHA256      string `json:"sha256"`
	Notes       string `json:"notes"`

	// GitHub Releases API fields. Keeping these alongside the compact custom
	// manifest lets a repository use releases/latest without another hosted file.
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
}

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Result struct {
	CurrentVersion string
	LatestVersion  string
	Available      bool
	DownloadURL    string
	Notes          string
}

type Checker struct {
	CurrentVersion string
	ManifestURL    string
	Client         *http.Client
}

func (c Checker) Check(ctx context.Context) (Result, error) {
	current := normalizeVersion(c.CurrentVersion)
	if strings.TrimSpace(c.ManifestURL) == "" {
		return Result{CurrentVersion: current}, ErrNotConfigured
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ManifestURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create update request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NewWind-update-checker")
	response, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("fetch update manifest: unexpected HTTP status %s", response.Status)
	}

	var manifest Manifest
	if err = json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return Result{}, fmt.Errorf("decode update manifest: %w", err)
	}
	latest := normalizeVersion(manifest.Version)
	if latest == "" {
		latest = normalizeVersion(manifest.TagName)
	}
	if latest == "" {
		return Result{}, errors.New("update response does not contain a version or GitHub tag_name")
	}
	available, err := newerThan(latest, current)
	if err != nil {
		return Result{}, err
	}
	downloadURL := manifest.DownloadURL
	if downloadURL == "" {
		downloadURL = selectReleaseAsset(manifest.Assets)
	}
	notes := manifest.Notes
	if notes == "" {
		notes = manifest.Body
	}
	return Result{
		CurrentVersion: current,
		LatestVersion:  latest,
		Available:      available,
		DownloadURL:    downloadURL,
		Notes:          notes,
	}, nil
}

func selectReleaseAsset(assets []ReleaseAsset) string {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".zip") && strings.Contains(name, "windows-amd64") {
			return asset.BrowserDownloadURL
		}
	}
	for _, asset := range assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}
