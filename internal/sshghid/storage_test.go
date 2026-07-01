package sshghid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveCacheNormalizesValidKeys(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	valid := testPublicKeyLine(t)
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{"  " + valid + "  "}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	cache, err := app.loadCache("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Keys) != 1 || cache.Keys[0] != valid {
		t.Fatalf("cache keys=%q want [%q]", cache.Keys, valid)
	}
}

func TestSaveCacheRejectsInvalidKeys(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	err := app.saveCache(UserCache{Username: "alice", Keys: []string{"ssh-ed25519 not-base64"}})
	if err == nil || !strings.Contains(err.Error(), "parse public key") {
		t.Fatalf("expected invalid key error, got %v", err)
	}
	if _, err := os.Stat(app.cachePath("alice")); !os.IsNotExist(err) {
		t.Fatalf("cache file exists after failed save: %v", err)
	}
}

func TestLoadCacheRejectsInvalidKeys(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	writeRawCache(t, app, UserCache{Username: "alice", Keys: []string{"ssh-ed25519 not-base64"}})
	_, err := app.loadCache("alice")
	if err == nil || !strings.Contains(err.Error(), "parse public key") {
		t.Fatalf("expected invalid key error, got %v", err)
	}
}

func TestLoadCacheRejectsMismatchedUsername(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	if err := os.MkdirAll(app.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(UserCache{Username: "mallory", Keys: []string{testPublicKeyLine(t)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.CacheDir, "alice.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = app.loadCache("alice")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected username mismatch error, got %v", err)
	}
}
