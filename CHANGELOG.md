# Changelog

All notable changes to PiGuard are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
PiGuard uses [Semantic Versioning](https://semver.org/).

---

## [0.8.0] — 2026-03-06

### Added
- **`piguard send` CLI command** — send arbitrary messages to Telegram from the command line or scripts; supports positional args (`piguard send "msg"`), stdin piping (`echo "msg" | piguard send`), and explicit stdin (`piguard send -`); validates Telegram is enabled without starting the daemon or opening SQLite
- **Persistent file logging** — new `logging` config section with `level` (debug/info/warn/error), `file` (path), and `max_size_mb` (rotation threshold, default 10 MB)
  - Logs written to both stderr (journald) and the configured file via `io.MultiWriter`
  - `RotatingWriter` with mutex-protected writes and single-backup rotation
  - `TailLines(n)` method for remote viewing
- **`--verbose` / `-v` flag** on `piguard run` — overrides log level to debug regardless of config
- **Telegram `/pilog` command** — tails last 30 lines of the log file; output in `<pre>` tags, truncated to Telegram's 4096-char limit; reports "not configured" if file logging is disabled

### Changed
- **SystemWatcher** refactored with injectable `readMemInfo`, `readCPUTemp`, `statfsFunc` functions for testability
- **FirewallWatcher** refactored with injectable `execIptables` function
- **PortLabeller** refactored with injectable `readProcessName`, `resolveContainer` functions
- **NetlinkWatcher** refactored with injectable `runSS` function

### Fixed
- **`/updates` command broken** — was calling `apt-get list --upgradable` which is not a valid `apt-get` subcommand (`list` belongs to `apt`, not `apt-get`); changed to `apt list --upgradable`; error message now includes the actual command output for debugging
- **`/upgrades` alias added** — users typing `/upgrades` instead of `/updates` now get routed correctly instead of "Unknown command"
- **`GetSystemHealth` nil panic** — the daily summary helper created a raw `SystemWatcher{}` struct literal, leaving the new injectable function fields as `nil`; now uses `NewSystemWatcher()` constructor to ensure all fields are initialised

### Tests
- ~74 new test functions across 8 files (3 new test files, 5 expanded)
- New: `system_test.go` (16), `portlabel_test.go` (7), `logging_test.go` (10)
- Expanded: `daemon_test.go` (+7), `firewall_test.go` (+14), `netlink_test.go` (+4), `telegram_bot_test.go` (+9), `events_test.go` (+7)
- All tests pass with `-race` on all 11 packages

[0.8.0]: https://github.com/Fullex26/piguard/releases/tag/v0.8.0

---

## [0.7.0] — 2026-03-05

### Added
- **AuthLogWatcher** — monitors `/var/log/auth.log` for SSH brute-force attempts, failed sudo authentication, and (optionally) successful SSH logins
  - Sliding-window brute force detection: fires `ssh.bruteforce` (Critical) when failed login count per IP exceeds configurable threshold (default: 5 in 5 min)
  - Publishes `sudo.failure` (Warning) on sudo authentication failures
  - Publishes `ssh.login` (Info) on successful SSH logins (opt-in via `alert_on_login: true`)
  - New config section `auth_log` with `enabled`, `log_path`, `poll_interval`, `brute_force_threshold`, `brute_force_window`, `alert_on_login`
  - `piguard doctor` checks log file existence when enabled
- **Quiet hours enforcement** — non-critical notifications are suppressed during the configured quiet window (default: 23:00–07:00); Critical events always get through; events are still saved to SQLite
- **Weekly trend reports** — automatic weekly summary sent on configurable day/time (default: Sunday 20:00) comparing this week's event counts vs last week with trend arrows (↑↓→)
  - New config field `alerts.weekly_report` (e.g. `"sunday:20:00"`)
  - Telegram `/report` command for on-demand weekly summary
  - New store method `GetEventCountByType(days)` for grouped event queries
- **Telegram inline keyboard buttons** — destructive commands now show tappable confirmation buttons instead of requiring `CONFIRM` text:
  - `/reboot`, `/update`, `/docker remove`, `/docker prune`, `/storage images`, `/storage volumes`, `/storage apt`, `/storage all`
  - Text-based CONFIRM still works for backward compatibility
  - Bot now handles Telegram callback queries alongside regular messages

[0.7.0]: https://github.com/Fullex26/piguard/releases/tag/v0.7.0

---

## [0.6.0] — 2026-03-05

### Added
- **AutoUpdateWatcher** — scheduled `apt-get update && apt-get upgrade -y` with configurable day-of-week and time (default: Sunday 03:00, disabled by default)
  - Publishes `system.updated` (Info) on success with package count; escalates to Warning if `/var/run/reboot-required` exists
  - Publishes `system.update_failed` (Warning) on apt errors
  - New config section `auto_update` with `enabled`, `day_of_week` (or `"daily"`), and `time`
- **Telegram `/updates`** — check available package upgrades without applying them
- **Telegram `/update CONFIRM`** — trigger an on-demand system upgrade from your phone
- **`piguard doctor`** now checks `apt-get` availability when auto-update is enabled

[0.6.0]: https://github.com/Fullex26/piguard/releases/tag/v0.6.0

---

## [0.5.0] — 2026-03-05

### Added
- **ConnectivityWatcher** — polls configurable TCP probe hosts (default: `8.8.8.8:53`, `1.1.1.1:53`) every 30 s and fires:
  - `connectivity.lost` (Critical) when all probe hosts are unreachable; bypasses deduplication on first occurrence
  - `connectivity.restored` (Info) when any host becomes reachable again, including outage duration
  - No ICMP/root required — uses plain `net.DialTimeout("tcp", ...)` so it works on all platforms
  - New config section `connectivity` with `enabled`, `poll_interval`, and `hosts`
- **Enhanced `/services` command** — Telegram bot `/services` now appends a Docker section showing running containers with host port bindings formatted as local access URLs (e.g. `:8080 → http://192.168.1.100:8080`)
- **`piguard doctor`** — new CLI command that checks installation health: config, daemon, event store, and dependencies (ss, iptables, docker, rkhunter, ClamAV, ip). Exits non-zero if any check fails. Disabled-watcher checks are automatically skipped.
- **Telegram `/doctor`** — same health report via the bot; renders as HTML with fix commands in `<code>` blocks
- **Watchtower container update alerts** — `DockerWatcher` detects when Watchtower replaces a container with a new image digest and fires `docker.container_updated` (Info) instead of a generic start alert

### Fixed
- **SQLITE_BUSY locking** — concurrent `SaveEvent` calls under burst event loads no longer race on SQLite's single-writer lock. Fix: `db.SetMaxOpenConns(1)` serialises writes; `_busy_timeout` increased to 30 s
- **Dual-stack port alert duplicates** — Docker containers binding to both `0.0.0.0:PORT` and `:::PORT` no longer fire two separate alerts; deduplicator normalises keys to `(port, process)`
- **rkhunter `/scan` permission error** — surfaces `sudo chmod 666 /var/log/rkhunter.log` instead of a generic scan error

[0.5.0]: https://github.com/Fullex26/piguard/releases/tag/v0.5.0

---

## [0.4.0] — 2026-03-03

### Added
- **Telegram storage management** — `/storage` command tree for reclaiming disk space:
  - `/storage` — disk usage report: root filesystem + Docker layer/image/volume/build-cache breakdown (`docker system df`)
  - `/storage images CONFIRM` — `docker image prune -af`; removes all unused images and reports space reclaimed
  - `/storage volumes CONFIRM` — `docker volume prune -f`; removes unused volumes
  - `/storage apt CONFIRM` — `apt-get clean && apt-get autoremove -y`; clears the apt package cache
  - `/storage all CONFIRM` — runs all three pruning operations in sequence
- Updated `/help` command with **Storage** section listing all `/storage` subcommands

[0.4.0]: https://github.com/Fullex26/piguard/releases/tag/v0.4.0

---

## [0.3.0] — 2026-02-28

### Added
- **Telegram Docker control** — interactive subcommands under `/docker`:
  - `/docker stop <name>` — stop a container
  - `/docker restart <name>` — restart a container
  - `/docker fix <name>` — restart an unhealthy/exited container (UX alias for restart)
  - `/docker logs <name>` — show last 20 lines of container logs
  - `/docker remove <name> CONFIRM` — force-remove a container (requires confirmation)
  - `/docker prune CONFIRM` — `docker system prune -f` (requires confirmation)
  - `/docker` (no args) — unchanged; still lists running containers
- **NetworkScanWatcher** — polls `ip neigh show` (ARP neighbour table) every 5 minutes for unknown devices; alerts when a new MAC appears on the LAN
- New config section `network` with `enabled`, `poll_interval`, `alert_on_leave`, and `ignore_macs`
- New event types `network.new_device` and `network.device_left`
- Updated `/help` command with full Docker subcommand reference

[0.3.0]: https://github.com/Fullex26/piguard/releases/tag/v0.3.0

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
