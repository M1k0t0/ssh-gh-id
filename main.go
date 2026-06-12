package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	version          = "0.3.1"
	appName          = "ssh-gh-id"
	startMarker      = "# >>> ssh-gh-id managed block >>>"
	endMarker        = "# <<< ssh-gh-id managed block <<<"
	cronStartMarker  = "# >>> ssh-gh-id managed cron >>>"
	cronEndMarker    = "# <<< ssh-gh-id managed cron <<<"
	pathStartMarker  = "# >>> ssh-gh-id managed path >>>"
	pathEndMarker    = "# <<< ssh-gh-id managed path <<<"
	defaultInterval  = "daily"
	lockFilename     = "lock"
	statusFilename   = "status.json"
	configFilename   = "config.json"
	usersFilename    = "users.json"
	cacheDirname     = "cache"
	logsDirname      = "logs"
	logFilename      = "ssh-gh-id.log"
	httpUserAgent    = "ssh-gh-id/0.3.1 (+https://github.com/)"
	systemdUnitName  = "ssh-gh-id.service"
	systemdTimerName = "ssh-gh-id.timer"
)

var githubUsernameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$|^[A-Za-z0-9]$`)

const (
	ansiReset  = "[0m"
	ansiBold   = "[1m"
	ansiDim    = "[2m"
	ansiRed    = "[31m"
	ansiGreen  = "[32m"
	ansiYellow = "[33m"
	ansiBlue   = "[34m"
	ansiCyan   = "[36m"
)

func useColor() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	return term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

func colorize(code, s string) string {
	if !useColor() {
		return s
	}
	return code + s + ansiReset
}

func successText(s string) string { return colorize(ansiGreen, s) }
func warnText(s string) string    { return colorize(ansiYellow, s) }
func infoText(s string) string    { return colorize(ansiCyan, s) }
func errorText(s string) string   { return colorize(ansiRed, s) }
func keyText(s string) string     { return colorize(ansiBlue, s) }
func titleText(s string) string   { return colorize(ansiBold, s) }
func dimText(s string) string     { return colorize(ansiDim, s) }

type Config struct {
	AuthorizedKeysPath string `json:"authorized_keys_path"`
	Interval           string `json:"interval"`
	Scheduler          string `json:"scheduler"`
}

type UsersFile struct {
	Users []string `json:"users"`
}

type UserCache struct {
	Username  string    `json:"username"`
	Keys      []string  `json:"keys"`
	FetchedAt time.Time `json:"fetched_at"`
}

type Status struct {
	LastRunAt      time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt  time.Time `json:"last_success_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	LastAction     string    `json:"last_action,omitempty"`
	Users          int       `json:"users,omitempty"`
	KeysInstalled  int       `json:"keys_installed,omitempty"`
	Scheduler      string    `json:"scheduler,omitempty"`
	AuthorizedKeys string    `json:"authorized_keys,omitempty"`
}

type App struct {
	Home                   string
	ConfigDir              string
	DataDir                string
	StateDir               string
	ConfigPath             string
	UsersPath              string
	StatusPath             string
	LogPath                string
	LockPath               string
	CacheDir               string
	AuthorizedKeysPath     string
	LocalBinPath           string
	SystemdDir             string
	SystemdUnitPath        string
	SystemdTimerPath       string
	SystemSystemdDir       string
	SystemSystemdUnitPath  string
	SystemSystemdTimerPath string
	BaseURL                string
	Now                    func() time.Time
	HTTPClient             *http.Client
}

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

type multiError struct {
	parts []string
}

func boolPtr(v bool) *bool { return &v }

func (m *multiError) add(format string, args ...any) {
	m.parts = append(m.parts, fmt.Sprintf(format, args...))
}

func (m *multiError) err() error {
	if len(m.parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(m.parts, "; "))
}

