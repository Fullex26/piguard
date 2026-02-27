# Contributing to PiGuard

Thanks for your interest! PiGuard is a small project and contributions are welcome.

## Development Setup

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard

# Requires Go 1.22+
go version

make build   # builds bin/piguard for your current platform
make test    # runs go test ./... -v
make dev     # runs the daemon directly with configs/default.yaml
```

Cross-compilation (no C toolchain needed — sqlite is pure Go):

```bash
make build-pi     # arm64 (Pi 4/5)
make build-pi3    # armv7 (Pi 3)
make build-amd64  # x86 Linux
make build-all    # all three
```

## Project Structure

| Path | Purpose |
|------|---------|
| `cmd/piguard/` | CLI entry point and commands |
| `internal/watchers/` | Watcher implementations (port, firewall, file, etc.) |
| `internal/notifiers/` | Notifier implementations (Telegram, Discord, ntfy, webhook) |
| `internal/eventbus/` | In-process pub/sub bus |
| `internal/daemon/` | Wires watchers + notifiers together |
| `pkg/models/` | Shared types (Event, Severity, EventType, etc.) |

## Adding a Watcher

1. Create `internal/watchers/mywatcher.go`
2. Implement the `Watcher` interface: `Name() string`, `Start(ctx) error`, `Stop() error`
3. Embed `Base` to get `Cfg` and `Bus` access
4. Register in `daemon.New()` in `internal/daemon/daemon.go`
5. Add any new `EventType` constants in `pkg/models/events.go`
6. Write tests alongside the implementation

## Adding a Notifier

1. Create `internal/notifiers/mynotifier.go`
2. Implement the `Notifier` interface: `Name()`, `Send(event)`, `SendRaw(msg)`, `Test()`
3. Add config fields to `internal/config/config.go`
4. Register in `daemon.New()`

## PR Checklist

- `make test` passes with zero failures
- New code has tests where practical
- No hardcoded secrets or tokens
- Commit messages are descriptive (what + why)
- Link the relevant issue if one exists

## Coding Style

- Standard Go formatting (`gofmt`)
- Use `log/slog` for structured logging
- Errors are wrapped with `fmt.Errorf("context: %w", err)`
- Keep watchers self-contained — don't reach into other packages' internals
