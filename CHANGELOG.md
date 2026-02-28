# Changelog

All notable changes to PiGuard are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
PiGuard uses [Semantic Versioning](https://semver.org/).

---

## [0.2.0] — 2026-02-28

### Added
- **DockerWatcher** — polls `docker ps` every 10 seconds and alerts on:
  - Container started / restarted
  - Container crashed (non-zero exit code)
  - Container went unhealthy (requires HEALTHCHECK in Dockerfile)
  - Container stopped gracefully (opt-in via `alert_on_stop: true`)
- New config fields `docker.poll_interval` and `docker.alert_on_stop`
- New event type `docker.container_stopped` for graceful stops

[0.2.0]: https://github.com/Fullex26/piguard/releases/tag/v0.2.0

---

## [0.1.0] — 2026-02-28

First public release.

### Watchers

- **NetlinkWatcher** — real-time port monitoring via `ss` polling with smart diffing;
  detects new/closed listening sockets with process and container labels; IPv6 wildcard
  (`:::port`) correctly flagged as externally exposed
- **FirewallWatcher** — polls iptables chains for policy drift or missing critical rules
- **SystemWatcher** — disk, memory, and CPU temperature threshold alerts with Pi thermal sensor support
- **FileIntegrityWatcher** — inotify-based monitoring of `/etc/passwd`, SSH config, sudoers,
  crontabs, and other critical system files
- **SecurityToolsWatcher** — tails ClamAV and rkhunter logs; fires Critical alerts on
  malware detections (`FOUND`) or rootkit warnings; handles log rotation automatically
- **TelegramBotWatcher** — interactive two-way Telegram bot commands for querying system status

### Notifiers

- **Telegram** — formatted alerts with severity emoji and suggested remediation
- **Discord** — webhook-based alerts with embed formatting
- **ntfy.sh** — lightweight push notifications with priority mapping
- **Webhook** — generic HTTP POST with JSON payload for custom integrations

### CLI

- `piguard run` — start the daemon (foreground; systemd manages it in production)
- `piguard status` — print current port, firewall, and system health snapshot
- `piguard test` — send a test alert to all configured notifiers
- `piguard setup` — interactive wizard: configure notifiers, test credentials, write config
- `piguard version` — print version and build info

### Infrastructure

- Event-driven architecture: watchers → in-process pub/sub bus → deduplicator → notifiers + SQLite
- Cooldown-based deduplication; critical events always bypass cooldown
- All events persisted to SQLite at `/var/lib/piguard/events.db`
- Config at `/etc/piguard/config.yaml` with `${ENV_VAR}` substitution; secrets in `/etc/piguard/env`
- systemd service with `EnvironmentFile` for secret injection
- Install script with architecture detection and checksum verification
- GoReleaser pipeline producing `linux/amd64`, `linux/arm64`, `linux/arm` binaries
- CI: lint (golangci-lint), test, govulncheck, cross-build on every push

[0.1.0]: https://github.com/Fullex26/piguard/releases/tag/v0.1.0
