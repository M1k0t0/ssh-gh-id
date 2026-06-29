package sshghid

import (
	"strings"
	"testing"
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
