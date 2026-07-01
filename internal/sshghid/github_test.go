package sshghid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchKeysValidatesAndNormalizesKeys(t *testing.T) {
	valid := testPublicKeyLine(t)
	app := &App{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/alice.keys" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			return textResponse(r, http.StatusOK, "  "+valid+"  \n\n"), nil
		})},
	}
	keys, err := app.fetchKeys(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != valid {
		t.Fatalf("keys=%q want [%q]", keys, valid)
	}
}

func TestFetchKeysRejectsInvalidKeyLines(t *testing.T) {
	app := &App{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResponse(r, http.StatusOK, "ssh-ed25519 not-base64\n"), nil
		})},
	}
	_, err := app.fetchKeys(context.Background(), "alice")
	if err == nil || !strings.Contains(err.Error(), "parse public key") {
		t.Fatalf("expected invalid key error, got %v", err)
	}
}

func TestFetchKeysRejectsAuthorizedKeysOptions(t *testing.T) {
	valid := testPublicKeyLine(t)
	app := &App{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResponse(r, http.StatusOK, "command=\"id\" "+valid+"\n"), nil
		})},
	}
	_, err := app.fetchKeys(context.Background(), "alice")
	if err == nil || !strings.Contains(err.Error(), "expected a bare public key") {
		t.Fatalf("expected options rejection, got %v", err)
	}
}

func TestRefreshUserCacheDoesNotOverwriteOnInvalidFetch(t *testing.T) {
	valid := testPublicKeyLine(t)
	app := newAuthorizedKeysTestApp(t)
	app.BaseURL = "https://example.test"
	app.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return textResponse(r, http.StatusOK, "ssh-ed25519 not-base64\n"), nil
	})}
	app.Now = func() time.Time { return time.Unix(10, 0).UTC() }
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{valid}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.refreshUserCache(context.Background(), "alice"); err == nil {
		t.Fatal("expected refresh failure")
	}
	cache, err := app.loadCache("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Keys) != 1 || cache.Keys[0] != valid {
		t.Fatalf("cache keys changed: %q", cache.Keys)
	}
}

func textResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}
