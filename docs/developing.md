# Developer Guide

This guide covers everything you need to contribute to PiGuard.

## Dev Setup

### Prerequisites

- Go 1.24+ — `go version`
- `golangci-lint` — `brew install golangci-lint` (or via go install)
- `govulncheck` — `go install golang.org/x/vuln/cmd/govulncheck@latest`
- No C toolchain needed (pure-Go SQLite via `modernc.org/sqlite`)

### Clone and Build

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard
make build    # → bin/piguard
make test     # Run tests with race detector
make lint     # Run golangci-lint
make vuln     # Run govulncheck
```

## Running Locally

```bash
make dev      # go run ./cmd/piguard run --config configs/default.yaml
```

**macOS limitations**: Linux-only watchers (`NetlinkWatcher`, `FileIntegrityWatcher`, `FirewallWatcher`, etc.) compile to no-ops via `_linux.go` filename suffixes and `inotify_stub.go`. Running `make dev` on macOS silently omits these watchers. The daemon still starts with platform-independent watchers (SystemWatcher, DockerWatcher, ConnectivityWatcher, TelegramBotWatcher).

## Project Structure

```
cmd/piguard/            CLI entry point (cobra commands)
internal/
  analysers/            Deduplicator (cooldown-based event dedup)
  config/               YAML config types, loading, validation, defaults
  daemon/               Main daemon: watcher/notifier registration, event handling, schedulers
  doctor/               piguard doctor: health checks and rendering
  eventbus/             In-process pub/sub bus
  logging/              slog setup, rotating file writer
  notifiers/            Telegram, Discord, ntfy, Webhook implementations
  setup/                Interactive setup wizard
  store/                SQLite event store
  watchers/             All watcher implementations
pkg/models/             Shared types: Event, Severity, EventType, PortInfo, FirewallState, SystemHealth
configs/                default.yaml, piguard.service
scripts/                install.sh
docs/                   Documentation
```

## Testing

```bash
make test     # go test -race ./... -v
```

Tests use **table-driven patterns** with `t.Run`. Watchers are tested by injecting a mock `eventbus.Bus` and asserting on published events.

Test file naming: `foo_test.go` alongside `foo.go`.

Example pattern from doctor tests:

```go
func TestCheckConfig(t *testing.T) {
    tests := []struct {
        name   string
        cfg    *config.Config
        expect doctor.Status
    }{
        {"valid config", validConfig(), doctor.StatusOK},
        {"nil config", nil, doctor.StatusFail},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            r := doctor.New(tt.cfg, "/tmp/test.db")
            // ... assertions
        })
    }
}
```

## Adding a Watcher

1. Create `internal/watchers/mywatcher.go` (or `mywatcher_linux.go` for Linux-only)
2. Implement the `Watcher` interface:

```go
type Watcher interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
}
```

3. Embed `Base` struct for `Cfg`/`Bus` access:

```go
type MyWatcher struct {
    watchers.Base
}

func NewMyWatcher(cfg *config.Config, bus *eventbus.Bus) *MyWatcher {
    return &MyWatcher{Base: watchers.Base{Cfg: cfg, Bus: bus}}
}
```

4. Add new `EventType` constants to `pkg/models/events.go`
5. Add config fields to `internal/config/config.go` (new struct + field in Config) and defaults to `DefaultConfig()`
6. Add config section to `configs/default.yaml`
7. Register in `daemon.New()` (`internal/daemon/daemon.go`):

```go
if cfg.MyFeature.Enabled {
    d.watchers = append(d.watchers, watchers.NewMyWatcher(cfg, bus))
}
```

8. Write tests in `internal/watchers/mywatcher_test.go`
9. If Linux-only, create a stub file `mywatcher_stub.go` with build tag or use `_linux.go` suffix

## Adding a Notifier

1. Create `internal/notifiers/mynotifier.go`
2. Implement the `Notifier` interface:

```go
type Notifier interface {
    Name() string
    Send(event models.Event) error
    SendRaw(message string) error
    Test() error
}
```

3. Add config fields to `internal/config/config.go`
4. Register in `daemon.New()`:

```go
if cfg.Notifications.MyNotifier.Enabled {
    d.notifiers = append(d.notifiers, notifiers.NewMyNotifier(cfg.Notifications.MyNotifier))
}
```

5. Add to `config.Validate()` hasNotifier check
6. Write tests

## Adding a Telegram Bot Command

1. Open `internal/watchers/telegram_bot.go`
2. Add a case in `handleCommand()`:

```go
case "/mycommand":
    reply = w.cmdMyCommand()
```

3. Implement `cmdMyCommand()` method
4. Add to `cmdHelp()` help text
5. For destructive commands, require CONFIRM:

```go
case "/mycommand":
    reply = w.cmdMyCommand(parts)
// ...
func (w *TelegramBotWatcher) cmdMyCommand(parts []string) string {
    if len(parts) < 2 || parts[1] != "CONFIRM" {
        return "This is destructive. Send /mycommand CONFIRM to proceed."
    }
    // ... do the thing
}
```

## Cross-Compilation

```bash
make build-pi       # arm64 → bin/piguard-linux-arm64
make build-pi3      # armv7 → bin/piguard-linux-armv7
make build-amd64    # x86   → bin/piguard-linux-amd64
make build-all      # All targets
```

Deploy to Pi over SSH:

```bash
make deploy-pi                    # Default host: 'fullexpi'
PI_HOST=mypi make deploy-pi       # Custom host
```

## Debugging

- Run with verbose logging: `piguard run -v` (sets log level to debug)
- File logging: Configure `logging.file` and `logging.max_size_mb` in config
- View service logs: `journalctl -u piguard -f`
- Inspect SQLite directly: `sqlite3 /var/lib/piguard/events.db "SELECT * FROM events ORDER BY timestamp DESC LIMIT 10;"`

## Release Process

Releases are managed by GoReleaser (`.goreleaser.yaml`). Tag-based: pushing a tag triggers CI to build binaries, generate checksums, and create a GitHub release.

```bash
git tag -a v0.9.1 -m "v0.9.1"
git push --tags
```

See the Release Checklist in CLAUDE.md for the full process.

---

## See also

- [Documentation index](README.md)
- [Architecture](architecture.md)
- [Compatibility](compatibility.md)
