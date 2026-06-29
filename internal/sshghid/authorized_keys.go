package sshghid

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

func (a *App) applyAuthorizedKeys(users []string, fetchMissing bool) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	authorizedKeysPath := cfg.AuthorizedKeysPath
	current, err := os.ReadFile(authorizedKeysPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read authorized_keys: %w", err)
	}
	block, count, err := a.renderManagedBlock(users, fetchMissing)
	if err != nil {
		return err
	}
	next, err := replaceManagedBlock(string(current), block)
	if err != nil {
		return err
	}
	if next == string(current) {
		_ = a.logf("authorized_keys unchanged (%d managed keys)", count)
		return nil
	}
	if err := writeFileAtomicWithExistingMode(authorizedKeysPath, []byte(next), 0o600); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}
	_ = a.logf("authorized_keys updated (%d managed keys)", count)
	return nil
}

func (a *App) renderManagedBlock(users []string, fetchMissing bool) (string, int, error) {
	users = append([]string(nil), users...)
	sort.Strings(users)
	lines := []string{
		startMarker,
		"# managed by ssh-gh-id; edits outside this block are preserved",
	}
	managedKeys := 0
	for _, user := range users {
		cache, err := a.loadCache(user)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && fetchMissing {
				if ferr := a.refreshUserCache(context.Background(), user); ferr == nil {
					cache, err = a.loadCache(user)
				}
			}
		}
		lines = append(lines, fmt.Sprintf("# user: %s", user))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				lines = append(lines, "# no cached keys yet")
				continue
			}
			return "", 0, fmt.Errorf("load cache for %s: %w", user, err)
		}
		if len(cache.Keys) == 0 {
			lines = append(lines, "# no public keys published on GitHub")
			continue
		}
		lines = append(lines, cache.Keys...)
		managedKeys += len(cache.Keys)
	}
	lines = append(lines, endMarker)
	return strings.Join(lines, "\n") + "\n", managedKeys, nil
}

func (a *App) countInstalledKeys() (int, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(cfg.AuthorizedKeysPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	content := string(b)
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start == -1 || end == -1 || end < start {
		return 0, nil
	}
	block := content[start:end]
	count := 0
	s := bufio.NewScanner(strings.NewReader(block))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	return count, s.Err()
}

func replaceManagedBlock(current, block string) (string, error) {
	start := strings.Index(current, startMarker)
	end := strings.Index(current, endMarker)
	switch {
	case start == -1 && end == -1:
		if current == "" {
			return block, nil
		}
		next := strings.TrimRight(current, "\n") + "\n\n" + block
		return next, nil
	case start == -1 || end == -1 || end < start:
		return "", errors.New("authorized_keys contains an incomplete ssh-gh-id managed block")
	default:
		end += len(endMarker)
		if end < len(current) && current[end] == '\n' {
			end++
		}
		return current[:start] + block + current[end:], nil
	}
}
