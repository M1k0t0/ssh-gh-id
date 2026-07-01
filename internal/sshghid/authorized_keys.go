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

type authorizedKeysUpdate struct {
	path     string
	snapshot fileSnapshot
	next     []byte
	count    int
	changed  bool
}

type managedBlockSpan struct {
	start          int
	endMarkerStart int
	endAfter       int
	present        bool
}

func (a *App) applyAuthorizedKeys(users []string, fetchMissing bool) error {
	update, err := a.prepareAuthorizedKeysUpdate(users, fetchMissing)
	if err != nil {
		return err
	}
	if !update.changed {
		_ = a.logf("authorized_keys unchanged (%d managed keys)", update.count)
		return nil
	}
	if err := update.commit(); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}
	_ = a.logf("authorized_keys updated (%d managed keys)", update.count)
	return nil
}

func (a *App) prepareAuthorizedKeysUpdate(users []string, fetchMissing bool) (authorizedKeysUpdate, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return authorizedKeysUpdate{}, err
	}
	path := cfg.AuthorizedKeysPath
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return authorizedKeysUpdate{}, fmt.Errorf("authorized_keys path %s is a symlink; refusing to replace it", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return authorizedKeysUpdate{}, fmt.Errorf("stat authorized_keys: %w", err)
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return authorizedKeysUpdate{}, fmt.Errorf("read authorized_keys: %w", err)
	}
	snapshot, err := snapshotFile(path)
	if err != nil {
		return authorizedKeysUpdate{}, fmt.Errorf("snapshot authorized_keys: %w", err)
	}
	if _, err := findManagedBlockSpan(string(current)); err != nil {
		return authorizedKeysUpdate{}, err
	}
	block, count, err := a.renderManagedBlock(users, fetchMissing)
	if err != nil {
		return authorizedKeysUpdate{}, err
	}
	next, err := replaceManagedBlock(string(current), block)
	if err != nil {
		return authorizedKeysUpdate{}, err
	}
	return authorizedKeysUpdate{
		path:     path,
		snapshot: snapshot,
		next:     []byte(next),
		count:    count,
		changed:  next != string(current),
	}, nil
}

func (u authorizedKeysUpdate) commit() error {
	if !u.changed {
		return nil
	}
	return writeFileAtomicWithExistingMode(u.path, u.next, 0o600)
}

func (u authorizedKeysUpdate) rollback() error {
	if !u.changed {
		return nil
	}
	return u.snapshot.restore(0o600)
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
		keys, err := validateAndNormalizePublicKeyLines("cached keys for "+user, cache.Keys)
		if err != nil {
			return "", 0, err
		}
		if len(keys) == 0 {
			lines = append(lines, "# no public keys published on GitHub")
			continue
		}
		lines = append(lines, keys...)
		managedKeys += len(keys)
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
	span, err := findManagedBlockSpan(content)
	if err != nil {
		return 0, err
	}
	if !span.present {
		return 0, nil
	}
	block := content[span.start:span.endMarkerStart]
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
	span, err := findManagedBlockSpan(current)
	if err != nil {
		return "", err
	}
	if !span.present {
		if current == "" {
			return block, nil
		}
		next := strings.TrimRight(current, "\n") + "\n\n" + block
		return next, nil
	}
	return current[:span.start] + block + current[span.endAfter:], nil
}

func findManagedBlockSpan(current string) (managedBlockSpan, error) {
	startCount := strings.Count(current, startMarker)
	endCount := strings.Count(current, endMarker)
	switch {
	case startCount == 0 && endCount == 0:
		return managedBlockSpan{}, nil
	case startCount != 1 || endCount != 1:
		if startCount > 1 || endCount > 1 {
			return managedBlockSpan{}, errors.New("authorized_keys contains multiple ssh-gh-id managed blocks; remove duplicate managed blocks manually")
		}
		return managedBlockSpan{}, errors.New("authorized_keys contains an incomplete ssh-gh-id managed block")
	}
	start := strings.Index(current, startMarker)
	end := strings.Index(current, endMarker)
	if start == -1 || end == -1 || end < start {
		return managedBlockSpan{}, errors.New("authorized_keys contains an incomplete ssh-gh-id managed block")
	}
	endAfter := end + len(endMarker)
	if endAfter < len(current) && current[endAfter] == '\n' {
		endAfter++
	}
	return managedBlockSpan{start: start, endMarkerStart: end, endAfter: endAfter, present: true}, nil
}
