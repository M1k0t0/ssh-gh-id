package sshghid

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleAdd(username string) error {
	username, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := a.ensureDirs(); err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	changed := addUnique(&users, username)
	if changed {
		if err := a.saveUsers(users); err != nil {
			return err
		}
	}
	fetchErr := a.refreshUserCache(context.Background(), username)
	applyErr := a.applyAuthorizedKeys(users, true)
	status := Status{
		LastRunAt:      a.Now(),
		LastAction:     "add",
		Users:          len(users),
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      a.detectScheduler(),
	}
	if fetchErr == nil && applyErr == nil {
		status.LastSuccessAt = a.Now()
	}
	if err := errors.Join(fetchErr, applyErr); err != nil {
		status.LastError = err.Error()
		_ = a.saveStatus(status)
		if !changed {
			return fmt.Errorf("%s already configured; refresh failed: %w", username, err)
		}
		return fmt.Errorf("%s added, but update failed: %w", username, err)
	}
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	if changed {
		fmt.Printf("%s %s\n", successText("added"), keyText(username))
	} else {
		fmt.Printf("%s %s\n", warnText("already configured"), keyText(username))
	}
	return nil
}

func (a *App) handleDelete(username string) error {
	username, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := a.ensureDirs(); err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	updated, found := removeValue(users, username)
	if !found {
		fmt.Printf("%s %s\n", warnText("not configured"), keyText(username))
		return nil
	}
	if err := a.saveUsers(updated); err != nil {
		return err
	}
	_ = os.Remove(a.cachePath(username))
	if err := a.applyAuthorizedKeys(updated, false); err != nil {
		return err
	}
	status := Status{
		LastRunAt:      a.Now(),
		LastSuccessAt:  a.Now(),
		LastAction:     "del",
		Users:          len(updated),
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      a.detectScheduler(),
	}
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	fmt.Printf("%s %s\n", successText("deleted"), keyText(username))
	return nil
}

func (a *App) handleList() error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	for _, user := range users {
		fmt.Printf("  %s\n", keyText(user))
	}
	if len(users) == 0 {
		fmt.Println(dimText("(no users configured)"))
	}
	return nil
}

func (a *App) handleUpdate(username string) error {
	username, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := a.ensureDirs(); err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	if !contains(users, username) {
		return fmt.Errorf("%s is not configured; add it first", username)
	}
	refreshErr := a.refreshUserCache(context.Background(), username)
	applyErr := a.applyAuthorizedKeys(users, true)
	status := Status{
		LastRunAt:      a.Now(),
		LastAction:     "update",
		Users:          len(users),
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      a.detectScheduler(),
	}
	if err := errors.Join(refreshErr, applyErr); err != nil {
		status.LastError = err.Error()
		_ = a.saveStatus(status)
		return err
	}
	status.LastSuccessAt = a.Now()
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	fmt.Printf("%s %s\n", successText("updated"), keyText(username))
	return nil
}

func (a *App) handleUpdateAll() error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println(dimText("no users configured"))
		return nil
	}
	var merr multiError
	for _, user := range users {
		if err := a.refreshUserCache(context.Background(), user); err != nil {
			merr.add("%s: %v", user, err)
		}
	}
	applyErr := a.applyAuthorizedKeys(users, false)
	status := Status{
		LastRunAt:      a.Now(),
		LastAction:     "update-all",
		Users:          len(users),
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      a.detectScheduler(),
	}
	joined := errors.Join(merr.err(), applyErr)
	if joined != nil {
		status.LastError = joined.Error()
		_ = a.saveStatus(status)
		return joined
	}
	status.LastSuccessAt = a.Now()
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	fmt.Printf("%s %s users\n", successText("updated"), keyText(strconv.Itoa(len(users))))
	return nil
}

func (a *App) handleSetInterval(spec string) error {
	parsed, err := parseIntervalSpec(spec)
	if err != nil {
		return err
	}
	if err := a.ensureDirs(); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	cfg.Interval = parsed.Original
	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", successText("interval set to"), keyText(parsed.Original))
	if a.detectScheduler() != "none" {
		fmt.Println(infoText("scheduler is already installed; run -i again to rewrite it"))
	}
	return nil
}

func (a *App) handleSetScheduler(name string) error {
	scheduler := normalizeSchedulerName(name)
	if !isValidScheduler(scheduler) {
		return fmt.Errorf("unsupported scheduler %q; use systemd, systemd-user, crontab, or auto", name)
	}
	if err := a.ensureDirs(); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	cfg.Scheduler = scheduler
	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", successText("scheduler set to"), keyText(scheduler))
	switch scheduler {
	case "systemd":
		fmt.Println(infoText("systemd installs to /etc/systemd/system and requires root"))
	case "systemd-user":
		fmt.Println(warnText("systemd-user timers may stop after logout unless linger is enabled: loginctl enable-linger " + currentUsernameForHint()))
	case "crontab":
		fmt.Println(infoText("crontab uses the current user's crontab and does not require sudo"))
	case "auto":
		fmt.Println(infoText("auto uses systemd for root and crontab for non-root users"))
	}
	if a.detectScheduler() != "none" {
		fmt.Println(infoText("run -i again to reinstall using the selected scheduler"))
	}
	return nil
}