func main() {
	app, err := newApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := app.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

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
		Now:                    time.Now,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func defaultInstallPath(home string) string {
	return filepath.Join(home, ".local", "bin", appName)
}

func (a *App) run(args []string) error {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	addUser := fs.String("add", "", "Add a GitHub username and update authorized_keys")
	addUserShort := fs.String("a", "", "Add a GitHub username and update authorized_keys")
	delUser := fs.String("del", "", "Delete a GitHub username and remove cached keys from the managed block")
	delUserShort := fs.String("d", "", "Delete a GitHub username and remove cached keys from the managed block")
	listUsers := fs.Bool("list", false, "List configured GitHub usernames")
	listUsersShort := fs.Bool("l", false, "List configured GitHub usernames")
	updateUser := fs.String("update", "", "Update one GitHub username from github.com/<user>.keys")
	updateUserShort := fs.String("u", "", "Update one GitHub username from github.com/<user>.keys")
	updateAll := fs.Bool("update-all", false, "Update all configured GitHub usernames")
	updateAllShort := fs.Bool("U", false, "Update all configured GitHub usernames")
	setInterval := fs.String("set-interval", "", "Set scheduler interval, for example daily, @hourly, 0 */6 * * *, 12h")
	setIntervalShort := fs.String("t", "", "Set scheduler interval")
	setScheduler := fs.String("set-scheduler", "", "Set scheduler backend: systemd, systemd-user, crontab, or auto")
	install := fs.Bool("install", false, "Install the binary and scheduler")
	installShort := fs.Bool("i", false, "Install the binary and scheduler")
	uninstall := fs.Bool("uninstall", false, "Remove the scheduler and installed binary")
	status := fs.Bool("status", false, "Show status")
	statusShort := fs.Bool("s", false, "Show status")
	showVersion := fs.Bool("version", false, "Show version")
	showVersionShort := fs.Bool("v", false, "Show version")
	help := fs.Bool("help", false, "Show help")
	helpShort := fs.Bool("h", false, "Show help")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w\n\n%s", err, usageText())
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected positional arguments: %s\n\n%s", strings.Join(fs.Args(), " "), usageText())
	}
	if *addUser == "" && *addUserShort != "" {
		*addUser = *addUserShort
	}
	if *delUser == "" && *delUserShort != "" {
		*delUser = *delUserShort
	}
	if !*listUsers && *listUsersShort {
		*listUsers = true
	}
	if *updateUser == "" && *updateUserShort != "" {
		*updateUser = *updateUserShort
	}
	if !*updateAll && *updateAllShort {
		*updateAll = true
	}
	if *setInterval == "" && *setIntervalShort != "" {
		*setInterval = *setIntervalShort
	}
	if !*install && *installShort {
		*install = true
	}
	if !*status && *statusShort {
		*status = true
	}
	if !*showVersion && *showVersionShort {
		*showVersion = true
	}
	if !*help && *helpShort {
		*help = true
	}
	if *help {
		fmt.Print(usageText())
		return nil
	}
	if *showVersion {
		fmt.Println(keyText(version))
		return nil
	}

	actions := 0
	for _, active := range []bool{
		*addUser != "",
		*delUser != "",
		*listUsers,
		*updateUser != "",
		*updateAll,
		*setInterval != "",
		*setScheduler != "",
		*install,
		*uninstall,
		*status,
	} {
		if active {
			actions++
		}
	}
	if actions == 0 {
		fmt.Print(usageText())
		return nil
	}
	if actions > 1 {
		return fmt.Errorf("use exactly one action at a time\n\n%s", usageText())
	}

	if *listUsers {
		return a.handleList()
	}
	if *status {
		return a.handleStatus()
	}

	unlock, err := a.acquireLock()
	if err != nil {
		return err
	}
	defer unlock()

	if *setInterval != "" {
		return a.handleSetInterval(*setInterval)
	}
	if *setScheduler != "" {
		return a.handleSetScheduler(*setScheduler)
	}
	if *install {
		return a.handleInstall()
	}
	if *uninstall {
		return a.handleUninstall()
	}
	if *addUser != "" {
		return a.handleAdd(*addUser)
	}
	if *delUser != "" {
		return a.handleDelete(*delUser)
	}
	if *updateUser != "" {
		return a.handleUpdate(*updateUser)
	}
	if *updateAll {
		return a.handleUpdateAll()
	}
	return nil
}

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

