# Security Policy

## Reporting a vulnerability

If you find a security issue in `ssh-gh-id`, please do not publish exploit details before maintainers have had time to respond.

Report privately by opening a GitHub Security Advisory for the repository, or by contacting the maintainer through the contact information available on the GitHub repository.

Please include:

- affected version or commit
- operating system and scheduler backend, if relevant
- reproduction steps
- impact and whether the issue can modify `authorized_keys`, scheduler state, or installed binaries
- any suggested mitigation

## Scope

Security-sensitive areas include:

- GitHub key fetching and validation
- cache persistence
- `authorized_keys` managed-block updates
- root/systemd installation paths
- crontab and systemd unit rendering
- self-update and release artifact verification

## Trust model summary

`ssh-gh-id` trusts GitHub's `.keys` endpoint for configured usernames. It validates fetched and cached lines as bare SSH public keys before writing them, but a compromised trusted GitHub account can still publish attacker-controlled keys.

Release checksums protect against download mismatch/corruption, not against compromised release assets or maintainer credentials.
