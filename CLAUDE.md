# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PiGuard is a lightweight, event-driven host security monitor for Raspberry Pi and ARM SBCs, written in Go 1.24+. It watches for port changes, firewall drift, and system health issues, then sends alerts via Telegram, Discord, ntfy.sh, or webhooks.

## Build Commands

```bash
make build          # Build for current platform → bin/piguard
make build-pi       # Cross-compile for Pi 5 (arm64) → bin/piguard-linux-arm64
make build-pi3      # Cross-compile for Pi 3 (armv7)
make build-amd64    # Cross-compile for x86 Linux
make build-all      # All cross-compile targets

make test           # go test -race ./... -v  (race detector enabled)
make lint           # golangci-lint run ./...  (requires golangci-lint)
make vuln           # govulncheck ./...        (requires govulncheck)
make clean          # Remove bin/

make dev            # go run ./cmd/piguard run --config configs/default.yaml
make install        # Build + install to /usr/local/bin + systemd service
make deploy-pi      # Cross-compile arm64 + scp + SSH restart on 'fullexpi' (PI_HOST=x to override)
```

Cross-compilation works without a C toolchain because `modernc.org/sqlite` is a pure-Go CGo-free SQLite port.

## Architecture

The system is event-driven with an in-process pub/sub bus:

```
Watchers → eventbus.Bus → Daemon subscriber → Deduplicator → Notifiers
                                            ↘ store.Store (SQLite at /var/lib/piguard)
```

**Core interfaces** — adding features means implementing one of these:

- `watchers.Watcher` (`internal/watchers/watcher.go`): `Name() string`, `Start(ctx) error`, `Stop() error`. Each watcher runs in its own goroutine and calls `bus.Publish(event)`.
- `notifiers.Notifier` (`internal/notifiers/notifier.go`): `Name() string`, `Send(event) error`, `SendRaw(msg) error`, `Test() error`.

**Data flow:**
1. Watchers detect changes and publish `models.Event` to the bus
2. Bus dispatches to all subscribers via goroutines (non-blocking)
3. `Daemon.handleEvent` saves every event to SQLite, then checks `Deduplicator.ShouldAlert`
4. If not deduplicated, event is forwarded to all configured Notifiers

**Key packages:**
- `pkg/models` — shared types: `Event`, `Severity`, `EventType`, `PortInfo`, `FirewallState`, `SystemHealth`
- `internal/eventbus` — simple goroutine-dispatched pub/sub (no channels, handlers run concurrently)
- `internal/analysers` — `Deduplicator`: cooldown-based dedup; critical events always bypass
- `internal/store` — SQLite wrapper; db at `/var/lib/piguard/events.db`
- `internal/logging` — `Setup()` configures slog with optional file logging + rotation; `RotatingWriter` supports `TailLines()` for remote viewing
- `internal/config` — YAML config with `os.ExpandEnv` substitution; `DefaultConfigPath = /etc/piguard/config.yaml`
- `internal/doctor` — `piguard doctor` diagnostics: checks prerequisites, config validity, service status
- `internal/setup` — interactive setup wizard: prompts for notifier credentials, writes config YAML + env file

**Watchers currently implemented:**
- `NetlinkWatcher` — real-time port monitoring via Linux netlink
- `FirewallWatcher` — polls iptables chains on an interval
- `SystemWatcher` — disk/memory/CPU temp thresholds
- `FileIntegrityWatcher` — inotify-based monitoring of critical system files (`/etc/passwd`, SSH config, sudoers, crontab, etc.)
- `SecurityToolsWatcher` — tails ClamAV and rkhunter logs; fires Critical alerts on malware/rootkit findings
- `TelegramBotWatcher` — interactive two-way bot commands (registered as a watcher, not a notifier); supports `/docker` (stop/restart/fix/logs/remove/prune), `/storage`, `/services`, `/doctor`, `/updates`, `/update`, `/report`, `/pilog`; inline keyboard confirmations for destructive actions
- `DockerWatcher` — polls `docker ps` for container lifecycle events (start/crash/stop/unhealthy)
- `NetworkScanWatcher` — polls `ip neigh show` for new/departed ARP neighbours; alerts on unknown devices
- `ConnectivityWatcher` — polls TCP hosts on an interval; fires events when connectivity is lost or restored
- `AutoUpdateWatcher` — scheduled `apt-get update && apt-get upgrade -y` on configurable day/time; publishes success/failure events; detects reboot-required
- `AuthLogWatcher` — monitors `/var/log/auth.log` for SSH brute-force attempts (sliding-window detection), failed sudo authentication, and successful SSH logins

