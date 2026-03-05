# 🛡️ PiGuard

[![Build](https://img.shields.io/github/actions/workflow/status/Fullex26/piguard/ci.yml?branch=main&style=for-the-badge&label=BUILD)](https://github.com/Fullex26/piguard/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Fullex26/piguard?style=for-the-badge&label=RELEASE)](https://github.com/Fullex26/piguard/releases/latest)
[![Stars](https://img.shields.io/github/stars/Fullex26/piguard?style=for-the-badge&label=STARS)](https://github.com/Fullex26/piguard/stargazers)
[![License](https://img.shields.io/github/license/Fullex26/piguard?style=for-the-badge&label=LICENSE)](LICENSE)

**Lightweight, event-driven host security monitor for Raspberry Pi & ARM SBCs.**

PiGuard watches your Pi in real-time and alerts you the moment something changes — a new port opens, firewall rules drift, or a container goes unhealthy. Alerts go to Telegram, Discord, ntfy.sh, or any webhook.

## Why PiGuard?

| | Wazuh | Bash scripts | **PiGuard** |
|---|---|---|---|
| RAM usage | 8GB+ | 5MB | **~20MB** |
| ARM native | ❌ | ✅ | **✅** |
| Real-time | ✅ | ❌ (cron) | **✅** |
| Docker-aware | Partial | Manual | **✅ Native** |
| Setup time | Hours | Minutes | **30 seconds** |

## Quick Start

```bash
# Install
curl -sSL https://raw.githubusercontent.com/Fullex26/piguard/main/scripts/install.sh | sudo bash

# Configure (set your Telegram bot token)
sudo nano /etc/piguard/config.yaml

# Test
sudo piguard test

# Start
sudo systemctl enable --now piguard
```

## What It Monitors

- **Ports**: Detects new listening sockets in real-time with process + container labels
- **Firewall**: Watches iptables chains for policy changes or missing rules
- **System**: Disk, memory, CPU temperature (Pi thermal sensor)
- **File integrity**: Detects changes to critical system files (`/etc/passwd`, SSH config, sudoers, crontab, etc.)
- **Docker containers**: Alerts on container start, crash (non-zero exit), graceful stop (opt-in), health transitions, and **Watchtower image updates** (detects same-name container restarting with a new image digest); interactive Telegram controls (stop/restart/fix/logs/remove/prune)
- **Storage management**: Telegram `/storage` command — disk usage report, Docker image/volume pruning, apt cache cleanup, all with confirmation guards
- **Network devices**: Detects new/unknown devices on the local network via ARP neighbour table (`ip neigh show`)
- **Security tools**: Tails ClamAV and rkhunter logs — fires Critical alerts on malware detections or rootkit warnings
- **Connectivity**: Polls configurable TCP probe hosts (default: `8.8.8.8:53`, `1.1.1.1:53`) every 30 s; fires Critical alert on outage and Info alert on recovery with outage duration
- **Services dashboard**: Telegram `/services` shows running systemd services plus Docker containers with host port bindings as local access URLs
- **Auto-update**: Scheduled `apt upgrade` with configurable day/time; Telegram `/updates` to check and `/update CONFIRM` to trigger on-demand; alerts on success/failure and reboot-required
- **Auth log monitoring**: Watches `/var/log/auth.log` for SSH brute-force attempts (Critical alert on threshold), failed sudo authentication (Warning), and successful SSH logins (opt-in Info)
- **Quiet hours**: Non-critical notifications suppressed during configurable window (default 23:00–07:00); Critical events always get through
- **Weekly trend reports**: Automatic weekly summary with event breakdown and trend arrows; on-demand via Telegram `/report`
- **Inline keyboard buttons**: Telegram destructive commands (reboot, update, docker prune, etc.) show tappable confirmation buttons
- **Daily summary**: 8am digest with full system status

## Works Best With

PiGuard integrates with the following optional security tools. Install them on your Pi for deeper coverage — PiGuard becomes the single real-time alerting layer for all security signals.

| Tool | What PiGuard alerts on | Install |
|------|------------------------|---------|
| **ClamAV** | Malware detected by a scan (`FOUND` lines in its log) | `sudo apt install clamav clamav-daemon` |
| **rkhunter** | Rootkit or hidden file warnings | `sudo apt install rkhunter` |

After installing, re-run `sudo piguard setup` to enable log monitoring, or set `security_tools.enabled: true` in `/etc/piguard/config.yaml`.

> **Tip:** Schedule regular scans with cron so PiGuard reports findings in real-time as they happen:
> ```
> 0 3 * * * root /usr/bin/clamscan -r /home --quiet --log=/var/log/clamav/clamav.log
> 0 4 * * * root /usr/bin/rkhunter --check --skip-keypress --report-warnings-only
> ```

## Example Alerts

**New port detected:**
```
🟡 PiGuard — Raspberrypi

New listening port: 0.0.0.0:5432 → docker-proxy (container: postgres)
Bound to all interfaces — accessible from network

💡 If this should be local-only, bind to 127.0.0.1 instead of 0.0.0.0
```

**Firewall drift:**
```
🔴 PiGuard — raspberrypi

Firewall policy changed: INPUT is ACCEPT (expected DROP)

💡 Run: sudo iptables -P INPUT DROP
```

## CLI Commands

```bash
piguard run       # Start the daemon
piguard status    # Show current security status
piguard test      # Send test notification
piguard setup     # Interactive setup wizard
piguard doctor    # Check installation health
piguard version   # Print version
```

## Configuration

Config lives at `/etc/piguard/config.yaml`. Environment variables are expanded (e.g. `${PIGUARD_TELEGRAM_TOKEN}`).

See [configs/default.yaml](configs/default.yaml) for all options.

## Building from Source

Requires Go 1.22+.

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard

# Build for current platform
make build

# Show build version derived from latest git tag
make version

# Cross-compile for Pi 5 (ARM64)
make build-pi

# Cross-compile for Pi 3 (ARMv7)
make build-pi3

# Build all targets
make build-all

# Cross-compile + deploy directly to Pi over SSH
make deploy-pi                   # deploys to 'fullexpi' (default)
make deploy-pi PI_HOST=other-pi  # override host
```

## Roadmap

- [x] **v0.1** — Port monitoring, firewall drift, system health, file integrity, ClamAV/rkhunter alerts, Telegram/Discord/ntfy/webhook notifiers
- [x] **v0.2** — Docker container event monitoring (start/stop/crash/unhealthy)
- [x] **v0.3** — Telegram bot Docker control (stop/restart/fix/logs/remove/prune); NetworkScanWatcher (ARP-based new device detection)
- [x] **v0.4** — System storage management via Telegram: Docker image/volume pruning, apt cache cleanup, disk usage reports
- [x] **v0.5** — Services dashboard + connectivity monitoring + diagnostics: Telegram `/services` with Docker port URLs; `ConnectivityWatcher` for internet outage alerts; `piguard doctor` CLI + Telegram `/doctor` for installation health checks; Watchtower update detection; SQLITE_BUSY and dual-stack dedup fixes
- [x] **v0.6** — Auto-update support: scheduled `apt upgrade` with Telegram `/updates` check and `/update CONFIRM` on-demand trigger; reboot-required detection
- [x] **v0.7** — Security hardening + UX polish: SSH/auth log watcher (brute force and failed sudo detection); quiet hours enforcement for non-critical alerts; Telegram inline keyboard buttons replacing CONFIRM text guards; weekly trend reports
- [ ] **v0.8** — Embedded web dashboard
- [ ] **v0.9** — Smart baselines with learning mode
- [ ] **v0.10** — Plugin system, multi-host support, Prometheus metrics
- Far future: Built-in AI agent for intelligent anomaly correlation and natural-language security Q&A

## License

MIT — see [LICENSE](LICENSE).
