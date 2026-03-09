# Configuration Reference

PiGuard is configured via a single YAML file with environment variable expansion. This document covers every available configuration option.

## Overview

| Item | Value |
|---|---|
| Default config path | `/etc/piguard/config.yaml` |
| Override flag | `--config PATH` |
| Secrets file | `/etc/piguard/env` (mode 0600, loaded by systemd `EnvironmentFile`) |
| Env expansion | All `${VAR}` placeholders are expanded at load time via `os.ExpandEnv` |
| Validation | At least one notification channel must be enabled or startup fails |
| Defaults | Missing fields are filled from `DefaultConfig()` -- you only need to specify overrides |

## Minimal Config Example

The smallest working configuration -- just Telegram with environment variable placeholders:

```yaml
notifications:
  telegram:
    enabled: true
    bot_token: "${PIGUARD_TELEGRAM_TOKEN}"
    chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"
```

Store the actual token and chat ID in `/etc/piguard/env`:

```
PIGUARD_TELEGRAM_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
PIGUARD_TELEGRAM_CHAT_ID=-1001234567890
```

All other sections use sane defaults (port monitoring, firewall monitoring, Docker monitoring, connectivity checks, file integrity, and system health are all enabled out of the box).

## Full Annotated Config

```yaml
# PiGuard Configuration
# https://github.com/Fullex26/piguard

# -- Notification channels (configure at least one) --
notifications:
  telegram:
    enabled: false
    bot_token: "${PIGUARD_TELEGRAM_TOKEN}"    # From @BotFather
    chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"    # Target chat/group ID
    interactive: true                          # Enable /commands in Telegram

  ntfy:
    enabled: false
    topic: "piguard-alerts"                   # ntfy topic name
    server: "https://ntfy.sh"                 # Self-hosted server supported
    token: ""                                  # Access token (optional, for private topics)

  discord:
    enabled: false
    webhook_url: "${PIGUARD_DISCORD_WEBHOOK}" # Discord webhook URL

  webhook:
    enabled: false
    url: ""                                    # Webhook endpoint URL
    method: "POST"                             # HTTP method

# -- Port monitoring --
ports:
  enabled: true
  ignore:                                      # Address patterns to ignore
    - "127.0.0.1:*"
    - "::1:*"
  known: []                                    # Known ports: list of {addr, label, risk}
  cooldown: "15m"                              # Deduplication cooldown for port events

# -- Firewall monitoring --
firewall:
  enabled: true
  chains:
    - table: "filter"
      chain: "INPUT"
      expect_policy: "DROP"                    # Expected default policy
    - table: "filter"
      chain: "DOCKER-USER"
      # Regex: matches bare DROP rule or ufw-docker-logging-deny jump
      expect_rule: "DROP.*0.0.0.0/0|ufw-docker-logging-deny"
  check_interval: "60s"                        # Polling interval

# -- System health --
system:
  disk_threshold: 80                           # Disk usage % to trigger warning
  memory_threshold: 90                         # Memory usage % to trigger warning
  temperature_threshold: 75                    # CPU temp (C) to trigger warning

# -- Alert behaviour --
alerts:
  min_severity: "warning"                      # Minimum severity: info, warning, critical
  daily_summary: "08:00"                       # Time for daily summary (HH:MM, empty to disable)
  weekly_report: "sunday:20:00"                # Day:HH:MM for weekly report
  quiet_hours:
    start: "23:00"                             # Non-critical alerts suppressed after this time
    end: "07:00"                               # Non-critical alerts resume at this time

# -- Baseline --
baseline:
  mode: "enforcing"                            # Reserved for future use
  learning_duration: "7d"                      # Reserved for future use

# -- Docker --
docker:
  enabled: true
  poll_interval: "10s"                         # Container status polling interval
  alert_on_stop: false                         # Alert on graceful stops (can be noisy)

# -- File integrity monitoring --
file_integrity:
  enabled: true
  cooldown: "5m"                               # Deduplication cooldown
  paths:
    - path: "/etc/passwd"
      description: "User accounts"
      severity: "critical"
    - path: "/etc/shadow"
      description: "Password hashes"
      severity: "critical"
    - path: "/etc/sudoers"
      description: "Sudo rules"
      severity: "critical"
    - path: "/etc/ssh/sshd_config"
      description: "SSH daemon config"
      severity: "critical"
    - path: "/etc/hosts"
      description: "Host resolution"
      severity: "warning"
    - path: "/etc/crontab"
      description: "System cron"
      severity: "warning"
    - path: "/etc/cron.d"
      description: "Cron job directory"
      severity: "warning"

# -- Security tool log monitoring (ClamAV / rkhunter) --
security_tools:
  enabled: false
  clamav_log: "/var/log/clamav/clamav.log"
  rkhunter_log: "/var/log/rkhunter.log"
  poll_interval: "30s"

# -- Network device monitoring --
network:
  enabled: false
  poll_interval: "5m"
  alert_on_leave: false                        # Alert when known devices leave (can be noisy)
  ignore_macs: []                              # MACs to never alert on
  #   - "aa:bb:cc:dd:ee:ff"

# -- Connectivity monitoring --
connectivity:
  enabled: true
  poll_interval: "30s"
  hosts:
    - "8.8.8.8:53"                             # Google DNS
    - "1.1.1.1:53"                             # Cloudflare DNS

# -- Auto-update --
auto_update:
  enabled: false
  day_of_week: "sunday"                        # or "daily" for every day
  time: "03:00"                                # 24-hour format
  auto_reboot: false                           # Reboot when reboot-required exists after upgrade
  reboot_delay_minutes: 5                      # Minutes to wait before rebooting (warning sent first)

# -- Auth log monitoring (SSH brute force / sudo failures) --
auth_log:
  enabled: false
  log_path: "/var/log/auth.log"
  poll_interval: "10s"
  brute_force_threshold: 5                     # Failed attempts before Critical alert
  brute_force_window: "5m"                     # Sliding window for counting attempts
  alert_on_login: false                        # Info alert on successful SSH logins

# -- Backup (reserved for v0.10) --
# backup:
#   enabled: false
#   sources:
#     - "/home"
#     - "/etc"
#     - "/var/lib/piguard"
#   destination: "/mnt/backup/piguard"
#   day_of_week: "daily"
#   time: "02:00"
#   retention: 7
#   rsync_flags: ""

# -- Logging --
logging:
  level: "info"                                # debug, info, warn, error
  file: ""                                     # Log file path (empty = stdout only)
  max_size_mb: 10                              # Log rotation threshold in MB
```