## CLI Subcommands

```bash
piguard run [--config PATH] [-v]  # Start daemon (default config: /etc/piguard/config.yaml; -v enables debug logging)
piguard send "message"            # Send a message to Telegram (reads stdin if no arg or arg is '-')
piguard status                    # Show events from last 24 h (reads SQLite directly)
piguard test                      # Fire a test notification to all configured channels
piguard setup [--env-file PATH]   # Interactive wizard — creates config YAML + /etc/piguard/env
piguard doctor                    # Check system prerequisites and configuration health
piguard version                   # Print version string
```

## Config

Config file: `/etc/piguard/config.yaml` (dev: `configs/default.yaml`). Environment variables are expanded at load time (e.g., `${PIGUARD_TELEGRAM_TOKEN}`). At least one notification channel must be enabled or `config.Validate()` returns an error. Logging can be configured via `logging.level` (debug/info/warn/error), `logging.file` (path to log file), and `logging.max_size_mb` (rotation threshold, default 10).

## Adding a New Watcher or Notifier

1. **Watcher**: Create a file in `internal/watchers/`, implement the `Watcher` interface, embed `Base` for `Cfg`/`Bus` access, and register it in `daemon.New()` (`internal/daemon/daemon.go`).
2. **Notifier**: Create a file in `internal/notifiers/`, implement the `Notifier` interface, add config fields to `internal/config/config.go`, and register in `daemon.New()`.

New `EventType` constants belong in `pkg/models/events.go`.

## Testing

Tests use table-driven patterns with `t.Run`. Watchers are tested by injecting a mock `eventbus.Bus` and asserting on published events. Run `make test` (includes `-race`).

## Gotchas

- **Linux-only watchers**: `NetlinkWatcher` and `FileIntegrityWatcher` (inotify) compile to no-ops on non-Linux platforms via `inotify_stub.go` / the `_linux.go` filename suffix. Running `make dev` locally on macOS will omit these watchers silently.
- **`SystemWatcher` platform split**: Disk usage uses the same `init()` override pattern — `statfs_linux.go` registers the real `syscall.Statfs` implementation at startup; on non-Linux the default is a no-op stub.
- **`piguard setup` writes two files**: secrets go to `/etc/piguard/env` (mode 0600, loaded by systemd `EnvironmentFile`); non-secrets (ntfy topic/server) are written directly into the YAML config.

## Deployment

PiGuard runs as a systemd service (`configs/piguard.service`). The install script (`scripts/install.sh`) handles the full setup. Releases are managed by GoReleaser (`.goreleaser.yaml`).

## Release Checklist

**Run these steps at the end of every version / before any push:**

1. **CHANGELOG.md** — add a `## [x.y.z] — YYYY-MM-DD` section above the previous version with `### Added / Fixed / Changed` bullets.
2. **README.md roadmap** — mark the completed version `[x]`; verify upcoming versions still reflect current plans.
3. **README.md "What It Monitors"** — add any new watcher capabilities introduced in this version.
4. **Todo.txt** — review the file; any items that are now captured in the roadmap or completed can be removed. New ideas should be turned into a versioned roadmap entry.
5. **`make test && make build-all`** — all tests pass and all three cross-compile targets build cleanly.
6. **CLAUDE.md** — update `Watchers currently implemented` if a new watcher was added.
7. **Commit** — stage and commit all changes.
8. **Branch + PR** — push to `release/vX.Y.Z` branch and open a PR (`main` is protected; direct pushes are rejected).
9. **After merge** — checkout main, pull, `git tag -a vX.Y.Z -m "vX.Y.Z" && git push --tags`.

Do **not** push or tag without completing this checklist.