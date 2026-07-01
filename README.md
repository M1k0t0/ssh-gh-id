# ssh-gh-id

`ssh-gh-id` keeps a managed block inside your `authorized_keys` file in sync with one or more GitHub accounts by fetching `https://github.com/<username>.keys`.

It is intentionally narrow in scope:
- stores a list of GitHub usernames
- fetches each user's published SSH keys from GitHub
- validates fetched and cached SSH public keys before writing them
- rewrites only its own managed block in `authorized_keys`
- schedules periodic refreshes with `systemd` or `crontab`

It does **not** overwrite unmanaged keys outside the managed block.

## Features

- CLI flags for add, delete, list, update, update-all, install, uninstall, self-update, interval changes, scheduler selection, status, help, and version
- XDG-style storage by default
  - config: `~/.config/ssh-gh-id/`
  - data: `~/.local/share/ssh-gh-id/`
  - state/logs: `~/.local/state/ssh-gh-id/`
- atomic writes for config, cache, and `authorized_keys`
- file locking to avoid concurrent runs
- cache-based updates so transient fetch failures do not automatically erase previously known keys
- managed scheduler install with `systemd` for root, and `crontab` or `systemd --user` for non-root users

## Installation

### Go install

```bash
go install github.com/M1k0t0/ssh-gh-id@latest
ssh-gh-id -i
```

### Local checkout

```bash
go build -o ssh-gh-id .
./ssh-gh-id -i
```

When you run `-i` as a non-root user, the install target is derived from the currently running `ssh-gh-id` binary on disk. For example, running `/some/path/ssh-gh-id -i` installs the managed binary at `/some/path/ssh-gh-id`, and the scheduler/PATH helper follow that location.

When you run `-i` as root, the install target is always the fixed root-owned path `/usr/local/bin/ssh-gh-id`. System-level `systemd` units point at that fixed path rather than at the directory where the installer was run.

### One-key install from the latest GitHub Release

Install the latest release with `curl`:

```bash
ARCH="$(uname -m)"; case "$ARCH" in x86_64|amd64) ASSET="ssh-gh-id-linux-amd64" ;; aarch64|arm64) ASSET="ssh-gh-id-linux-arm64" ;; *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;; esac && \
mkdir -p "$HOME/.local/bin" && \
curl -fsSL -o "$HOME/.local/bin/ssh-gh-id" "https://github.com/M1k0t0/ssh-gh-id/releases/latest/download/${ASSET}" && \
chmod +x "$HOME/.local/bin/ssh-gh-id" && \
"$HOME/.local/bin/ssh-gh-id" -i
```

Install the latest release with `wget`:

```bash
ARCH="$(uname -m)"; case "$ARCH" in x86_64|amd64) ASSET="ssh-gh-id-linux-amd64" ;; aarch64|arm64) ASSET="ssh-gh-id-linux-arm64" ;; *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;; esac && \
mkdir -p "$HOME/.local/bin" && \
wget -qO "$HOME/.local/bin/ssh-gh-id" "https://github.com/M1k0t0/ssh-gh-id/releases/latest/download/${ASSET}" && \
chmod +x "$HOME/.local/bin/ssh-gh-id" && \
"$HOME/.local/bin/ssh-gh-id" -i
```

These convenience snippets trust GitHub Releases over HTTPS and execute the downloaded binary. If you need stronger provenance, download a pinned release plus its `.sha256` asset and verify it before running the binary.

If the installed binary directory is not already on your current `PATH`, `ssh-gh-id -i` will add a managed PATH block to your shell profile automatically. Open a new shell or source the profile file to pick it up in the current session.

### Uninstall

```bash
~/.local/bin/ssh-gh-id --uninstall
```

or:

```bash
ssh-gh-id --uninstall
```

Root installs remove `/usr/local/bin/ssh-gh-id`.

## Usage

```bash
ssh-gh-id --add <username>          # or -a <username>
ssh-gh-id --del <username>          # or -d <username>
ssh-gh-id --list                    # or -l
ssh-gh-id --update <username>       # or -u <username>
ssh-gh-id --update-all              # or -U
ssh-gh-id --set-interval <spec>     # or -t <spec>
ssh-gh-id --set-scheduler <backend> # auto | systemd | systemd-user | crontab
ssh-gh-id --install                 # or -i
ssh-gh-id --uninstall
ssh-gh-id --self-update
ssh-gh-id --status                  # or -s
ssh-gh-id --version                 # or -v
ssh-gh-id --help                    # or -h
```

### Common examples

Add a user and immediately fetch their current keys:

```bash
ssh-gh-id -a <username>
```

Refresh every configured user:

```bash
ssh-gh-id -U
```

List configured users:

```bash
ssh-gh-id --list
```

Delete a user and remove their cached keys from the managed block:

```bash
ssh-gh-id -d <username>
```

