package sshghid

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func newApp() (*App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	configHome := firstNonEmpty(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	dataHome := firstNonEmpty(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	stateHome := firstNonEmpty(os.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))

	configDir := filepath.Join(configHome, appName)
	dataDir := filepath.Join(dataHome, appName)
	stateDir := filepath.Join(stateHome, appName)
	cacheDir := filepath.Join(stateDir, cacheDirname)
	logDir := filepath.Join(stateDir, logsDirname)
	authorizedKeysPath := firstNonEmpty(os.Getenv("SSH_GH_ID_AUTHORIZED_KEYS_PATH"), filepath.Join(home, ".ssh", "authorized_keys"))
	installPath := defaultInstallPath(home)
	if exe, err := os.Executable(); err == nil {
		installPath = filepath.Join(filepath.Dir(exe), appName)
	}

	return &App{
		Home:                   home,
		ConfigDir:              configDir,
		DataDir:                dataDir,
		StateDir:               stateDir,
		ConfigPath:             filepath.Join(configDir, configFilename),
		UsersPath:              filepath.Join(dataDir, usersFilename),
		StatusPath:             filepath.Join(stateDir, statusFilename),
		LogPath:                filepath.Join(logDir, logFilename),
		LockPath:               filepath.Join(stateDir, lockFilename),
		CacheDir:               cacheDir,
		AuthorizedKeysPath:     authorizedKeysPath,
		LocalBinPath:           installPath,
		SystemdDir:             filepath.Join(configHome, "systemd", "user"),
		SystemdUnitPath:        filepath.Join(configHome, "systemd", "user", systemdUnitName),
		SystemdTimerPath:       filepath.Join(configHome, "systemd", "user", systemdTimerName),
		SystemSystemdDir:       filepath.Join(string(filepath.Separator), "etc", "systemd", "system"),
		SystemSystemdUnitPath:  filepath.Join(string(filepath.Separator), "etc", "systemd", "system", systemdUnitName),
		SystemSystemdTimerPath: filepath.Join(string(filepath.Separator), "etc", "systemd", "system", systemdTimerName),
		BaseURL:                strings.TrimRight(firstNonEmpty(os.Getenv("SSH_GH_ID_KEYS_BASE_URL"), "https://github.com"), "/"),
		ReleaseAPIURL:          firstNonEmpty(os.Getenv("SSH_GH_ID_RELEASE_API_URL"), releaseAPIURL),
		Now:                    time.Now,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func defaultInstallPath(home string) string {
	return filepath.Join(home, ".local", "bin", appName)
}

func (a *App) ensureDirs() error {
	for _, dir := range []string{a.ConfigDir, a.DataDir, a.StateDir, a.CacheDir, filepath.Dir(a.LogPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	sshDir := filepath.Dir(a.resolveAuthorizedKeysPathMust())
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create ssh dir %s: %w", sshDir, err)
	}
	return nil
}

func (a *App) acquireLock() (func(), error) {
	if err := os.MkdirAll(a.StateDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(a.LockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another ssh-gh-id process is already running")
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func (a *App) resolveAuthorizedKeysPathMust() string {
	cfg, err := a.loadConfig()
	if err == nil && cfg.AuthorizedKeysPath != "" {
		return cfg.AuthorizedKeysPath
	}
	return a.AuthorizedKeysPath
}
