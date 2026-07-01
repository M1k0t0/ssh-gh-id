package sshghid

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type intervalKind string

const (
	intervalSystemdCalendar intervalKind = "systemd-calendar"
	intervalSystemdDuration intervalKind = "systemd-duration"
	intervalCron            intervalKind = "cron"
)

type parsedInterval struct {
	Kind            intervalKind
	Original        string
	SystemdCalendar string
	SystemdDuration string
	CronSpec        string
}

func parseIntervalSpec(spec string) (parsedInterval, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return parsedInterval{}, errors.New("interval cannot be empty")
	}
	if containsControlChar(raw) {
		return parsedInterval{}, errors.New("interval cannot contain control characters")
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "hourly", "@hourly":
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: "hourly", CronSpec: "@hourly"}, nil
	case "daily", "@daily":
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: "daily", CronSpec: "@daily"}, nil
	case "weekly", "@weekly":
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: "weekly", CronSpec: "@weekly"}, nil
	case "monthly", "@monthly":
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: "monthly", CronSpec: "@monthly"}, nil
	case "yearly", "annually", "@yearly", "@annually":
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: "yearly", CronSpec: "@yearly"}, nil
	}
	if d, ok := parseFlexibleDuration(lower); ok {
		return parsedInterval{Kind: intervalSystemdDuration, Original: raw, SystemdDuration: formatSystemdDuration(d), CronSpec: durationToCron(d)}, nil
	}
	if isCronSpec(raw) {
		return parsedInterval{Kind: intervalCron, Original: raw, CronSpec: raw}, nil
	}
	if looksLikeSystemdOnCalendar(raw) {
		return parsedInterval{Kind: intervalSystemdCalendar, Original: raw, SystemdCalendar: raw}, nil
	}
	return parsedInterval{}, fmt.Errorf("unsupported interval %q; use daily/@daily, a 5-field cron expression, or a duration like 12h", spec)
}

func parseFlexibleDuration(v string) (time.Duration, bool) {
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, true
		}
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

func formatSystemdDuration(d time.Duration) string {
	seconds := int64(d.Seconds())
	if seconds%86400 == 0 {
		return fmt.Sprintf("%dd", seconds/86400)
	}
	if seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%dmin", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func durationToCron(d time.Duration) string {
	switch d {
	case time.Hour:
		return "@hourly"
	case 24 * time.Hour:
		return "@daily"
	case 7 * 24 * time.Hour:
		return "@weekly"
	case 30 * 24 * time.Hour:
		return "@monthly"
	}
	if d > 0 && d < 24*time.Hour && d%time.Hour == 0 {
		hours := int(d / time.Hour)
		if 24%hours == 0 {
			return fmt.Sprintf("0 */%d * * *", hours)
		}
	}
	return ""
}

func isCronSpec(spec string) bool {
	fields := strings.Fields(spec)
	if len(fields) == 1 && strings.HasPrefix(fields[0], "@") {
		switch fields[0] {
		case "@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually":
			return true
		}
	}
	return len(fields) == 5
}

func looksLikeSystemdOnCalendar(spec string) bool {
	return strings.ContainsAny(spec, "*-:") || strings.Contains(strings.ToLower(spec), "mon") || strings.Contains(strings.ToLower(spec), "fri")
}
