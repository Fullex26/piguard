# Telegram Bot Command Reference

## Overview

PiGuard includes an interactive Telegram bot that lets you monitor and manage your Pi remotely through chat commands.

- Requires `notifications.telegram.enabled: true` and `notifications.telegram.interactive: true` in your config
- The Telegram bot is implemented as a watcher (`TelegramBotWatcher`), not a notifier — it runs in its own goroutine and publishes events to the bus like any other watcher
- Commands are slash-prefixed and case-insensitive
- Destructive commands require the `CONFIRM` keyword or inline keyboard confirmation before execution
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
| `/docker stop <name>` | Stop a running container |
| `/docker restart <name>` | Restart a container |
| `/docker fix <name>` | Restart an unhealthy or exited container |
| `/docker logs <name>` | Show last 20 log lines |
| `/docker remove <name> CONFIRM` | Force-remove a container (destructive) |
| `/docker prune CONFIRM` | Remove all stopped containers (destructive) |

`/containers` is an alias for `/docker`.

### Services

| Command | Description |
|---|---|
| `/services` | Show running systemd services plus Docker containers with host port bindings as local access URLs |

### Storage

| Command | Description |
|---|---|
| `/storage` | Disk usage + Docker space report |
| `/storage images CONFIRM` | Prune unused Docker images |
| `/storage volumes CONFIRM` | Prune unused Docker volumes |
| `/storage apt CONFIRM` | Clean apt package cache |
| `/storage all CONFIRM` | Run all pruning operations |

### Updates

| Command | Aliases | Description |
|---|---|---|
| `/updates` | `/upgrades` | Check available package upgrades |
| `/update CONFIRM` | | Run apt upgrade immediately |

### Diagnostics

| Command | Description |
|---|---|
| `/pilog` | Tail PiGuard's own log file (last 30 lines) |
| `/doctor` | Run PiGuard installation health checks |

### Reports

| Command | Description |
|---|---|
| `/report` | On-demand weekly trend report (events this week vs last week) |

### Danger Zone

| Command | Description |
|---|---|
| `/reboot CONFIRM` | Reboot the Pi |

### Help

| Command | Description |
|---|---|
| `/start` | Welcome message |
| `/help` | Full command list |

## Confirmation Behavior

Destructive commands require the `CONFIRM` keyword appended to the command text. These include:

- `/docker remove <name> CONFIRM`
- `/docker prune CONFIRM`
- `/storage images CONFIRM`, `/storage volumes CONFIRM`, `/storage apt CONFIRM`, `/storage all CONFIRM`
- `/update CONFIRM`
- `/reboot CONFIRM`

Without `CONFIRM`, the bot responds with a warning message and instructions on how to proceed. This prevents accidental execution of dangerous operations.

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
