# PiGuard Architecture

## Overview

PiGuard is an event-driven security monitor. Independent **Watchers** observe the host and publish events to an in-process **Bus**; the **Daemon** receives every event, persists it to SQLite, deduplicates it, and forwards alerts to one or more **Notifiers**.

```
┌──────────────────────────────────────────────────────────┐
│                        Daemon                            │
│                                                          │
│  ┌─────────────┐      ┌────────────┐                     │
│  │  Watchers   │─────▶│ eventbus   │──────┐              │
│  │             │      │  .Bus      │      │              │
│  │ • Netlink   │      └────────────┘      │ goroutine    │
│  │ • Firewall  │                          ▼              │
│  │ • System    │               ┌─────────────────────┐   │
│  │ • FileInteg │               │   handleEvent()     │   │
│  │ • SecTools  │               │                     │   │
│  │ • Docker    │               │  1. store.SaveEvent │   │
│  │ • Network   │               │  2. dedup.ShouldAlert│  │
│  │ • TgBot     │               │  3. notifiers.Send  │   │
│  └─────────────┘               └─────────────────────┘   │
│                                        │                  │
│                         ┌──────────────┼──────────────┐  │
│                         ▼              ▼              ▼  │
│                    ┌─────────┐  ┌──────────┐  ┌──────┐  │
│                    │Telegram │  │  Discord │  │ ntfy │  │
│                    └─────────┘  └──────────┘  └──────┘  │
│                                                    │      │
│                    ┌───────────────────┐           ▼      │
│                    │  SQLite store     │      ┌────────┐  │
│                    │ /var/lib/piguard/ │      │Webhook │  │
│                    └───────────────────┘      └────────┘  │
└──────────────────────────────────────────────────────────┘
```

---

## Component Details

### Event Bus (`internal/eventbus`)

A minimal in-process pub/sub bus backed by a slice of handler functions and a `sync.RWMutex`.

- **`Subscribe(handler)`** — registers a handler; called once during daemon startup.
- **`Publish(event)`** — dispatches to every registered handler in its own goroutine, so a slow notifier never blocks a watcher.
- There are no channels or queues; if a handler panics it will only crash its own goroutine.

### Watchers (`internal/watchers`)

Each watcher implements:

```go
type Watcher interface {
    Name()        string
    Start(ctx)    error
    Stop()        error
}
```

Watchers are started in separate goroutines by the Daemon and run until `ctx` is cancelled. When a watcher detects a change it calls `bus.Publish(event)`.

| Watcher | Mechanism | Platform |
|---|---|---|
| `NetlinkWatcher` | Linux netlink socket (SOCK_DIAG) — real-time port events | Linux only |
| `FirewallWatcher` | Polls `iptables -L` on an interval | Linux only |
| `SystemWatcher` | Polls `/proc`, `/sys/class/thermal` for disk/mem/CPU temp | All (temp Linux only) |
| `FileIntegrityWatcher` | inotify watches on `/etc/passwd`, SSH config, sudoers, crontab | Linux only |
| `SecurityToolsWatcher` | Tails ClamAV and rkhunter log files | Linux only |
| `DockerWatcher` | Polls `docker ps` output for container lifecycle changes | Optional (requires Docker) |
| `NetworkScanWatcher` | Polls `ip neigh show` (ARP table) for new/departed LAN devices | Linux only |
| `TelegramBotWatcher` | Long-polls Telegram Bot API for interactive commands (`/docker`, etc.) | All |

> **macOS / non-Linux note:** Watchers that use Linux-specific syscalls (`NetlinkWatcher`, `FileIntegrityWatcher`) compile to no-ops via `_linux.go` filename suffixes and `inotify_stub.go`. Running `make dev` locally on macOS silently omits them.

### Event Model (`pkg/models`)

Every event is a `models.Event`:

```go
type Event struct {
    ID        string
    Type      EventType   // e.g. "port.opened", "file.changed"
    Severity  Severity    // Info, Warning, Critical
    Hostname  string
    Timestamp time.Time
    Message   string
    Details   string      // Extended info
    Suggested string      // Suggested remediation
    Source    string      // Which watcher produced this

    // Optional typed payloads
    Port     *PortInfo
    Firewall *FirewallState
    Health   *SystemHealth
}
```

