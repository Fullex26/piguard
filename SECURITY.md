# Security Policy

## Supported Versions

PiGuard is currently in early release. Security fixes are applied to the latest version only.

| Version | Supported |
|---------|-----------|
| latest  | ✅ |
| older   | ❌ |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

If you discover a security vulnerability, report it privately via GitHub's Security Advisory feature:

1. Go to https://github.com/Fullex26/piguard/security/advisories
2. Click **"Report a vulnerability"**
3. Fill in the details — what the issue is, how to reproduce it, and its potential impact

You can expect an acknowledgement within **72 hours** and a status update within **7 days**.
If the issue is confirmed, a fix will be prepared and released as soon as practical, with credit to
the reporter in the release notes (unless you prefer to remain anonymous).

## Scope

Vulnerabilities worth reporting include:

- Authentication or authorization bypasses in the Telegram bot command handler
- Secret leakage (tokens, credentials) via logs or config files
- Command injection via config values passed to shell commands
- Privilege escalation via the systemd service or install script
- Dependency vulnerabilities with a credible local exploit path

Out of scope:

- Vulnerabilities requiring physical access to the Pi
- Issues in third-party tools PiGuard integrates with (ClamAV, rkhunter, iptables)
- Denial of service against the PiGuard process itself

## Security Design Notes

- PiGuard runs as root (required for netlink and iptables access)
- Secrets are stored in `/etc/piguard/env` (mode 0600, root-only)
- Config uses `${ENV_VAR}` substitution — values are never written to disk in plaintext
- The install script verifies binary checksums before execution
