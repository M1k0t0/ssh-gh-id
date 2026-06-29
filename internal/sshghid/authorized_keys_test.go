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

func TestRenderManagedBlockUsesCache(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "state", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{
		Home:               tmp,
		CacheDir:           cacheDir,
		AuthorizedKeysPath: filepath.Join(tmp, ".ssh", "authorized_keys"),
		Now:                func() time.Time { return time.Unix(0, 0).UTC() },
	}
	cache := UserCache{
		Username:  "alice",
		Keys:      []string{"ssh-ed25519 AAAA alice@test"},
		FetchedAt: time.Unix(0, 0).UTC(),
	}
	b, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "alice.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	block, count, err := app.renderManagedBlock([]string{"alice", "bob"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	if !strings.Contains(block, "alice@test") {
		t.Fatal("missing alice key")
	}
	if !strings.Contains(block, "# no cached keys yet") {
		t.Fatal("missing no-cache note for bob")
	}
}
