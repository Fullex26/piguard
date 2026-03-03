# Contributing to PiGuard

Thanks for your interest in contributing to PiGuard.

## First PR in 15 Minutes

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard
go version                 # requires Go 1.22+
make test                  # must pass
```

Pick an issue labeled `good first issue` or `help wanted`, create a branch, make a focused change, and open a PR using the template in `.github/pull_request_template.md`.

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
- `make lint` passes for non-trivial Go changes
- New code has tests where practical
- No hardcoded secrets or tokens
- Commit messages are descriptive (what + why)
- Link the relevant issue if one exists
- Include risk + rollback notes for behavior changes
- Update docs/config comments for user-facing changes

## Labels & Triage

Repository labels should follow this taxonomy:

- Type: `bug`, `feature`, `docs`, `chore`, `security`
- Priority: `priority:high`, `priority:medium`, `priority:low`
- Status: `needs-triage`, `needs-repro`, `blocked`, `good first issue`, `help wanted`

Maintainers triage new issues and PRs within 72 hours (see `MAINTAINERS.md`).

## Required Repository Settings (P0)

These are mandatory for mainline quality:

- Branch protection on `main` with required checks: `Lint`, `Test`, `Vulnerability scan`, `Build (cross-compile check)`
- Require pull requests before merge
- Require up-to-date branches before merge
- Require linear history
- Dismiss stale approvals on new commits
- Enforce restrictions for administrators

See `MAINTAINERS.md`, `SUPPORT.md`, and `DEPRECATION.md` for governance policies.

## Coding Style

- Standard Go formatting (`gofmt`)
- Use `log/slog` for structured logging
- Errors are wrapped with `fmt.Errorf("context: %w", err)`
- Keep watchers self-contained — don't reach into other packages' internals