func (a *App) refreshUserCache(ctx context.Context, username string) error {
	keys, err := a.fetchKeys(ctx, username)
	if err != nil {
		_ = a.logf("refresh %s failed: %v", username, err)
		return err
	}
	cache := UserCache{Username: username, Keys: keys, FetchedAt: a.Now().UTC()}
	if err := a.saveCache(cache); err != nil {
		return err
	}
	_ = a.logf("refreshed %s (%d keys)", username, len(keys))
	return nil
}

func (a *App) fetchKeys(ctx context.Context, username string) ([]string, error) {
	url := fmt.Sprintf("%s/%s.keys", a.BaseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", httpUserAgent)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("fetch %s: unexpected status %s %s", username, resp.Status, strings.TrimSpace(string(body)))
	}
	lines, err := readNonEmptyLines(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s keys: %w", username, err)
	}
	return lines, nil
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

func parseIntervalSpec(spec string) (parsedInterval, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return parsedInterval{}, errors.New("interval cannot be empty")
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

func (a *App) installBinary() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.LocalBinPath), 0o755); err != nil {
		return err
	}
	input, err := os.Open(exe)
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer input.Close()
	return writeReaderAtomic(a.LocalBinPath, input, 0o755)
}

func (a *App) installScheduler(spec string, scheduler string) (string, error) {
	parsed, err := parseIntervalSpec(spec)
	if err != nil {
		return "", err
	}
	scheduler = normalizeSchedulerName(scheduler)
	if scheduler == "auto" {
		if runningAsRoot() {
			scheduler = "systemd"
		} else if crontabAvailable() {
			scheduler = "crontab"
		} else if ok, _ := a.systemdUserUsable(); ok {
			scheduler = "systemd-user"
		} else {
			return "", errors.New("crontab is not installed and systemd --user is unavailable; install cron/cronie or use --set-scheduler systemd-user after enabling user systemd")
		}
	}

	switch scheduler {
	case "systemd":
		if !runningAsRoot() {
			return "", errors.New("systemd scheduler installs to /etc/systemd/system and requires root; use --set-scheduler crontab or --set-scheduler systemd-user")
		}
		if err := a.installSystemdSystem(parsed); err != nil {
			return "", err
		}
		return "systemd", nil
	case "systemd-user":
		if err := os.MkdirAll(a.SystemdDir, 0o755); err != nil {
			return "", err
		}
		if ok, err := a.systemdUserUsable(); err != nil {
			return "", err
		} else if !ok {
			return "", errors.New("systemd --user is unavailable; use --set-scheduler crontab")
		}
		if err := a.installSystemdUser(parsed); err != nil {
			return "", err
		}
		fmt.Println(warnText("systemd-user timers may stop after logout unless linger is enabled: loginctl enable-linger " + currentUsernameForHint()))
		return "systemd-user", nil
	case "crontab":
		if !crontabAvailable() {
			return "", errors.New("crontab executable not found in PATH; install cron/cronie or use --set-scheduler systemd-user")
		}
		if parsed.CronSpec == "" {
			return "", fmt.Errorf("interval %q cannot be expressed in crontab", spec)
		}
		if err := a.installCron(parsed); err != nil {
			return "", err
		}
		return "crontab", nil
	default:
		return "", fmt.Errorf("unsupported scheduler %q; use systemd, systemd-user, crontab, or auto", scheduler)
	}
}

func renderSystemdTimer(parsed parsedInterval) (string, error) {
	timer := &strings.Builder{}
	fmt.Fprintln(timer, "[Unit]")
	fmt.Fprintln(timer, "Description=Periodic SSH key refresh from GitHub")
	fmt.Fprintln(timer)
	fmt.Fprintln(timer, "[Timer]")
	fmt.Fprintln(timer, "Persistent=true")
	switch parsed.Kind {
	case intervalSystemdDuration:
		fmt.Fprintln(timer, "OnBootSec=2min")
		fmt.Fprintf(timer, "OnUnitActiveSec=%s\n", parsed.SystemdDuration)
	case intervalSystemdCalendar:
		fmt.Fprintf(timer, "OnCalendar=%s\n", parsed.SystemdCalendar)
	default:
		return "", fmt.Errorf("interval %q is not supported by systemd install", parsed.Original)
	}
	fmt.Fprintln(timer, "RandomizedDelaySec=2min")
	fmt.Fprintln(timer, "Unit=ssh-gh-id.service")
	fmt.Fprintln(timer)
	fmt.Fprintln(timer, "[Install]")
	fmt.Fprintln(timer, "WantedBy=timers.target")
	return timer.String(), nil
}

