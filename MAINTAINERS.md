# Maintainers

This project is currently maintained by:

- **JFullex** (`@Fullex26`) — primary maintainer and final reviewer

## Scope Ownership

- `internal/watchers/**`: watcher lifecycle, security signal quality, false-positive tuning
- `internal/notifiers/**`: outbound delivery reliability and notifier UX
- `.github/workflows/**`: CI/CD, release, and repository automation
- `configs/**`: default behavior and production-safe settings

## Review & Response SLA

- New issues: triaged within **72 hours**
- New pull requests: first maintainer response within **72 hours**
- Security reports: acknowledged within **24 hours** (see `SECURITY.md`)

## Decision Process

- Normal changes: maintainer approval + passing required checks
- Breaking changes: require documented migration notes in PR and release notes
- Inactive/abandoned PRs may be closed after **30 days** without updates

## Release & Support Policy

- Supported versions: **latest release only** for fixes and support
- Best effort guidance may be provided for older releases
- Emergency security fixes are prioritized for the current release line
