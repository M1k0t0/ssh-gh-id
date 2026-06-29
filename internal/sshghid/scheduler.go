package sshghid

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
