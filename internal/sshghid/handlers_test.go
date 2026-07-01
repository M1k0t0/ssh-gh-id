package sshghid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleDeleteRemovesUserCacheAndManagedKey(t *testing.T) {
	app := newHandlerTestApp(t)
	key := testPublicKeyLine(t)
	if err := app.saveUsers([]string{"alice", "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{key}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "bob", Keys: []string{testPublicKeyLine(t)}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.applyAuthorizedKeys([]string{"alice", "bob"}, false); err != nil {
		t.Fatal(err)
	}
	if err := app.handleDelete("alice"); err != nil {
		t.Fatal(err)
	}
	users, err := app.loadUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != "bob" {
		t.Fatalf("users=%v want [bob]", users)
	}
	if _, err := os.Stat(app.cachePath("alice")); !os.IsNotExist(err) {
		t.Fatalf("alice cache still exists or stat failed: %v", err)
	}
	content, err := os.ReadFile(app.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "# user: alice") {
		t.Fatalf("authorized_keys still contains alice block:\n%s", content)
	}
	if !strings.Contains(string(content), "# user: bob") {
		t.Fatalf("authorized_keys missing bob block:\n%s", content)
	}
}

func TestHandleDeleteDuplicateManagedBlockLeavesStateUnchanged(t *testing.T) {
	app := newHandlerTestApp(t)
	aliceKey := testPublicKeyLine(t)
	if err := app.saveUsers([]string{"alice", "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{aliceKey}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "bob", Keys: []string{testPublicKeyLine(t)}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	originalUsers, err := os.ReadFile(app.UsersPath)
	if err != nil {
		t.Fatal(err)
	}
	originalCache, err := os.ReadFile(app.cachePath("alice"))
	if err != nil {
		t.Fatal(err)
	}
	originalAuthorizedKeys := strings.Join([]string{
		startMarker,
		"old one",
		endMarker,
		startMarker,
		"old two",
		endMarker,
		"",
	}, "\n")
	if err := os.WriteFile(app.AuthorizedKeysPath, []byte(originalAuthorizedKeys), 0o600); err != nil {
		t.Fatal(err)
	}
	err = app.handleDelete("alice")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected duplicate block error, got %v", err)
	}
	assertFileContent(t, app.UsersPath, string(originalUsers))
	assertFileContent(t, app.cachePath("alice"), string(originalCache))
	assertFileContent(t, app.AuthorizedKeysPath, originalAuthorizedKeys)
}

func TestHandleDeleteInvalidRemainingCacheLeavesStateUnchanged(t *testing.T) {
	app := newHandlerTestApp(t)
	if err := app.saveUsers([]string{"alice", "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{testPublicKeyLine(t)}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	writeRawCache(t, app, UserCache{Username: "bob", Keys: []string{"ssh-ed25519 not-base64"}})
	originalUsers, err := os.ReadFile(app.UsersPath)
	if err != nil {
		t.Fatal(err)
	}
	originalCache, err := os.ReadFile(app.cachePath("alice"))
	if err != nil {
		t.Fatal(err)
	}
	err = app.handleDelete("alice")
	if err == nil || !strings.Contains(err.Error(), "parse public key") {
		t.Fatalf("expected invalid cache error, got %v", err)
	}
	assertFileContent(t, app.UsersPath, string(originalUsers))
	assertFileContent(t, app.cachePath("alice"), string(originalCache))
}

func TestHandleDeleteRollsBackAuthorizedKeysAndUsersIfCacheRemovalFails(t *testing.T) {
	app := newHandlerTestApp(t)
	if err := app.saveUsers([]string{"alice", "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{testPublicKeyLine(t)}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "bob", Keys: []string{testPublicKeyLine(t)}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.applyAuthorizedKeys([]string{"alice", "bob"}, false); err != nil {
		t.Fatal(err)
	}
	originalAuthorizedKeys, err := os.ReadFile(app.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	originalUsers, err := os.ReadFile(app.UsersPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(app.cachePath("alice")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(app.cachePath("alice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.cachePath("alice"), "kept"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = app.handleDelete("alice")
	if err == nil || !strings.Contains(err.Error(), "remove cache") {
		t.Fatalf("expected cache removal failure, got %v", err)
	}
	assertFileContent(t, app.AuthorizedKeysPath, string(originalAuthorizedKeys))
	assertFileContent(t, app.UsersPath, string(originalUsers))
	if _, err := os.Stat(filepath.Join(app.cachePath("alice"), "kept")); err != nil {
		t.Fatalf("alice cache directory should remain after rollback: %v", err)
	}
}

func TestHandleSetSchedulerRejectsInvalidWithoutChangingConfig(t *testing.T) {
	app := newHandlerTestApp(t)
	if err := app.saveConfig(Config{AuthorizedKeysPath: app.AuthorizedKeysPath, Interval: defaultInterval, Scheduler: "crontab"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(app.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	err = app.handleSetScheduler("bad")
	if err == nil || !strings.Contains(err.Error(), "unsupported scheduler") {
		t.Fatalf("expected invalid scheduler error, got %v", err)
	}
	after, err := os.ReadFile(app.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed on invalid scheduler:\n%s", after)
	}
}

func newHandlerTestApp(t *testing.T) *App {
	t.Helper()
	app := newAuthorizedKeysTestApp(t)
	app.DataDir = filepath.Join(app.Home, "data")
	app.StateDir = filepath.Join(app.Home, "state")
	app.ConfigDir = filepath.Join(app.Home, "config")
	app.ConfigPath = filepath.Join(app.ConfigDir, configFilename)
	app.UsersPath = filepath.Join(app.DataDir, usersFilename)
	app.StatusPath = filepath.Join(app.StateDir, statusFilename)
	app.LockPath = filepath.Join(app.StateDir, lockFilename)
	app.SystemdDir = filepath.Join(app.Home, "systemd", "user")
	app.SystemdUnitPath = filepath.Join(app.SystemdDir, systemdUnitName)
	app.SystemdTimerPath = filepath.Join(app.SystemdDir, systemdTimerName)
	app.SystemSystemdDir = filepath.Join(app.Home, "etc", "systemd", "system")
	app.SystemSystemdUnitPath = filepath.Join(app.SystemSystemdDir, systemdUnitName)
	app.SystemSystemdTimerPath = filepath.Join(app.SystemSystemdDir, systemdTimerName)
	if err := os.MkdirAll(app.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(app.AuthorizedKeysPath), 0o700); err != nil {
		t.Fatal(err)
	}
	return app
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s content changed:\n%s\nwant:\n%s", path, got, want)
	}
}

func readUsersFile(t *testing.T, path string) UsersFile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var uf UsersFile
	if err := json.Unmarshal(b, &uf); err != nil {
		t.Fatal(err)
	}
	return uf
}
