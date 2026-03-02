# Repository Guidelines

## Project Structure & Module Organization
PiGuard is a Go CLI daemon organized by responsibility:

- `cmd/piguard/`: CLI entrypoint and command wiring.
- `internal/daemon/`: runtime orchestration (watchers + notifiers).
- `internal/watchers/`: security signal collectors (ports, firewall, Docker, files, network, security tools).
- `internal/notifiers/`: outbound channels (Telegram, Discord, ntfy, webhook).
- `internal/config/`, `internal/eventbus/`, `internal/store/`: config parsing, pub/sub, persistence.
- `pkg/models/`: shared domain types (`Event`, severity, event types).
- `configs/`: default config and systemd unit; `scripts/`: install helpers; `bin/`: build outputs.

## Build, Test, and Development Commands
Use the `Makefile` targets for consistency:

- `make build`: build local binary at `bin/piguard`.
- `make dev`: run daemon with `configs/default.yaml`.
- `make test`: run full test suite with race detector (`go test -race ./... -v`).
- `make lint`: run `golangci-lint` using `.golangci.yml`.
- `make vuln`: run `govulncheck` dependency/code scan.
- `make build-pi`, `make build-pi3`, `make build-amd64`, `make build-all`: cross-compile targets.

## Coding Style & Naming Conventions
- Follow standard Go style: format with `gofmt` (or `go fmt ./...`) before opening a PR.
- Keep package boundaries clean: watchers/notifiers should be self-contained and registered through `internal/daemon`.
- Prefer structured logging (`log/slog`) and wrapped errors (`fmt.Errorf("context: %w", err)`).
- Naming: exported identifiers use Go conventions; files are lower_snake_case (for example, `docker_test.go`).

## Testing Guidelines
- Place tests next to implementation using `*_test.go` (see `internal/watchers/*_test.go`).
- Add/update tests for new watchers, notifiers, and bug fixes.
- Run targeted tests during development (example: `go test ./internal/watchers -run TestDockerWatcher`).
- Run `make test` before pushing. No fixed coverage threshold is enforced, but meaningful coverage is expected for changed behavior.

## Commit & Pull Request Guidelines
- Follow the observed commit style: conventional prefixes like `feat:`, `fix:`, `chore:`.
- Keep commit subjects concise and imperative; include issue/PR refs when relevant (example: `fix: improve firewall drift detection (#12)`).
- PRs should include: what changed, why, test evidence (`make test` / `make lint`), and linked issues.
- For user-facing changes, update `README.md`, `CHANGELOG.md`, or config docs in `configs/` as appropriate.

## Security & Configuration Tips
- Never commit real secrets/tokens; use placeholders or env vars in config values.
- Validate changes against `configs/default.yaml` and preserve safe defaults for Raspberry Pi deployments.
