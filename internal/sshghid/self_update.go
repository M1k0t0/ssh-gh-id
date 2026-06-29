package sshghid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type selfUpdateResult struct {
	Updated         bool
	PreviousVersion string
	NewVersion      string
}

func currentReleasePlatform() releasePlatform {
	return releasePlatform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func (a *App) selfUpdate(ctx context.Context, platform releasePlatform) (selfUpdateResult, error) {
	release, err := a.fetchLatestRelease(ctx)
	if err != nil {
		return selfUpdateResult{}, err
	}
	if release.TagName == "" {
		return selfUpdateResult{}, errors.New("latest release has no tag name")
	}

	cmp, err := compareVersions(release.TagName, version)
	if err != nil {
		return selfUpdateResult{}, fmt.Errorf("compare release version %q with current version %q: %w", release.TagName, version, err)
	}
	if cmp <= 0 {
		fmt.Printf("%s %s %s %s\n", successText("already up to date"), keyText(version), dimText("latest:"), keyText(release.TagName))
		return selfUpdateResult{PreviousVersion: version, NewVersion: release.TagName}, nil
	}

	assetName, err := releaseAssetName(platform)
	if err != nil {
		return selfUpdateResult{}, err
	}
	asset, ok := release.findAsset(assetName)
	if !ok {
		return selfUpdateResult{}, fmt.Errorf("latest release %s does not include asset %q", release.TagName, assetName)
	}
	checksumAsset, ok := release.findAsset(assetName + ".sha256")
	if !ok {
		return selfUpdateResult{}, fmt.Errorf("latest release %s does not include checksum asset %q", release.TagName, assetName+".sha256")
	}

	fmt.Printf("%s %s -> %s\n", infoText("updating"), keyText(version), keyText(release.TagName))
	binary, err := a.downloadReleaseAsset(ctx, asset)
	if err != nil {
		return selfUpdateResult{}, err
	}
	checksum, err := a.downloadReleaseAsset(ctx, checksumAsset)
	if err != nil {
		return selfUpdateResult{}, err
	}
	if err := verifySHA256(binary, string(checksum)); err != nil {
		return selfUpdateResult{}, err
	}
	targetPath, err := a.selfUpdateTargetPath()
	if err != nil {
		return selfUpdateResult{}, err
	}
	if err := writeReaderAtomic(targetPath, bytes.NewReader(binary), 0o755); err != nil {
		return selfUpdateResult{}, fmt.Errorf("install update to %s: %w", targetPath, err)
	}
	_ = a.logf("self-updated from %s to %s", version, release.TagName)
	fmt.Printf("%s %s\n", successText("updated"), keyText(targetPath))
	return selfUpdateResult{Updated: true, PreviousVersion: version, NewVersion: release.TagName}, nil
}

func (a *App) selfUpdateTargetPath() (string, error) {
	path := a.ExecutablePath
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current executable: %w", err)
		}
		path = exe
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve absolute executable path %q: %w", path, err)
		}
		path = abs
	}
	return path, nil
}

func (a *App) fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	var release githubRelease
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.ReleaseAPIURL, nil)
	if err != nil {
		return release, fmt.Errorf("build latest release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", httpUserAgent)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return release, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return release, fmt.Errorf("fetch latest release: unexpected status %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return release, fmt.Errorf("parse latest release: %w", err)
	}
	return release, nil
}

func (a *App) downloadReleaseAsset(ctx context.Context, asset githubReleaseAsset) ([]byte, error) {
	if asset.BrowserDownloadURL == "" {
		return nil, fmt.Errorf("release asset %q has no download URL", asset.Name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request for %s: %w", asset.Name, err)
	}
	req.Header.Set("User-Agent", httpUserAgent)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("download %s: unexpected status %s %s", asset.Name, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (r githubRelease) findAsset(name string) (githubReleaseAsset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func releaseAssetName(platform releasePlatform) (string, error) {
	if platform.GOOS != "linux" {
		return "", fmt.Errorf("self-update is only available for released Linux binaries; current platform is %s/%s", platform.GOOS, platform.GOARCH)
	}
	switch platform.GOARCH {
	case "amd64", "arm64":
		return fmt.Sprintf("%s-linux-%s", appName, platform.GOARCH), nil
	default:
		return "", fmt.Errorf("self-update is not available for %s/%s", platform.GOOS, platform.GOARCH)
	}
}

func verifySHA256(content []byte, checksumText string) error {
	fields := strings.Fields(checksumText)
	if len(fields) == 0 {
		return errors.New("checksum file is empty")
	}
	want := strings.ToLower(fields[0])
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("invalid sha256 length in checksum file: %q", fields[0])
	}
	if _, err := hex.DecodeString(want); err != nil {
		return fmt.Errorf("invalid sha256 in checksum file: %w", err)
	}
	sum := sha256.Sum256(content)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, want)
	}
	return nil
}

func (a *App) runUpdatedBinaryMigrations(fromVersion, toVersion string) error {
	cmp, err := compareVersions(fromVersion, toVersion)
	if err != nil {
		return err
	}
	if cmp >= 0 {
		return nil
	}
	targetPath, err := a.selfUpdateTargetPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(targetPath, "--run-migrations-from", fromVersion, "--run-migrations-to", toVersion)
	cmd.Env = append(os.Environ(), "SSH_GH_ID_INTERNAL_MIGRATION=1", "SSH_GH_ID_PARENT_LOCK_HELD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run migrations with updated binary: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		fmt.Println(text)
	}
	return nil
}
