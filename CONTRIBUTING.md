# Contributing

Thanks for helping improve `ssh-gh-id`.

## Development setup

This project is a small Go CLI. Use Go 1.22 or newer.

```bash
git clone https://github.com/M1k0t0/ssh-gh-id.git
cd ssh-gh-id
make check
```

## Before opening a pull request

Run:

```bash
make check
```

This checks formatting, tests, vet, and build output.

## Security-sensitive changes

This tool writes to `authorized_keys`, installs schedulers, and can self-update binaries. Please include tests for changes that touch:

- key fetching, parsing, caching, or rendering
- managed-block replacement
- install/uninstall paths
- systemd or crontab rendering
- self-update and release metadata

Prefer small, reviewable changes and document any trust-model changes in `README.md`.