## Section Reference

### notifications.telegram

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable Telegram notifications |
| `bot_token` | string | `""` | Bot API token from @BotFather |
| `chat_id` | string | `""` | Target chat/group ID |
| `interactive` | bool | `true` | Enable two-way bot commands (starts the TelegramBotWatcher) |

When enabled, `bot_token` and `chat_id` are both required or validation fails.

### notifications.ntfy

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable ntfy.sh push notifications |
| `topic` | string | `"piguard-alerts"` | ntfy topic name (required when enabled) |
| `server` | string | `"https://ntfy.sh"` | ntfy server URL (self-hosted supported) |
| `token` | string | `""` | Access token (optional, for private topics) |

### notifications.discord

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable Discord webhook notifications |
| `webhook_url` | string | `""` | Discord webhook URL |

### notifications.webhook

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable generic HTTP webhook |
| `url` | string | `""` | Webhook endpoint URL |
| `method` | string | `"POST"` | HTTP method |

### ports

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable port monitoring |
| `ignore` | []string | `["127.0.0.1:*", "::1:*"]` | Address patterns to ignore (supports `*` wildcard) |
| `known` | []KnownPort | `[]` | Known ports (see below) |
| `cooldown` | string | `"15m"` | Deduplication cooldown for port events |

**KnownPort fields:**

| Field | Type | Description |
|---|---|---|
| `addr` | string | Address in `host:port` format |
| `label` | string | Human-readable label for the port |
| `risk` | string | Risk level annotation |

### firewall

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable firewall monitoring |
| `chains` | []ChainConfig | *(see below)* | Chains to monitor |
| `check_interval` | string | `"60s"` | Polling interval |

**Default chains:**
- `filter/INPUT` with `expect_policy: "DROP"`
- `filter/DOCKER-USER` with `expect_rule: "DROP.*0.0.0.0/0|ufw-docker-logging-deny"`

**ChainConfig fields:**

| Field | Type | Description |
|---|---|---|
| `table` | string | iptables table name (e.g., `"filter"`) |
| `chain` | string | Chain name (e.g., `"INPUT"`) |
| `expect_policy` | string | Expected default policy (e.g., `"DROP"`) |
| `expect_rule` | string | Regex pattern that must match at least one rule in the chain |

Use `expect_policy` or `expect_rule` (or both) per chain entry.

### system

| Field | Type | Default | Description |
|---|---|---|---|
| `disk_threshold` | int | `80` | Disk usage % to trigger warning |
| `memory_threshold` | int | `90` | Memory usage % to trigger warning |
| `temperature_threshold` | int | `75` | CPU temp (degrees C) to trigger warning |

### alerts

| Field | Type | Default | Description |
|---|---|---|---|
| `min_severity` | string | `"warning"` | Minimum severity for notifications (`info`, `warning`, or `critical`) |
| `daily_summary` | string | `"08:00"` | Time for daily summary (HH:MM format, empty string to disable) |
| `weekly_report` | string | `"sunday:20:00"` | Day:HH:MM for weekly report |
| `quiet_hours.start` | string | `"23:00"` | Quiet hours start (non-critical alerts suppressed) |
| `quiet_hours.end` | string | `"07:00"` | Quiet hours end |

### baseline

| Field | Type | Default | Description |
|---|---|---|---|
| `mode` | string | `"enforcing"` | Baseline mode (reserved for future use) |
| `learning_duration` | string | `"7d"` | Learning period duration (reserved for future use) |