func (a *App) handleInstall() error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if err := a.installBinary(); err != nil {
		return err
	}
	profilePath, pathAdded, err := a.ensureLocalBinOnPath()
	if err != nil {
		return err
	}
	method, err := a.installScheduler(cfg.Interval, cfg.Scheduler)
	if err != nil {
		return err
	}
	status := Status{
		LastRunAt:      a.Now(),
		LastSuccessAt:  a.Now(),
		LastAction:     "install",
		Users:          0,
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      method,
	}
	if users, err := a.loadUsers(); err == nil {
		status.Users = len(users)
	}
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	binDir := filepath.Dir(a.LocalBinPath)
	pathMsg := infoText(binDir + " is already on your PATH")
	if pathAdded {
		pathMsg = successText("added " + binDir + " to PATH in " + profilePath + " (open a new shell or source the file)")
	} else if !pathContains(a.LocalBinPath) {
		pathMsg = warnText(binDir + " is not on your current PATH, but a PATH block is already present in " + profilePath + "; open a new shell or source the file")
	}
	fmt.Printf("%s %s %s\n", successText("installed"), keyText(a.LocalBinPath), dimText("and scheduler ("+method+")"))
	fmt.Println(pathMsg)
	fmt.Println()
	fmt.Print(usageText())
	return nil
}

func (a *App) handleUninstall() error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	var merr multiError
	if err := a.uninstallScheduler(); err != nil {
		merr.add("scheduler: %v", err)
	}
	if err := os.Remove(a.LocalBinPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		merr.add("binary: %v", err)
	}
	status := Status{
		LastRunAt:      a.Now(),
		LastSuccessAt:  a.Now(),
		LastAction:     "uninstall",
		Users:          0,
		AuthorizedKeys: a.resolveAuthorizedKeysPathMust(),
		Scheduler:      "none",
	}
	if users, err := a.loadUsers(); err == nil {
		status.Users = len(users)
	}
	status.KeysInstalled, _ = a.countInstalledKeys()
	_ = a.saveStatus(status)
	if err := merr.err(); err != nil {
		return err
	}
	fmt.Println(successText("uninstalled scheduler and binary"))
	return nil
}

func (a *App) handleSelfUpdate() error {
	unlock, err := a.acquireLock()
	if err != nil {
		return err
	}
	defer unlock()
	result, err := a.selfUpdate(context.Background(), currentReleasePlatform())
	if err != nil {
		return err
	}
	if !result.Updated {
		return nil
	}
	if err := a.runUpdatedBinaryMigrations(result.PreviousVersion, result.NewVersion); err != nil {
		return err
	}
	return nil
}

func (a *App) handleRunMigrations(fromVersion, toVersion string) error {
	if fromVersion == "" || toVersion == "" {
		return errors.New("migration runner requires both source and target versions")
	}
	var unlock func()
	if os.Getenv("SSH_GH_ID_PARENT_LOCK_HELD") != "1" {
		var err error
		unlock, err = a.acquireLock()
		if err != nil {
			return err
		}
	}
	if unlock != nil {
		defer unlock()
	}
	ran, err := a.runMigrations(fromVersion, toVersion)
	if err != nil {
		return err
	}
	if ran == 0 {
		fmt.Printf("%s %s -> %s\n", dimText("no migrations needed"), keyText(fromVersion), keyText(toVersion))
		return nil
	}
	fmt.Printf("%s %s -> %s\n", successText("migrations complete"), keyText(fromVersion), keyText(toVersion))
	return nil
}

func (a *App) handleStatus() error {
	if err := a.ensureDirs(); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	users, err := a.loadUsers()
	if err != nil {
		return err
	}
	status, _ := a.loadStatus()
	fmt.Println(titleText("ssh-gh-id status"))
	fmt.Printf("%s %s\n", dimText("version:"), keyText(version))
	fmt.Printf("%s %s\n", dimText("config:"), a.ConfigPath)
	fmt.Printf("%s %s\n", dimText("data:"), a.DataDir)
	fmt.Printf("%s %s\n", dimText("state:"), a.StateDir)
	fmt.Printf("%s %s\n", dimText("authorized_keys:"), cfg.AuthorizedKeysPath)
	fmt.Printf("%s %s\n", dimText("interval:"), keyText(cfg.Interval))
	fmt.Printf("%s %s\n", dimText("scheduler-config:"), keyText(normalizeSchedulerName(cfg.Scheduler)))
	fmt.Printf("%s %s\n", dimText("scheduler-installed:"), keyText(a.detectScheduler()))
	fmt.Printf("%s %s\n", dimText("users:"), keyText(strconv.Itoa(len(users))))
	if len(users) > 0 {
		fmt.Printf("%s %s\n", dimText("user-list:"), strings.Join(users, ", "))
	}
	keys, _ := a.countInstalledKeys()
	fmt.Printf("%s %s\n", dimText("managed-keys:"), keyText(strconv.Itoa(keys)))
	if !status.LastRunAt.IsZero() {
		fmt.Printf("%s %s\n", dimText("last-run:"), status.LastRunAt.Format(time.RFC3339))
	}
	if !status.LastSuccessAt.IsZero() {
		fmt.Printf("%s %s\n", dimText("last-success:"), status.LastSuccessAt.Format(time.RFC3339))
	}
	if status.LastError != "" {
		fmt.Printf("%s %s\n", errorText("last-error:"), status.LastError)
	}
	return nil
}
