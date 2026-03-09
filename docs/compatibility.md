# Compatibility Matrix

## Hardware

| Board | Architecture | Tested | Notes |
|---|---|---|---|
| Raspberry Pi 5 | arm64 | ✅ | Primary target; use `make build-pi` |
| Raspberry Pi 4 Model B | arm64 | ✅ | Same binary as Pi 5 |
| Raspberry Pi 3 Model B/B+ | armv7 | ✅ | Use `make build-pi3` |
| Raspberry Pi Zero 2 W | arm64 | ⚠️ | Runs; limited RAM may affect SQLite under heavy event load |
| Raspberry Pi Zero / 1 | armv6 | ❌ | Not supported; no armv6 build target |
| Generic arm64 SBC (Orange Pi, Rock Pi, etc.) | arm64 | ⚠️ | Should work; not routinely tested |
| x86-64 Linux server | amd64 | ✅ | Use `make build-amd64`; inotify and netlink watchers work on any Linux |

## Operating System

| OS | Version | Status | Notes |
|---|---|---|---|
| Raspberry Pi OS (Bookworm) | 12 (Debian 12) | ✅ | Primary tested OS |
| Raspberry Pi OS (Bullseye) | 11 (Debian 11) | ✅ | Supported |
| Ubuntu Server | 22.04 LTS, 24.04 LTS | ✅ | Tested on arm64 and amd64 |
| Debian | 11, 12 | ✅ | Supported |
| Other systemd Linux | — | ⚠️ | Likely works; not routinely tested |
| macOS | Any | ⚠️ | Build and dev only (`make dev`); Linux-specific watchers silently disabled |
| Windows | Any | ❌ | Not supported |

## Go Runtime

| Go Version | Status |
|---|---|
| 1.22 | ✅ Minimum required |
| 1.23 | ✅ Supported |
| 1.24+ | ✅ Supported (used in CI) |

## Docker (optional)

Docker is **optional**. All watchers except `DockerWatcher` and the `/docker` Telegram bot commands work without it.

| Scenario | Supported |
|---|---|
| No Docker installed | ✅ Set `docker.enabled: false` in config |
| Docker Engine (Linux) | ✅ `DockerWatcher` polls `docker ps` |
| Docker Desktop (macOS) | ⚠️ Dev/test only; `DockerWatcher` works if the socket is accessible |
| Rootless Docker | ⚠️ May work; `docker ps` must be on `PATH` and accessible to the piguard user |
| Podman (docker-compatible CLI) | ⚠️ Untested; may work if `docker` is aliased to `podman` |

## Notification Channels

| Channel | Requirement |
|---|---|
| Telegram | Bot token + chat ID; interactive mode requires the bot to have message permissions |
| ntfy.sh | Public topic (no auth) or private topic with token; self-hosted ntfy supported via `server` config key |
| Discord | Webhook URL |
| Webhook | Any HTTP/HTTPS endpoint accepting POST |

At least one channel must be enabled; `config.Validate()` enforces this at startup.

## Optional System Tools

These tools extend PiGuard's detection capabilities but are not required:

| Tool | Purpose | Watcher |
|---|---|---|
| `iptables` | Firewall drift detection | `FirewallWatcher` |
| `ip` (iproute2) | LAN device discovery via ARP | `NetworkScanWatcher` |
| ClamAV | Malware alerts from scan logs | `SecurityToolsWatcher` |
| rkhunter | Rootkit alerts from scan logs | `SecurityToolsWatcher` |
| Docker | Container lifecycle events | `DockerWatcher` |

## Privileges

PiGuard requires elevated privileges for several watchers:

| Capability | Reason | Minimum |
|---|---|---|
| `CAP_NET_ADMIN` | Open netlink socket (port monitoring) | Or run as root |
| Read `/proc`, `/sys` | System health metrics | Typically available to all users |
| Read `/var/log/clamav`, `/var/log/rkhunter.log` | Security tool log tailing | Read access to log files |
| `iptables -L` | Firewall state polling | `CAP_NET_ADMIN` or sudo |
| Write `/var/lib/piguard/` | SQLite database | Created at startup; requires write permission |

The standard install (`scripts/install.sh`) runs PiGuard as root via systemd, which satisfies all requirements.

---

## See also

- [Documentation Index](README.md)
- [Getting Started](getting-started.md) — installation and first-run guide
- [Architecture](architecture.md) — system design and data flow