`EventType` constants live in `pkg/models/events.go`. Add new types there when adding a new watcher.

### Deduplicator (`internal/analysers`)

Prevents alert storms. Maintains an in-memory `map[string]time.Time` keyed by a stable dedup key derived from event type + contextual detail (e.g. port address, firewall chain, or message text).

- **First occurrence** of any key always passes through.
- **Subsequent occurrences** within the cooldown window (default: 15 min, configured via `ports.cooldown`) are silently dropped.
- Cleanup runs every hour, removing keys last seen more than `2 × cooldown` ago to prevent unbounded memory growth.
- The dedup key is intentionally coarse — `"port.opened:0.0.0.0:8080"` — so the same condition from different sources does not generate separate alert floods.

### Store (`internal/store`)

SQLite database at `/var/lib/piguard/events.db` via `modernc.org/sqlite` (pure Go, no CGo required).

- **Every** event is saved regardless of dedup outcome — the store is the audit log.
- `piguard status` reads directly from SQLite (no daemon required).
- Events older than 30 days are pruned hourly.

### Notifiers (`internal/notifiers`)

Each notifier implements:

```go
type Notifier interface {
    Name()          string
    Send(event)     error   // Formatted alert
    SendRaw(msg)    error   // Plain HTML/Markdown string
    Test()          error   // Fire a test message
}
```

Notifiers run synchronously inside `handleEvent` — a slow or failing notifier will delay others. Errors are logged but do not crash the daemon.

---

## Startup Sequence

```
1. config.Load()          — parse YAML, expand ${ENV_VARS}
2. store.Open()           — open/create SQLite at /var/lib/piguard/events.db
3. daemon.New()           — register watchers and notifiers from config
4. bus.Subscribe()        — register handleEvent as the single bus subscriber
5. watcher.Start() × N   — launch each watcher in its own goroutine
6. runDailySummary()      — goroutine: fires summary at configured HH:MM
7. runCleanup()           — goroutine: hourly dedup cleanup + store prune
8. SendRaw("started")     — startup notification to all notifiers
9. <block on SIGINT/SIGTERM>
10. cancel() → watcher.Stop() × N → store.Close()
```

---

## Shutdown

On `SIGINT` or `SIGTERM`:

1. Context is cancelled — all `Start(ctx)` loops exit via `ctx.Done()`.
2. `wg.Wait()` drains all watcher goroutines.
3. Each watcher's `Stop()` is called for any additional cleanup (e.g. closing netlink sockets).
4. The SQLite store is closed cleanly.

---

## Failure Modes

| Failure | Behaviour |
|---|---|
| Watcher crashes (`Start` returns error) | Logged with `slog.Error`; other watchers continue unaffected |
| Notifier `Send` fails | Logged; next notifier in the list is still attempted |
| SQLite write fails | Logged; event still goes through dedup and notification |
| Bus handler panics | Only that goroutine dies; the bus continues dispatching to other handlers |
| Config missing / invalid | `config.Load` returns error; daemon exits before starting any watchers |
| `/var/lib/piguard` not writable | `os.MkdirAll` fails; daemon exits at startup with a clear error |
| Deduplicator memory grows | Cleanup goroutine prunes entries every hour; bounded by number of unique event keys |

---

## Adding a Watcher

1. Create `internal/watchers/mywatcher.go` (use `mywatcher_linux.go` if Linux-only).
2. Implement `Watcher` interface; embed `Base` for `Cfg`/`Bus` access.
3. Add any new `EventType` constants to `pkg/models/events.go`.
4. Register in `daemon.New()` (`internal/daemon/daemon.go`).
5. Add config fields to `internal/config/config.go` and `configs/default.yaml`.
6. Write tests alongside the implementation.

## Adding a Notifier

1. Create `internal/notifiers/mynotifier.go`.
2. Implement `Notifier` interface.
3. Add config fields to `internal/config/config.go`.
4. Register in `daemon.New()`.

---

## See also

- [Documentation Index](README.md)
- [Developer Guide](developing.md) — dev setup, testing patterns, extension guides
- [Compatibility Matrix](compatibility.md) — supported hardware, OS, and dependencies
