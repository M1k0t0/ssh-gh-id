package sshghid

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	executablePath := ""
	if exe, err := os.Executable(); err == nil {
		executablePath = exe
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
		ExecutablePath:         executablePath,
		SystemdDir:             filepath.Join(configHome, "systemd", "user"),
		SystemdUnitPath:        filepath.Join(configHome, "systemd", "user", systemdUnitName),
		SystemdTimerPath:       filepath.Join(configHome, "systemd", "user", systemdTimerName),
		SystemSystemdDir:       filepath.Join(string(filepath.Separator), "etc", "systemd", "system"),
		SystemSystemdUnitPath:  filepath.Join(string(filepath.Separator), "etc", "systemd", "system", systemdUnitName),
		SystemSystemdTimerPath: filepath.Join(string(filepath.Separator), "etc", "systemd", "system", systemdTimerName),
		BaseURL:                strings.TrimRight(firstNonEmpty(os.Getenv("SSH_GH_ID_KEYS_BASE_URL"), "https://github.com"), "/"),
		ReleaseAPIURL:          releaseAPIURL,
		Now:                    time.Now,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func defaultInstallPath(home string) string {
	return filepath.Join(home, ".local", "bin", appName)
}

type lockHandle struct {
	file               *os.File
	skipExplicitUnlock bool
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
	lock, err := a.acquireLockHandle()
	if err != nil {
		return nil, err
	}
	return lock.release, nil
}

func (a *App) acquireLockHandle() (*lockHandle, error) {
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
	return &lockHandle{file: f}, nil
}

func (a *App) acquireMigrationLock() (*lockHandle, error) {
	fdText := os.Getenv("SSH_GH_ID_LOCK_FD")
	if fdText == "" {
		return nil, errors.New("migration runner requires inherited lock fd")
	}
	fd, err := strconv.Atoi(fdText)
	if err != nil || fd < 0 {
		return nil, fmt.Errorf("invalid inherited lock fd %q", fdText)
	}
	f := os.NewFile(uintptr(fd), "ssh-gh-id-lock")
	if f == nil {
		return nil, fmt.Errorf("invalid inherited lock fd %q", fdText)
	}
	if err := a.validateLockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another ssh-gh-id process is already running")
		}
		return nil, fmt.Errorf("acquire inherited lock: %w", err)
	}
	return &lockHandle{file: f, skipExplicitUnlock: true}, nil
}

func (a *App) validateLockFile(f *os.File) error {
	lockInfo, err := os.Stat(a.LockPath)
	if err != nil {
		return fmt.Errorf("stat lock file: %w", err)
	}
	fdInfo, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat inherited lock fd: %w", err)
	}
	if !os.SameFile(lockInfo, fdInfo) {
		return errors.New("inherited lock fd does not match ssh-gh-id lock file")
	}
	return nil
}

func (l *lockHandle) release() {
	if l == nil || l.file == nil {
		return
	}
	if !l.skipExplicitUnlock {
		_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	}
	_ = l.file.Close()
}

func (a *App) resolveAuthorizedKeysPathMust() string {
	cfg, err := a.loadConfig()
	if err == nil && cfg.AuthorizedKeysPath != "" {
		return cfg.AuthorizedKeysPath
	}
	return a.AuthorizedKeysPath
}
