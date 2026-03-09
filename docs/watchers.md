# Watcher Reference

## Overview

Watchers are independent goroutines that observe the host and publish events to an in-process event bus. Each watcher implements the `Watcher` interface defined in `internal/watchers/watcher.go`:

- `Name() string` -- returns the watcher's identifier
- `Start(ctx context.Context) error` -- begins observation in a goroutine
- `Stop() error` -- gracefully shuts down

Watchers are enabled or disabled via config. A disabled watcher is never started by the daemon. Some watchers are Linux-only and compile to no-ops on macOS (noted per watcher below).

---

## Watcher Reference

### Port Monitor (NetlinkWatcher)

| | |
|---|---|
| **Detects** | New listening sockets opening or closing in real time |
| **Mechanism** | Linux netlink socket (`SOCK_DIAG`) -- event-driven, not polling |
| **Events** | `port.opened` (Warning if exposed to network, Info if localhost-only), `port.closed` (Info) |
| **Config keys** | `ports.enabled`, `ports.ignore`, `ports.known`, `ports.cooldown` |
| **Platform** | Linux only (no-op on macOS) |

**Example alert:**
> New port 0.0.0.0:8080 (docker-proxy, container: nginx)

---

### Firewall Monitor (FirewallWatcher)

| | |
|---|---|
| **Detects** | iptables chain policy changes, missing expected rules |
| **Mechanism** | Polls `iptables -L` on a configurable interval |
| **Events** | `firewall.changed` (Critical), `firewall.ok` (Info) |
| **Config keys** | `firewall.enabled`, `firewall.chains[]` (each with `table`, `chain`, `expect_policy`, `expect_rule`), `firewall.check_interval` |
| **Platform** | Linux only (requires iptables) |

**Example alert:**
> Firewall chain INPUT policy changed to ACCEPT (expected DROP)

---

### System Health (SystemWatcher)

| | |
|---|---|
| **Detects** | Disk usage, memory usage, or CPU temperature exceeding thresholds; system reboot via uptime check |
| **Mechanism** | Polls `/proc` and `/sys/class/thermal` on interval |
| **Events** | `system.disk_high` (Warning), `system.memory_high` (Warning), `system.temp_high` (Warning), `system.reboot` (Info) |
| **Config keys** | `system.disk_threshold`, `system.memory_threshold`, `system.temperature_threshold` |
| **Platform** | All platforms (CPU temperature reading is Linux-only) |

**Example alert:**
> Disk usage at 92% on / (threshold: 85%)

---

### File Integrity (FileIntegrityWatcher)

| | |
|---|---|
| **Detects** | Changes to critical system files |
| **Mechanism** | Linux inotify -- event-driven file watching |
| **Events** | `file.changed` (Warning or Critical, configurable per path) |
| **Config keys** | `file_integrity.enabled`, `file_integrity.cooldown`, `file_integrity.paths[]` (each with `path`, `description`, `severity`) |
| **Platform** | Linux only (inotify) |

**Default watched paths:** `/etc/passwd`, `/etc/shadow`, `/etc/sudoers`, `/etc/ssh/sshd_config`, `/etc/hosts`, `/etc/crontab`, `/etc/cron.d`

**Example alert:**
> Critical file modified: /etc/shadow (user password database)

---

### Docker Monitor (DockerWatcher)

| | |
|---|---|
| **Detects** | Container lifecycle events -- start, crash (non-zero exit), stop, unhealthy health check, Watchtower image updates |
| **Mechanism** | Polls `docker ps` output on interval |
| **Events** | `docker.container_start` (Info), `docker.container_died` (Critical), `docker.container_stopped` (Info, only if `alert_on_stop`), `docker.container_unhealthy` (Warning), `docker.container_updated` (Info) |
| **Config keys** | `docker.enabled`, `docker.poll_interval`, `docker.alert_on_stop` |
| **Platform** | Requires Docker installed |

**Example alert:**
> Container 'nginx' exited with code 137 (OOMKilled)

---

### Security Tools (SecurityToolsWatcher)

| | |
|---|---|
| **Detects** | Malware and rootkit findings from ClamAV and rkhunter scan logs |
| **Mechanism** | Tails log files, matching `FOUND` lines (ClamAV) and `Warning:` lines (rkhunter) |
| **Events** | `malware.found` (Critical), `rootkit.warning` (Critical) |
| **Config keys** | `security_tools.enabled`, `security_tools.clamav_log`, `security_tools.rkhunter_log`, `security_tools.poll_interval` |
| **Platform** | Linux only (requires ClamAV and/or rkhunter) |

**Example alert:**
> ClamAV: /home/pi/Downloads/sketch.zip: Win.Trojan.Agent FOUND

---

### Network Scanner (NetworkScanWatcher)

| | |
|---|---|
| **Detects** | New or unknown devices appearing on the local network, known devices leaving |
| **Mechanism** | Polls `ip neigh show` (ARP neighbour table) |
| **Events** | `network.new_device` (Warning), `network.device_left` (Info, only if `alert_on_leave`) |
| **Config keys** | `network.enabled`, `network.poll_interval`, `network.alert_on_leave`, `network.ignore_macs` |
| **Platform** | Linux only (requires iproute2) |

