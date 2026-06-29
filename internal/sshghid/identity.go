package sshghid

import (
	"fmt"
	"regexp"
	"strings"
)

var githubUsernameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$|^[A-Za-z0-9]$`)

func normalizeUsername(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !githubUsernameRE.MatchString(v) {
		return "", fmt.Errorf("invalid GitHub username %q", v)
	}
	if strings.HasPrefix(v, "-") || strings.HasSuffix(v, "-") {
		return "", fmt.Errorf("invalid GitHub username %q", v)
	}
	return strings.ToLower(v), nil
}
