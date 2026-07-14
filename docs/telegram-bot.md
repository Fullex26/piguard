# Telegram Bot Command Reference

## Overview

PiGuard includes an optional interactive Telegram bot for read-only status and diagnostic commands.

Outbound-only alerts are the secure default. Interactive mode must be enabled explicitly,
and remote package, reboot, Docker mutation, cleanup, and backup actions remain disabled.

- Requires `notifications.telegram.enabled: true` and `notifications.telegram.interactive: true` in your config
- The Telegram bot is implemented as a watcher (`TelegramBotWatcher`), not a notifier — it runs in its own goroutine and publishes events to the bus like any other watcher
- Commands are slash-prefixed and case-insensitive
- Commands that mutate the host, containers, update schedule, or backups are disabled
- The bot long-polls the Telegram Bot API for incoming messages

## Command Reference

### System

| Command | Aliases | Description |
|---|---|---|
| `/status` | | Full system overview (disk, memory, temp, uptime, containers, ports, firewall) |
| `/disk` | | Storage usage per filesystem |
| `/memory` | `/mem`, `/ram` | RAM usage breakdown |
| `/temp` | `/temperature` | CPU temperature reading |
| `/uptime` | | System uptime |
| `/ip` | | Network interface addresses |

### Security

| Command | Aliases | Description |
|---|---|---|
| `/ports` | | Listening ports with process names and labels |
| `/firewall` | `/fw` | iptables rule check against expected policies |
| `/events` | `/logs` | Recent security events from SQLite store |
| `/scan` | | Trigger ClamAV/rkhunter security scan |

### Docker

| Command | Description |
|---|---|
| `/docker` | Container status overview |
| `/docker logs <name>` | Show last 20 log lines |

`/containers` is an alias for `/docker`.

### Services

| Command | Description |
|---|---|
| `/services` | Show running systemd services plus Docker containers with host port bindings as local access URLs |

### Storage

| Command | Description |
|---|---|
| `/storage` | Disk usage + Docker space report |
Storage cleanup subcommands are disabled. Perform maintenance through an authenticated local or SSH session.

### Updates

| Command | Aliases | Description |
|---|---|---|
| `/updates` | `/upgrades` | Check available package upgrades |
On-demand package upgrades and Telegram changes to the automatic update schedule are disabled.

### Diagnostics

| Command | Description |
|---|---|
| `/pilog` | Tail PiGuard's own log file (last 30 lines) |
| `/doctor` | Run PiGuard installation health checks |

### Reports

| Command | Description |
|---|---|
| `/report` | On-demand weekly trend report (events this week vs last week) |

### Help

| Command | Description |
|---|---|
| `/start` | Welcome message |
| `/help` | Full command list |

## Mutation Policy

Telegram cannot reboot the host, install packages, change the automatic update schedule,
modify or prune containers, clean storage, or start backups. These operations require an
authenticated local or SSH session. Scheduled updates and backups remain controlled by
the local configuration.

## Automatic Messages

PiGuard sends these messages automatically (not in response to commands):

- **Startup notification** — When the daemon starts, includes version, watcher count, and notifier count
- **Daily summary** — At the configured time (default 08:00), a system health snapshot
- **Weekly report** — At the configured time (default Sunday 20:00), event trends compared to the previous week
- **Security alerts** — Real-time alerts from all enabled watchers (ports, firewall, Docker, file integrity, etc.)

## See also

- [Documentation index](README.md)
- [Notifiers configuration](notifiers.md)
- [Configuration reference](configuration.md)
