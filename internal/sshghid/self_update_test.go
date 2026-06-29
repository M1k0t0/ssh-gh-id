package sshghid

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseAssetName(t *testing.T) {
	got, err := releaseAssetName(releasePlatform{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh-gh-id-linux-amd64" {
		t.Fatalf("asset=%q", got)
	}
	if _, err := releaseAssetName(releasePlatform{GOOS: "darwin", GOARCH: "arm64"}); err == nil {
		t.Fatal("expected unsupported platform error")
	}
}

func TestVerifySHA256RejectsMismatch(t *testing.T) {
	sum := sha256.Sum256([]byte("expected"))
	if err := verifySHA256([]byte("actual"), fmt.Sprintf("%x  asset\n", sum)); err == nil {
		t.Fatal("expected sha256 mismatch")
	}
}

func TestSelfUpdateDownloadsAndInstallsLatestRelease(t *testing.T) {
	binary := []byte("new binary")
	sum := sha256.Sum256(binary)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) *http.Response {
			return &http.Response{
				StatusCode: status,
				Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}
		}
		switch r.URL.Path {
		case "/latest":
			return response(http.StatusOK, `{
				"tag_name": "v0.3.2",
				"html_url": "https://example.test/releases/tag/v0.3.2",
				"assets": [
					{"name": "ssh-gh-id-linux-amd64", "browser_download_url": "https://example.test/asset"},
					{"name": "ssh-gh-id-linux-amd64.sha256", "browser_download_url": "https://example.test/asset.sha256"}
				]
			}`), nil
		case "/asset":
			return response(http.StatusOK, string(binary)), nil
		case "/asset.sha256":
			return response(http.StatusOK, fmt.Sprintf("%x  ssh-gh-id-linux-amd64\n", sum)), nil
		}
		return response(http.StatusNotFound, "not found"), nil
	})}

	tmp := t.TempDir()
	target := filepath.Join(tmp, "ssh-gh-id")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{
		Home:          tmp,
		StateDir:      filepath.Join(tmp, "state"),
		LogPath:       filepath.Join(tmp, "state", "logs", "ssh-gh-id.log"),
		LocalBinPath:  target,
		ReleaseAPIURL: "https://example.test/latest",
		HTTPClient:    client,
		Now:           func() time.Time { return time.Unix(0, 0).UTC() },
	}

	result, err := app.selfUpdate(context.Background(), releasePlatform{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatal("expected update")
	}
	if result.NewVersion != "v0.3.2" {
		t.Fatalf("result=%+v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("installed binary=%q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %v", info.Mode().Perm())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
