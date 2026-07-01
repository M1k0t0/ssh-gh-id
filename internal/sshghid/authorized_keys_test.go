package sshghid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplaceManagedBlockAppendsWhenMissing(t *testing.T) {
	body := "ssh-ed25519 AAAA unmanaged@host\n"
	out, err := replaceManagedBlock(body, startMarker+"\nkey\n"+endMarker+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unmanaged@host") {
		t.Fatal("unmanaged content missing")
	}
	if !strings.Contains(out, startMarker) || !strings.Contains(out, endMarker) {
		t.Fatal("managed block missing")
	}
}

func TestReplaceManagedBlockReplacesExisting(t *testing.T) {
	body := strings.Join([]string{
		"before",
		startMarker,
		"old",
		endMarker,
		"after",
	}, "\n")
	out, err := replaceManagedBlock(body, startMarker+"\nnew\n"+endMarker+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "old") {
		t.Fatal("old block content still present")
	}
	if !strings.Contains(out, "new") {
		t.Fatal("new block content missing")
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatal("surrounding content lost")
	}
}

func TestReplaceManagedBlockRejectsDuplicateManagedBlocks(t *testing.T) {
	body := strings.Join([]string{
		startMarker,
		"old one",
		endMarker,
		startMarker,
		"old two",
		endMarker,
	}, "\n")
	_, err := replaceManagedBlock(body, startMarker+"\nnew\n"+endMarker+"\n")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected duplicate managed block error, got %v", err)
	}
}

func TestReplaceManagedBlockRejectsIncompleteManagedBlock(t *testing.T) {
	_, err := replaceManagedBlock(startMarker+"\nold\n", startMarker+"\nnew\n"+endMarker+"\n")
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete managed block error, got %v", err)
	}
}

func TestRenderManagedBlockUsesCache(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	cache := UserCache{
		Username:  "alice",
		Keys:      []string{testPublicKeyLine(t)},
		FetchedAt: time.Unix(0, 0).UTC(),
	}
	writeRawCache(t, app, cache)
	block, count, err := app.renderManagedBlock([]string{"alice", "bob"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	if !strings.Contains(block, "ssh-ed25519 ") {
		t.Fatal("missing alice key")
	}
	if !strings.Contains(block, "# no cached keys yet") {
		t.Fatal("missing no-cache note for bob")
	}
}

func TestRenderManagedBlockRejectsInvalidCachedKey(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	writeRawCache(t, app, UserCache{Username: "alice", Keys: []string{"ssh-ed25519 not-base64"}})
	_, _, err := app.renderManagedBlock([]string{"alice"}, false)
	if err == nil || !strings.Contains(err.Error(), "parse public key") {
		t.Fatalf("expected invalid cache error, got %v", err)
	}
}

func TestApplyAuthorizedKeysRejectsDuplicateBlocksWithoutWriting(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	writeRawCache(t, app, UserCache{Username: "alice", Keys: []string{testPublicKeyLine(t)}})
	original := strings.Join([]string{
		startMarker,
		"old one",
		endMarker,
		startMarker,
		"old two",
		endMarker,
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(app.AuthorizedKeysPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.AuthorizedKeysPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	err := app.applyAuthorizedKeys([]string{"alice"}, false)
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected duplicate block error, got %v", err)
	}
	got, err := os.ReadFile(app.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("authorized_keys changed on failure:\n%s", got)
	}
}

func TestCountInstalledKeysRejectsDuplicateBlocks(t *testing.T) {
	app := newAuthorizedKeysTestApp(t)
	content := strings.Join([]string{
		startMarker,
		"ssh-ed25519 AAAA",
		endMarker,
		startMarker,
		"ssh-ed25519 BBBB",
		endMarker,
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(app.AuthorizedKeysPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.AuthorizedKeysPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := app.countInstalledKeys()
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected duplicate block error, got %v", err)
	}
}

func newAuthorizedKeysTestApp(t *testing.T) *App {
	t.Helper()
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "state", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &App{
		Home:               tmp,
		ConfigPath:         filepath.Join(tmp, "config", "config.json"),
		CacheDir:           cacheDir,
		AuthorizedKeysPath: filepath.Join(tmp, ".ssh", "authorized_keys"),
		LogPath:            filepath.Join(tmp, "state", "logs", "ssh-gh-id.log"),
		Now:                func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func writeRawCache(t *testing.T, app *App, cache UserCache) {
	t.Helper()
	if err := os.MkdirAll(app.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.CacheDir, cache.Username+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}