func (a *App) installSystemdUser(parsed parsedInterval) error {
	service := fmt.Sprintf(`[Unit]
Description=Update SSH authorized_keys from GitHub identities

[Service]
Type=oneshot
ExecStart=%s --update-all
`, shellEscapePathForUnit(a.LocalBinPath))

	timer, err := renderSystemdTimer(parsed)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.SystemdUnitPath, []byte(service), 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(a.SystemdTimerPath, []byte(timer), 0o644); err != nil {
		return err
	}
	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", systemdTimerName},
	}
	for _, parts := range cmds {
		cmd := exec.Command(parts[0], parts[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(parts, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (a *App) installSystemdSystem(parsed parsedInterval) error {
	if err := os.MkdirAll(a.SystemSystemdDir, 0o755); err != nil {
		return err
	}
	service := fmt.Sprintf(`[Unit]
Description=Update SSH authorized_keys from GitHub identities

[Service]
Type=oneshot
ExecStart=%s --update-all
`, shellEscapePathForUnit(a.LocalBinPath))

	timer, err := renderSystemdTimer(parsed)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.SystemSystemdUnitPath, []byte(service), 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(a.SystemSystemdTimerPath, []byte(timer), 0o644); err != nil {
		return err
	}
	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", systemdTimerName},
	}
	for _, parts := range cmds {
		cmd := exec.Command(parts[0], parts[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(parts, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func shellEscapePathForUnit(path string) string {
	return strings.ReplaceAll(path, " ", `\x20`)
}

func (a *App) installCron(parsed parsedInterval) error {
	if parsed.CronSpec == "" {
		return fmt.Errorf("interval %q cannot be converted to cron", parsed.Original)
	}
	existing, err := readCrontab()
	if err != nil {
		return err
	}
	command := fmt.Sprintf("XDG_CONFIG_HOME=%s XDG_DATA_HOME=%s XDG_STATE_HOME=%s %s --update-all >> %s 2>&1",
		shellEscape(filepath.Dir(a.ConfigDir)),
		shellEscape(filepath.Dir(a.DataDir)),
		shellEscape(filepath.Dir(a.StateDir)),
		shellEscape(a.LocalBinPath),
		shellEscape(a.LogPath),
	)
	block := strings.Join([]string{
		cronStartMarker,
		fmt.Sprintf("%s %s", parsed.CronSpec, command),
		cronEndMarker,
	}, "\n") + "\n"
	next, err := replaceManagedCron(existing, block)
	if err != nil {
		return err
	}
	return writeCrontab(next)
}

func replaceManagedCron(current, block string) (string, error) {
	start := strings.Index(current, cronStartMarker)
	end := strings.Index(current, cronEndMarker)
	switch {
	case start == -1 && end == -1:
		if block == "" {
			return current, nil
		}
		if strings.TrimSpace(current) == "" {
			return block, nil
		}
		return strings.TrimRight(current, "\n") + "\n\n" + block, nil
	case start == -1 || end == -1 || end < start:
		return "", errors.New("crontab contains an incomplete ssh-gh-id managed block")
	default:
		end += len(cronEndMarker)
		if end < len(current) && current[end] == '\n' {
			end++
		}
		next := current[:start] + block + current[end:]
		if block == "" {
			next = strings.TrimLeft(next, "\n")
			next = strings.ReplaceAll(next, "\n\n\n", "\n\n")
		}
		return next, nil
	}
}

func readCrontab() (string, error) {
	cmd := exec.Command("crontab", "-l")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "no crontab") {
			return "", nil
		}
	}
	return "", fmt.Errorf("crontab -l: %w: %s", err, strings.TrimSpace(string(out)))
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crontab -: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a *App) uninstallScheduler() error {
	var merr multiError
	if _, err := os.Stat(a.SystemSystemdTimerPath); err == nil && runningAsRoot() {
		for _, parts := range [][]string{{"systemctl", "disable", "--now", systemdTimerName}, {"systemctl", "daemon-reload"}} {
			cmd := exec.Command(parts[0], parts[1:]...)
			out, err := cmd.CombinedOutput()
			if err != nil && !strings.Contains(strings.ToLower(string(out)), "not loaded") {
				merr.add("%s: %v: %s", strings.Join(parts, " "), err, strings.TrimSpace(string(out)))
			}
		}
	}
	for _, path := range []string{a.SystemSystemdTimerPath, a.SystemSystemdUnitPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			merr.add("remove %s: %v", path, err)
		}
	}
	if ok, _ := a.systemdUserUsable(); ok {
		for _, parts := range [][]string{{"systemctl", "--user", "disable", "--now", systemdTimerName}, {"systemctl", "--user", "daemon-reload"}} {
			cmd := exec.Command(parts[0], parts[1:]...)
			out, err := cmd.CombinedOutput()
			if err != nil && !strings.Contains(strings.ToLower(string(out)), "not loaded") {
				merr.add("%s: %v: %s", strings.Join(parts, " "), err, strings.TrimSpace(string(out)))
			}
		}
	}
	for _, path := range []string{a.SystemdTimerPath, a.SystemdUnitPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			merr.add("remove %s: %v", path, err)
		}
	}
	if cron, err := readCrontab(); err == nil {
		next, replErr := replaceManagedCron(cron, "")
		if replErr == nil {
			next = strings.TrimSpace(next)
			if next != "" {
				next += "\n"
			}
			if err := writeCrontab(next); err != nil {
				merr.add("write crontab: %v", err)
			}
		} else if !strings.Contains(replErr.Error(), "incomplete") {
			merr.add("rewrite crontab: %v", replErr)
		}
	}
	return merr.err()
}

func normalizeSchedulerName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return "auto"
	case "cron", "crontab", "user-cron", "cron-user":
		return "crontab"
	case "system", "systemd", "systemd-system":
		return "systemd"
	case "user", "systemd-user":
		return "systemd-user"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func isValidScheduler(name string) bool {
	switch name {
	case "auto", "systemd", "systemd-user", "crontab":
		return true
	default:
		return false
	}
}

func crontabAvailable() bool {
	_, err := exec.LookPath("crontab")
	return err == nil
}

func currentUsernameForHint() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("LOGNAME"); u != "" {
		return u
	}
	if runningAsRoot() {
		return "root"
	}
	return "$USER"
}

func runningAsRoot() bool {
	return os.Geteuid() == 0
}

func (a *App) systemdUserUsable() (bool, error) {
	cmd := exec.Command("systemctl", "--user", "show-environment")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "failed to connect to bus") || strings.Contains(msg, "no medium found") || strings.Contains(msg, "not been booted with systemd") {
			return false, nil
		}
		return false, fmt.Errorf("systemd --user unavailable: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func (a *App) detectScheduler() string {
	if _, err := os.Stat(a.SystemSystemdTimerPath); err == nil {
		return "systemd"
	}
	if _, err := os.Stat(a.SystemdTimerPath); err == nil {
		return "systemd-user"
	}
	if cron, err := readCrontab(); err == nil && strings.Contains(cron, cronStartMarker) {
		return "crontab"
	}
	return "none"
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

func pathContains(binPath string) bool {
	binDir := filepath.Dir(binPath)
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part == binDir {
			return true
		}
	}
	return false
}

func (a *App) ensureLocalBinOnPath() (string, bool, error) {
	profilePath := a.preferredShellProfile()
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return profilePath, false, err
	}
	current, err := os.ReadFile(profilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profilePath, false, err
	}
	binDir := filepath.Dir(a.LocalBinPath)
	exportLine := fmt.Sprintf("export PATH=%s:$PATH", shellEscape(binDir))
	block := strings.Join([]string{
		pathStartMarker,
		exportLine,
		pathEndMarker,
	}, "\n") + "\n"
	updated, changed, err := replaceManagedPathBlock(string(current), block)
	if err != nil {
		return profilePath, false, err
	}
	if !changed {
		return profilePath, false, nil
	}
	if err := writeFileAtomic(profilePath, []byte(updated), 0o644); err != nil {
		return profilePath, false, err
	}
	return profilePath, true, nil
}

func (a *App) preferredShellProfile() string {
	shell := strings.ToLower(filepath.Base(os.Getenv("SHELL")))
	switch shell {
	case "zsh":
		return filepath.Join(a.Home, ".zshrc")
	case "bash":
		return filepath.Join(a.Home, ".bashrc")
	default:
		return filepath.Join(a.Home, ".profile")
	}
}

func replaceManagedPathBlock(current, block string) (string, bool, error) {
	start := strings.Index(current, pathStartMarker)
	end := strings.Index(current, pathEndMarker)
	switch {
	case start == -1 && end == -1:
		if strings.TrimSpace(current) == "" {
			return block, true, nil
		}
		trimmed := strings.TrimRight(current, "\n") + "\n\n" + block
		return trimmed, true, nil
	case start == -1 || end == -1 || end < start:
		return "", false, fmt.Errorf("invalid managed PATH block in shell profile")
	default:
		end += len(pathEndMarker)
		if end < len(current) && current[end] == '\n' {
			end++
		}
		next := current[:start] + block + current[end:]
		if next == current {
			return current, false, nil
		}
		return next, true, nil
	}
}

func usageText() string {
	header := titleText("ssh-gh-id") + " - manage SSH authorized_keys from GitHub user identities\n\n"
	body := strings.Join([]string{
		"Usage:",
		"  ssh-gh-id --add <username>        " + dimText("(-a)"),
		"  ssh-gh-id --del <username>        " + dimText("(-d)"),
		"  ssh-gh-id --list                  " + dimText("(-l)"),
		"  ssh-gh-id --update <username>     " + dimText("(-u)"),
		"  ssh-gh-id --update-all            " + dimText("(-U)"),
		"  ssh-gh-id --set-interval <spec>   " + dimText("(-t)"),
		"  ssh-gh-id --set-scheduler <backend> " + dimText("systemd | systemd-user | crontab | auto"),
		"  ssh-gh-id --install               " + dimText("(-i)"),
		"  ssh-gh-id --uninstall",
		"  ssh-gh-id --status                " + dimText("(-s)"),
		"  ssh-gh-id --version               " + dimText("(-v)"),
		"  ssh-gh-id --help                  " + dimText("(-h)"),
		"",
		"Examples:",
		"  ssh-gh-id -a <username>",
		"  ssh-gh-id -U",
		"  ssh-gh-id -t daily",
		"  ssh-gh-id --set-scheduler crontab",
		"  ssh-gh-id --set-interval '0 */6 * * *'",
		"  ssh-gh-id -i",
	}, "\n") + "\n"
	return header + body
}

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

func addUnique(users *[]string, username string) bool {
	for _, u := range *users {
		if u == username {
			return false
		}
	}
	*users = append(*users, username)
	sort.Strings(*users)
	return true
}

func removeValue(users []string, username string) ([]string, bool) {
	out := make([]string, 0, len(users))
	found := false
	for _, u := range users {
		if u == username {
			found = true
			continue
		}
		out = append(out, u)
	}
	return out, found
}

func contains(users []string, username string) bool {
	for _, u := range users {
		if u == username {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func expandHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func (a *App) resolveAuthorizedKeysPathMust() string {
	cfg, err := a.loadConfig()
	if err == nil && cfg.AuthorizedKeysPath != "" {
		return cfg.AuthorizedKeysPath
	}
	return a.AuthorizedKeysPath
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	return writeFileAtomicWithExistingMode(path, content, mode)
}

func writeFileAtomicWithExistingMode(path string, content []byte, fallbackMode os.FileMode) error {
	mode := fallbackMode
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func writeReaderAtomic(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func readNonEmptyLines(r io.Reader) ([]string, error) {
	s := bufio.NewScanner(r)
	var lines []string
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, s.Err()
}

func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
