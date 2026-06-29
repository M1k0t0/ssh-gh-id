package sshghid

import (
	"fmt"
	"strconv"
	"strings"
)

type migration struct {
	Version string
	Name    string
	Run     func(*App) error
}

var migrations = []migration{
	// Future migrations go here. A migration with Version "0.4.0" runs when
	// updating from any version below 0.4.0 to 0.4.0 or newer.
}

func (a *App) runMigrations(fromVersion, toVersion string) (int, error) {
	cmp, err := compareVersions(fromVersion, toVersion)
	if err != nil {
		return 0, err
	}
	if cmp >= 0 {
		return 0, nil
	}
	ran := 0
	for _, migration := range migrations {
		applies, err := migrationApplies(fromVersion, toVersion, migration.Version)
		if err != nil {
			return ran, fmt.Errorf("check migration %s: %w", migration.Name, err)
		}
		if !applies {
			continue
		}
		if err := migration.Run(a); err != nil {
			return ran, fmt.Errorf("run migration %s: %w", migration.Name, err)
		}
		ran++
	}
	return ran, nil
}

func migrationApplies(fromVersion, toVersion, migrationVersion string) (bool, error) {
	fromCmp, err := compareVersions(fromVersion, migrationVersion)
	if err != nil {
		return false, err
	}
	toCmp, err := compareVersions(migrationVersion, toVersion)
	if err != nil {
		return false, err
	}
	return fromCmp < 0 && toCmp <= 0, nil
}

func compareVersions(left, right string) (int, error) {
	leftParts, err := parseVersionParts(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseVersionParts(right)
	if err != nil {
		return 0, err
	}
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for i := 0; i < maxLen; i++ {
		leftPart := 0
		if i < len(leftParts) {
			leftPart = leftParts[i]
		}
		rightPart := 0
		if i < len(rightParts) {
			rightPart = rightParts[i]
		}
		switch {
		case leftPart > rightPart:
			return 1, nil
		case leftPart < rightPart:
			return -1, nil
		}
	}
	return 0, nil
}

func parseVersionParts(v string) ([]int, error) {
	original := v
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	if v == "" {
		return nil, fmt.Errorf("invalid version %q", original)
	}
	fields := strings.Split(v, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			return nil, fmt.Errorf("invalid version %q", original)
		}
		part, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("invalid version %q: %w", original, err)
		}
		parts = append(parts, part)
	}
	return parts, nil
}
