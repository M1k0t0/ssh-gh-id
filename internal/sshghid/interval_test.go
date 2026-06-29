package sshghid

import (
	"fmt"
	"testing"
)

func TestParseIntervalSpec(t *testing.T) {
	cases := []struct {
		in       string
		kind     string
		cronSpec string
	}{
		{in: "daily", kind: "systemd-calendar", cronSpec: "@daily"},
		{in: "12h", kind: "systemd-duration", cronSpec: "0 */12 * * *"},
		{in: "0 */6 * * *", kind: "cron", cronSpec: "0 */6 * * *"},
	}
	for _, tc := range cases {
		got, err := parseIntervalSpec(tc.in)
		if err != nil {
			t.Fatalf("parseIntervalSpec(%q): %v", tc.in, err)
		}
		if fmt.Sprint(got.Kind) != tc.kind {
			t.Fatalf("parseIntervalSpec(%q).Kind=%q want %q", tc.in, got.Kind, tc.kind)
		}
		if got.CronSpec != tc.cronSpec {
			t.Fatalf("parseIntervalSpec(%q).CronSpec=%q want %q", tc.in, got.CronSpec, tc.cronSpec)
		}
	}
}
