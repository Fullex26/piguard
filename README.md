# 🛡️ PiGuard

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
- **Security tools**: Tails ClamAV and rkhunter logs — fires Critical alerts on malware detections or rootkit warnings
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
- [ ] **v0.2** — Docker container event monitoring (start/stop/crash/unhealthy)
- [ ] **v0.3** — Telegram bot commands: list/stop/restart Docker services
- [ ] **v0.4** — System storage management via Telegram: Docker pruning, cache cleanup, disk reporting
- [ ] **v0.5** — Auto-update support: scheduled `apt upgrade`, clean, and status reporting
- [ ] **v0.6** — Embedded web dashboard
- [ ] **v0.7** — Smart baselines with learning mode
- [ ] **v1.0** — Plugin system, multi-host support, Prometheus metrics

## License

MIT — see [LICENSE](LICENSE).