Set scheduler backend and interval, then reinstall the scheduler:

```bash
ssh-gh-id --set-scheduler crontab
ssh-gh-id --set-interval '@hourly'
ssh-gh-id -i
```

Show status:

```bash
ssh-gh-id --status
```

Update the installed binary to the latest GitHub Release:

```bash
ssh-gh-id --self-update
```

`--self-update` currently supports the Linux release assets published by this project (`linux-amd64` and `linux-arm64`). It downloads the matching binary and `.sha256` asset from the latest GitHub Release, verifies the binary against that checksum, then replaces the currently running executable path. This checksum check catches download mismatch/corruption; it does not protect against a compromised repository, maintainer account, workflow token, release asset, or checksum asset.

## Scheduler behavior

`--install` writes the managed binary and installs a scheduler using the configured interval and backend.

Scheduler backend values:

- `auto` — default. Root uses system-level `systemd`; non-root users prefer `crontab` when available, then fall back to `systemd --user` when usable.
- `systemd` — writes `/etc/systemd/system/ssh-gh-id.service` and `.timer`; requires root.
- `systemd-user` — writes user units under `$XDG_CONFIG_HOME/systemd/user`; may require linger (`loginctl enable-linger <user>`) to keep running after logout.
- `crontab` — writes a managed block to the current user's crontab.

After changing the interval or scheduler backend, rerun `ssh-gh-id -i` so the timer or crontab entry is rewritten.

## Interval formats

`--set-interval` stores the refresh interval used by `--install`.

Supported values:
- `hourly`, `daily`, `weekly`, `monthly`, `yearly`
- `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`
- 5-field cron expressions, for example `0 */6 * * *`
- simple durations for systemd-style timers, for example `12h`, `24h`, `7d`

Examples:

```bash
ssh-gh-id --set-interval daily
ssh-gh-id --set-interval '@hourly'
ssh-gh-id --set-interval '0 */6 * * *'
ssh-gh-id --set-interval 12h
```

Duration-style intervals are best suited for systemd timers. Some simple durations can be converted to cron; others require `systemd`/`systemd-user`.

## Managed block behavior

The tool writes a block like this inside `authorized_keys`:

```text
# >>> ssh-gh-id managed block >>>
# managed by ssh-gh-id; edits outside this block are preserved
# user: alice
ssh-ed25519 AAAA...
# user: bob
ssh-ed25519 BBBB...
# <<< ssh-gh-id managed block <<<
```

Anything outside that block is left alone.

If a user has no cached keys yet, the block contains only a comment for that user until a successful fetch occurs. If `authorized_keys` contains incomplete or duplicate `ssh-gh-id` managed blocks, the tool fails loudly instead of guessing which block is authoritative; remove duplicate/corrupt managed blocks manually and rerun the command.

## Storage layout

By default:

- config: `~/.config/ssh-gh-id/config.json`
- user list: `~/.local/share/ssh-gh-id/users.json`
- per-user key cache: `~/.local/state/ssh-gh-id/cache/*.json`
- status: `~/.local/state/ssh-gh-id/status.json`
- logs: `~/.local/state/ssh-gh-id/logs/ssh-gh-id.log`
- lock file: `~/.local/state/ssh-gh-id/lock`

Useful environment overrides:

- `XDG_CONFIG_HOME`
- `XDG_DATA_HOME`
- `XDG_STATE_HOME`
- `SSH_GH_ID_AUTHORIZED_KEYS_PATH`

## Trust model

This tool trusts GitHub's `.keys` endpoint for every configured username. It does not support custom key-source endpoints from environment variables.

That means:
- if GitHub serves a new valid public key for a configured account, the next refresh can add it
- if GitHub stops serving a key, the next successful refresh can remove it from the managed block
- a compromised GitHub account can publish attacker-controlled keys
- network failures do not automatically delete previously cached keys, because refresh failures fall back to the last cached copy
- fetched and cached key lines are parsed and validated as bare SSH public keys before they are written

You should only configure GitHub accounts you intentionally trust for SSH access.

## Recovery notes

If something looks wrong:

1. Check current status:
   ```bash
   ssh-gh-id --status
   ```
2. Inspect the log:
   ```bash
   tail -n 100 ~/.local/state/ssh-gh-id/logs/ssh-gh-id.log
   ```
3. Force a refresh:
   ```bash
   ssh-gh-id --update-all
   ```
4. If needed, remove only the managed block from `authorized_keys` manually. Unmanaged entries can stay in place.
5. If scheduler configuration is stale, rerun:
   ```bash
   ssh-gh-id --install
   ```

If you want to fully stop automation but keep your user list and cache, run:

```bash
ssh-gh-id --uninstall
```

That removes the scheduler and installed binary, but leaves config/data/state files alone.

## Development

Run the full local check suite:

```bash
make check
```

Useful individual commands:

```bash
make fmt
make test
make vet
make build
```
