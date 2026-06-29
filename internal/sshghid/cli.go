package sshghid

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

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
