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
curl -sSL https://raw.githubusercontent.com/fullexpi/piguard/main/scripts/install.sh | sudo bash

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
- **Daily summary**: 8am digest with full system status

## Example Alerts

**New port detected:**
```
🟡 PiGuard — fullexpi

New listening port: 0.0.0.0:5432 → docker-proxy (container: postgres)
Bound to all interfaces — accessible from network

💡 If this should be local-only, bind to 127.0.0.1 instead of 0.0.0.0
```

**Firewall drift:**
```
🔴 PiGuard — fullexpi

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
git clone https://github.com/fullexpi/piguard.git
cd piguard

# Build for current platform
make build

# Cross-compile for Pi 5 (ARM64)
make build-pi

# Cross-compile for Pi 3 (ARMv7)
make build-pi3

# Build all targets
make build-all
```

## Roadmap

- [x] **v0.1** — Event-driven port monitoring, firewall drift, system health, Telegram/ntfy/Discord
- [ ] **v0.2** — File integrity monitoring (inotify)
- [ ] **v0.3** — Docker container events and image age tracking
- [ ] **v0.4** — Embedded web dashboard
- [ ] **v0.5** — Smart baselines with learning mode
- [ ] **v1.0** — Plugin system, multi-host, Prometheus metrics

## License

MIT — see [LICENSE](LICENSE).
