# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- Harden root installs so system-level installs use `/usr/local/bin/ssh-gh-id`.
- Remove environment-based key endpoint overrides; keys are fetched from GitHub only.
- Validate fetched and cached SSH public keys before writing `authorized_keys`.
- Detect duplicate or incomplete managed `authorized_keys` blocks and fail loudly.
- Make delete operations safer by planning `authorized_keys` changes before removing users/cache.
- Add CI, release workflow hardening, project metadata, and developer Makefile targets.
