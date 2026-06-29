package sshghid

var version = "0.3.1"

const (
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
	releaseAPIURL    = "https://api.github.com/repos/M1k0t0/ssh-gh-id/releases/latest"
	systemdUnitName  = "ssh-gh-id.service"
	systemdTimerName = "ssh-gh-id.timer"
)

func httpUserAgent() string {
	return appName + "/" + version + " (+https://github.com/)"
}
