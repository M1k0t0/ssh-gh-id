package sshghid

import (
	"net/http"
	"time"
)

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
	ReleaseAPIURL          string
	Now                    func() time.Time
	HTTPClient             *http.Client
}