**Note:** ARP entries age out naturally. Enabling `alert_on_leave` can produce frequent notifications on busy networks.

**Example alert:**
> Unknown device on LAN: 192.168.1.47 (MAC aa:bb:cc:dd:ee:ff)

---

### Connectivity Monitor (ConnectivityWatcher)

| | |
|---|---|
| **Detects** | Internet connectivity loss and restoration |
| **Mechanism** | Polls TCP dial to configured hosts on interval |
| **Events** | `connectivity.lost` (Critical), `connectivity.restored` (Info, includes outage duration) |
| **Config keys** | `connectivity.enabled`, `connectivity.poll_interval`, `connectivity.hosts` |
| **Platform** | All platforms |

**Example alert:**
> Internet connectivity lost -- all probe hosts unreachable

---

### Auto-Update (AutoUpdateWatcher)

| | |
|---|---|
| **Detects** | Scheduled apt upgrade results, reboot-required state |
| **Mechanism** | Runs `apt-get update && apt-get upgrade -y` on a configurable schedule (day + time); optional auto-reboot with delay |
| **Events** | `system.updated` (Info), `system.update_failed` (Warning) |
| **Config keys** | `auto_update.enabled`, `auto_update.day_of_week`, `auto_update.time`, `auto_update.auto_reboot`, `auto_update.reboot_delay_minutes` |
| **Platform** | Linux only (Debian/Ubuntu, requires apt-get) |

**Example alert:**
> Scheduled upgrade completed: 12 packages updated. Reboot required.

---

### Auth Log Monitor (AuthLogWatcher)

| | |
|---|---|
| **Detects** | SSH brute-force attempts (sliding window detection), failed sudo authentication, successful SSH logins |
| **Mechanism** | Tails `/var/log/auth.log` |
| **Events** | `ssh.bruteforce` (Critical), `sudo.failure` (Warning), `ssh.login` (Info, only if `alert_on_login`) |
| **Config keys** | `auth_log.enabled`, `auth_log.log_path`, `auth_log.poll_interval`, `auth_log.brute_force_threshold`, `auth_log.brute_force_window`, `auth_log.alert_on_login` |
| **Platform** | Linux only (requires rsyslog or equivalent writing auth.log) |

**Example alert:**
> SSH brute-force detected: 15 failed attempts from 203.0.113.42 in 5 minutes

---

### Telegram Bot (TelegramBotWatcher)

| | |
|---|---|
| **Provides** | Interactive two-way bot commands (registered as a watcher, not a notifier) |
| **Mechanism** | Long-polls Telegram Bot API for incoming messages |
| **Config keys** | `notifications.telegram.enabled`, `notifications.telegram.interactive` |
| **Platform** | All platforms |

This watcher does not produce security events itself. It provides a command interface for querying and managing the host remotely. See [telegram-bot.md](telegram-bot.md) for the full command reference.

---

## Event Types Summary

| Event Type | Source Watcher | Default Severity | Description |
|---|---|---|---|
| `port.opened` | Port Monitor | Warning / Info | New listening port detected |
| `port.closed` | Port Monitor | Info | Port closed |
| `firewall.changed` | Firewall | Critical | Firewall policy or rule changed |
| `firewall.ok` | Firewall | Info | Firewall restored to expected state |
| `ssh.bruteforce` | Auth Log | Critical | Brute-force attempt detected |
| `sudo.failure` | Auth Log | Warning | Failed sudo authentication |
| `ssh.login` | Auth Log | Info | Successful SSH login |
| `system.disk_high` | System | Warning | Disk usage above threshold |
| `system.memory_high` | System | Warning | Memory usage above threshold |
| `system.temp_high` | System | Warning | CPU temperature above threshold |
| `system.reboot` | System | Info | System reboot detected |
| `docker.container_died` | Docker | Critical | Container exited with error |
| `docker.container_start` | Docker | Info | Container started |
| `docker.container_unhealthy` | Docker | Warning | Container health check failing |
| `docker.container_stopped` | Docker | Info | Container stopped gracefully |
| `docker.container_updated` | Docker | Info | Container updated (Watchtower) |
| `file.changed` | File Integrity | Warning / Critical | Monitored file modified |
| `malware.found` | Security Tools | Critical | ClamAV malware detection |
| `rootkit.warning` | Security Tools | Critical | rkhunter rootkit warning |
| `network.new_device` | Network | Warning | Unknown device on LAN |
| `network.device_left` | Network | Info | Device left network |
| `connectivity.lost` | Connectivity | Critical | Internet connectivity lost |
| `connectivity.restored` | Connectivity | Info | Connectivity restored |
| `system.updated` | Auto-Update | Info | apt upgrade completed |
| `system.update_failed` | Auto-Update | Warning | apt upgrade failed |
| `summary.daily` | Daemon | Info | Daily summary report |
| `summary.weekly` | Daemon | Info | Weekly trend report |
| `backup.started` | Backup | Info | Backup job started |
| `backup.completed` | Backup | Info | Backup completed successfully |
| `backup.failed` | Backup | Warning | Backup failed |

---

## See also

- [docs/README.md](README.md) -- documentation index
- [docs/getting-started.md](getting-started.md) -- installation and first-run guide
- [docs/architecture.md](architecture.md) -- system architecture and data flow