### docker

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable Docker container monitoring |
| `poll_interval` | string | `"10s"` | Container status polling interval |
| `alert_on_stop` | bool | `false` | Alert on graceful container stops (can be noisy) |

### file_integrity

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable file integrity monitoring (inotify-based, Linux only) |
| `cooldown` | string | `"5m"` | Deduplication cooldown |
| `paths` | []WatchPath | *(7 default paths)* | Files/directories to watch |

**Default watched paths:**

| Path | Description | Severity |
|---|---|---|
| `/etc/passwd` | User accounts | critical |
| `/etc/shadow` | Password hashes | critical |
| `/etc/sudoers` | Sudo rules | critical |
| `/etc/ssh/sshd_config` | SSH daemon config | critical |
| `/etc/hosts` | Host resolution | warning |
| `/etc/crontab` | System cron | warning |
| `/etc/cron.d` | Cron job directory | warning |

**WatchPath fields:**

| Field | Type | Description |
|---|---|---|
| `path` | string | Absolute path to file or directory |
| `description` | string | Human-readable description |
| `severity` | string | Alert severity: `"warning"` or `"critical"` |

### security_tools

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable ClamAV/rkhunter log monitoring |
| `clamav_log` | string | `"/var/log/clamav/clamav.log"` | ClamAV log path |
| `rkhunter_log` | string | `"/var/log/rkhunter.log"` | rkhunter log path |
| `poll_interval` | string | `"30s"` | Log polling interval |

### network

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable LAN device monitoring (ARP table) |
| `poll_interval` | string | `"5m"` | ARP table polling interval |
| `alert_on_leave` | bool | `false` | Alert when known devices leave the network (can be noisy -- ARP entries age out) |
| `ignore_macs` | []string | `[]` | MAC addresses to never alert on (e.g., your router) |

### connectivity

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable connectivity monitoring |
| `poll_interval` | string | `"30s"` | TCP probe interval |
| `hosts` | []string | `["8.8.8.8:53", "1.1.1.1:53"]` | TCP dial targets (host:port format) |

### auto_update

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable scheduled apt upgrades |
| `day_of_week` | string | `"sunday"` | Day to run (`"daily"` for every day, or a weekday name) |
| `time` | string | `"03:00"` | Time to run (24-hour format) |
| `auto_reboot` | bool | `false` | Auto-reboot when `/var/run/reboot-required` exists after upgrade |
| `reboot_delay_minutes` | int | `5` | Minutes to wait before auto-reboot (warning notification sent first) |

### auth_log

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable auth log monitoring |
| `log_path` | string | `"/var/log/auth.log"` | Auth log file path |
| `poll_interval` | string | `"10s"` | Log polling interval |
| `brute_force_threshold` | int | `5` | Failed SSH attempts before Critical alert |
| `brute_force_window` | string | `"5m"` | Sliding window for counting failed attempts |
| `alert_on_login` | bool | `false` | Send Info alert on successful SSH logins |

### backup (reserved for v0.10)

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable scheduled backups |
| `sources` | []string | `["/home", "/etc", "/var/lib/piguard"]` | Directories to back up |
| `destination` | string | `"/mnt/backup/piguard"` | Backup destination (local path or `user@host:/path`) |
| `day_of_week` | string | `"daily"` | Schedule day (`"daily"` or a weekday name) |
| `time` | string | `"02:00"` | Schedule time (24-hour format) |
| `retention` | int | `7` | Number of date-stamped backups to keep |
| `rsync_flags` | string | `""` | Custom rsync flags (overrides defaults) |

### logging

| Field | Type | Default | Description |
|---|---|---|---|
| `level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `file` | string | `""` | Log file path (empty string = stdout only, no file logging) |
| `max_size_mb` | int | `10` | Log file rotation threshold in MB |

## Environment Variables

| Variable | Used By | Description |
|---|---|---|
| `PIGUARD_TELEGRAM_TOKEN` | `notifications.telegram.bot_token` | Telegram bot API token |
| `PIGUARD_TELEGRAM_CHAT_ID` | `notifications.telegram.chat_id` | Telegram chat/group ID |
| `PIGUARD_DISCORD_WEBHOOK` | `notifications.discord.webhook_url` | Discord webhook URL |
| `PIGUARD_WEBHOOK_URL` | `notifications.webhook.url` | Generic webhook URL |

These variables should be stored in `/etc/piguard/env` (created by `piguard setup`). The systemd service loads this file via `EnvironmentFile=-/etc/piguard/env` before starting PiGuard, making the variables available for `os.ExpandEnv` substitution at config load time.

You can also define custom environment variables and reference them anywhere in the YAML config with `${VAR_NAME}` syntax.

## See Also

- [docs/README.md](README.md) -- Documentation index
- [docs/getting-started.md](getting-started.md) -- Installation and first-run guide
- [docs/architecture.md](architecture.md) -- System architecture and data flow
