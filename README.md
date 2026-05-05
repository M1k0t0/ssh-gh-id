# ssh-gh-id

`ssh-gh-id` keeps a managed block inside your `authorized_keys` file in sync with one or more GitHub accounts by fetching `https://github.com/<username>.keys`.

It is intentionally narrow in scope:
- stores a list of GitHub usernames
- fetches each user's published SSH keys
- rewrites only its own managed block in `authorized_keys`
- schedules periodic refreshes with `systemd --user` when available, otherwise `crontab`

It does **not** overwrite unmanaged keys outside the managed block.

## Features

- CLI flags for add, delete, list, update, update-all, install, uninstall, interval changes, status, help, and version
- XDG-style storage by default
  - config: `~/.config/ssh-gh-id/`
  - data: `~/.local/share/ssh-gh-id/`
  - state/logs: `~/.local/state/ssh-gh-id/`
- atomic writes for config, cache, and `authorized_keys`
- file locking to avoid concurrent runs
- cache-based updates so transient fetch failures do not automatically erase previously known keys
- managed scheduler install with `systemd --user` preferred, `crontab` fallback

## Installation

### Local checkout

```bash
go build -o ssh-gh-id .
./ssh-gh-id -i
```

When you run `-i`, the install target directory is derived from the currently running `ssh-gh-id` binary on disk. In other words, if you run `/some/path/ssh-gh-id -i`, the managed install target becomes `/some/path/ssh-gh-id`. The scheduler and PATH helper follow that location.

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

If the installed binary directory is not already on your current `PATH`, `ssh-gh-id -i` will add a managed PATH block to your shell profile automatically. Open a new shell or source the profile file to pick it up in the current session.

### Uninstall

```bash
~/.local/bin/ssh-gh-id --uninstall
```

or:

```bash
ssh-gh-id --uninstall
```

## Usage

```bash
ssh-gh-id --add <username>        # or -a <username>
ssh-gh-id --del <username>        # or -d <username>
ssh-gh-id --list                  # or -l
ssh-gh-id --update <username>     # or -u <username>
ssh-gh-id --update-all            # or -U
ssh-gh-id --set-interval <spec>   # or -t <spec>
ssh-gh-id --install               # or -i
ssh-gh-id --uninstall
ssh-gh-id --status                # or -s
ssh-gh-id --version               # or -v
ssh-gh-id --help                  # or -h
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

Show status:

```bash
ssh-gh-id --status
```

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

If a scheduler is already installed, rerun `--install` after changing the interval so the timer or crontab entry is rewritten.

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

If a user has no cached keys yet, the block contains only a comment for that user until a successful fetch occurs.

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
- `SSH_GH_ID_KEYS_BASE_URL` (primarily for testing)

## Trust model

This tool trusts GitHub's `.keys` endpoint for every configured username.

That means:
- if GitHub serves a new key for a configured account, the next refresh can add it
- if GitHub stops serving a key, the next successful refresh can remove it from the managed block
- a compromised GitHub account can publish attacker-controlled keys
- network failures do not automatically delete previously cached keys, because refresh failures fall back to the last cached copy

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

Run tests and build locally:

```bash
go test ./...
go build ./...
```
