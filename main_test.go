package main

import (
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

func TestNormalizeUsername(t *testing.T) {
	got, err := normalizeUsername(" Foo-Bar ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo-bar" {
		t.Fatalf("got %q", got)
	}
	if _, err := normalizeUsername("bad_name"); err == nil {
		t.Fatal("expected invalid username error")
	}
}

func TestParseIntervalSpec(t *testing.T) {
	cases := []struct {
		in       string
		kind     intervalKind
		cronSpec string
	}{
		{in: "daily", kind: intervalSystemdCalendar, cronSpec: "@daily"},
		{in: "12h", kind: intervalSystemdDuration, cronSpec: "0 */12 * * *"},
		{in: "0 */6 * * *", kind: intervalCron, cronSpec: "0 */6 * * *"},
	}
	for _, tc := range cases {
		got, err := parseIntervalSpec(tc.in)
		if err != nil {
			t.Fatalf("parseIntervalSpec(%q): %v", tc.in, err)
		}
		if got.Kind != tc.kind {
			t.Fatalf("parseIntervalSpec(%q).Kind=%q want %q", tc.in, got.Kind, tc.kind)
		}
		if got.CronSpec != tc.cronSpec {
			t.Fatalf("parseIntervalSpec(%q).CronSpec=%q want %q", tc.in, got.CronSpec, tc.cronSpec)
		}
	}
}

func TestRenderManagedBlockUsesCache(t *testing.T) {
	tmp := t.TempDir()
	app := &App{
		Home:               tmp,
		ConfigDir:          filepath.Join(tmp, "config"),
		DataDir:            filepath.Join(tmp, "data"),
		StateDir:           filepath.Join(tmp, "state"),
		CacheDir:           filepath.Join(tmp, "state", cacheDirname),
		ConfigPath:         filepath.Join(tmp, "config", configFilename),
		UsersPath:          filepath.Join(tmp, "data", usersFilename),
		StatusPath:         filepath.Join(tmp, "state", statusFilename),
		LogPath:            filepath.Join(tmp, "state", logsDirname, logFilename),
		LockPath:           filepath.Join(tmp, "state", lockFilename),
		AuthorizedKeysPath: filepath.Join(tmp, ".ssh", "authorized_keys"),
		Now:                func() time.Time { return time.Unix(0, 0).UTC() },
	}
	if err := app.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := app.saveCache(UserCache{Username: "alice", Keys: []string{"ssh-ed25519 AAAA alice@test"}, FetchedAt: time.Unix(0, 0).UTC()}); err != nil {
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

func TestReplaceManagedCronHandlesEmptyBlockRemoval(t *testing.T) {
	current := strings.Join([]string{
		"MAILTO=user@example.com",
		cronStartMarker,
		"@daily /tmp/ssh-gh-id --update-all",
		cronEndMarker,
		"# trailing",
	}, "\n")
	out, err := replaceManagedCron(current, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, cronStartMarker) || strings.Contains(out, cronEndMarker) {
		t.Fatal("managed cron block still present")
	}
	if !strings.Contains(out, "MAILTO=user@example.com") || !strings.Contains(out, "# trailing") {
		t.Fatal("surrounding crontab content lost")
	}
}
