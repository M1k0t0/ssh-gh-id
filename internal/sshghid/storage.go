package sshghid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (a *App) loadConfig() (Config, error) {
	cfg := Config{
		AuthorizedKeysPath: a.AuthorizedKeysPath,
		Interval:           defaultInterval,
	}
	b, err := os.ReadFile(a.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.AuthorizedKeysPath == "" {
		cfg.AuthorizedKeysPath = a.AuthorizedKeysPath
	}
	if cfg.Interval == "" {
		cfg.Interval = defaultInterval
	}
	if cfg.Scheduler == "" {
		cfg.Scheduler = "auto"
	}
	cfg.AuthorizedKeysPath = expandHome(cfg.AuthorizedKeysPath, a.Home)
	return cfg, nil
}

func (a *App) saveConfig(cfg Config) error {
	cfg.AuthorizedKeysPath = expandHome(cfg.AuthorizedKeysPath, a.Home)
	if cfg.AuthorizedKeysPath == "" {
		cfg.AuthorizedKeysPath = a.AuthorizedKeysPath
	}
	if cfg.Interval == "" {
		cfg.Interval = defaultInterval
	}
	cfg.Scheduler = normalizeSchedulerName(cfg.Scheduler)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	b = append(b, '\n')
	return writeFileAtomic(a.ConfigPath, b, 0o644)
}

func (a *App) loadUsers() ([]string, error) {
	b, err := os.ReadFile(a.UsersPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read users: %w", err)
	}
	var uf UsersFile
	if err := json.Unmarshal(b, &uf); err != nil {
		return nil, fmt.Errorf("parse users: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(uf.Users))
	for _, user := range uf.Users {
		norm, err := normalizeUsername(user)
		if err != nil {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	sort.Strings(out)
	return out, nil
}

func (a *App) saveUsers(users []string) error {
	users = append([]string(nil), users...)
	sort.Strings(users)
	b, err := json.MarshalIndent(UsersFile{Users: users}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	b = append(b, '\n')
	return writeFileAtomic(a.UsersPath, b, 0o644)
}

func (a *App) loadStatus() (Status, error) {
	var status Status
	b, err := os.ReadFile(a.StatusPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}
	err = json.Unmarshal(b, &status)
	return status, err
}

func (a *App) saveStatus(status Status) error {
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(a.StatusPath, b, 0o644)
}

func (a *App) saveCache(cache UserCache) error {
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(a.cachePath(cache.Username), b, 0o644)
}

func (a *App) loadCache(username string) (UserCache, error) {
	var cache UserCache
	b, err := os.ReadFile(a.cachePath(username))
	if err != nil {
		return cache, err
	}
	if err := json.Unmarshal(b, &cache); err != nil {
		return cache, err
	}
	return cache, nil
}

func (a *App) cachePath(username string) string {
	return filepath.Join(a.CacheDir, username+".json")
}

func (a *App) logf(format string, args ...any) error {
	if err := os.MkdirAll(filepath.Dir(a.LogPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(a.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", a.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	return err
}
