package sshghid

import (
	"strings"
	"testing"
	"time"
)

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

func TestReplaceManagedCronAppendsAndReplacesBlock(t *testing.T) {
	block := cronStartMarker + "\n@daily /bin/one\n" + cronEndMarker + "\n"
	out, err := replaceManagedCron("MAILTO=user@example.com\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MAILTO=user@example.com") || !strings.Contains(out, "@daily /bin/one") {
		t.Fatalf("append lost content: %q", out)
	}
	replacement := cronStartMarker + "\n@hourly /bin/two\n" + cronEndMarker + "\n"
	out, err = replaceManagedCron(out, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "/bin/one") || !strings.Contains(out, "/bin/two") {
		t.Fatalf("replace failed: %q", out)
	}
}

func TestReplaceManagedCronRejectsIncompleteBlock(t *testing.T) {
	_, err := replaceManagedCron(cronStartMarker+"\n@daily /bin/one\n", "")
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete block error, got %v", err)
	}
}

func TestRenderSystemdServiceEscapesExecPath(t *testing.T) {
	service, err := renderSystemdService("/tmp/path with%chars/$bin/ssh-gh-id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(service, `ExecStart=/tmp/path\x20with%%chars/$$bin/ssh-gh-id --update-all`) {
		t.Fatalf("service did not escape ExecStart as expected:\n%s", service)
	}
}

func TestRenderSystemdServiceRejectsUnsafeExecPath(t *testing.T) {
	for _, path := range []string{"relative/ssh-gh-id", "/tmp/bad\nssh-gh-id", "/tmp/bad\"quote/ssh-gh-id"} {
		if _, err := renderSystemdService(path); err == nil {
			t.Fatalf("expected renderSystemdService(%q) to fail", path)
		}
	}
}

func TestRenderSystemdTimer(t *testing.T) {
	calendar, err := renderSystemdTimer(parsedInterval{Kind: intervalSystemdCalendar, Original: "daily", SystemdCalendar: "daily", CronSpec: "@daily"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calendar, "OnCalendar=daily") || !strings.Contains(calendar, "Persistent=true") {
		t.Fatalf("calendar timer missing expected fields:\n%s", calendar)
	}
	duration, err := renderSystemdTimer(parsedInterval{Kind: intervalSystemdDuration, Original: "12h", SystemdDuration: formatSystemdDuration(12 * time.Hour), CronSpec: "0 */12 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(duration, "OnBootSec=2min") || !strings.Contains(duration, "OnUnitActiveSec=12h") {
		t.Fatalf("duration timer missing expected fields:\n%s", duration)
	}
}

func TestRenderSystemdTimerRejectsControlCharacters(t *testing.T) {
	_, err := renderSystemdTimer(parsedInterval{Kind: intervalSystemdCalendar, Original: "bad", SystemdCalendar: "daily\nUnit=evil.service"})
	if err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatalf("expected control-character error, got %v", err)
	}
}

func TestNormalizeSchedulerName(t *testing.T) {
	cases := map[string]string{
		"":                "auto",
		"auto":            "auto",
		"cron":            "crontab",
		"user-cron":       "crontab",
		"system":          "systemd",
		"systemd-system":  "systemd",
		"user":            "systemd-user",
		"systemd-user":    "systemd-user",
		"UNKNOWN-BACKEND": "unknown-backend",
	}
	for in, want := range cases {
		if got := normalizeSchedulerName(in); got != want {
			t.Fatalf("normalizeSchedulerName(%q)=%q want %q", in, got, want)
		}
	}
}
