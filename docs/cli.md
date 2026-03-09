# CLI Reference

PiGuard uses [Cobra](https://github.com/spf13/cobra) for its command-line interface.

## Global Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | `/etc/piguard/config.yaml` | Path to config file |

## Commands

### `piguard run`

Start the PiGuard daemon. Typically managed by systemd rather than invoked directly.

| Flag | Short | Default | Description |
|---|---|---|---|
| `--verbose` | `-v` | `false` | Enable debug-level logging |

- Loads config, sets up logging, initializes daemon with watchers and notifiers
- Blocks until SIGINT or SIGTERM
- On shutdown: cancels context, waits for watcher goroutines, closes SQLite store
- Sends startup notification to all notifiers on launch

### `piguard setup`

Interactive setup wizard for first-time configuration.

| Flag | Default | Description |
|---|---|---|
| `--env-file` | `/etc/piguard/env` | Path to credentials file |

- Creates default config at `--config` path if none exists
- Prompts: setup mode, notification channel, credentials, security tools, advanced settings
- Writes secrets to env file (mode 0600), non-secrets to config YAML
- Optionally sends test notification and enables systemd service
- Safe to re-run on existing installations

### `piguard status`

Show current security status from the last 24 hours.

- Reads SQLite database directly -- no daemon needed
- Shows event count (24h), last alert time, and up to 10 recent events
- Database path: `/var/lib/piguard/events.db`

### `piguard test`

Send a test notification to all configured channels.

- Loads config and creates a temporary daemon instance
- Calls `Test()` on each enabled notifier
- Useful for verifying credentials after setup or changes

### `piguard send [message]`

Send an arbitrary message to Telegram.

- Reads from stdin if no argument given or argument is `-`
- Useful for scripting: `echo "Deploy complete" | piguard send -`
- Requires Telegram to be enabled in config
- Uses the Telegram notifier only (not other channels)

### `piguard doctor`

Check PiGuard installation health.

- Loads config (best-effort -- reports if it fails)
- Runs checks in categories:
  - **Config**: Config file loaded, notifiers enabled
  - **Daemon**: systemd service status
  - **Event store**: SQLite database accessible, event count
  - **Dependencies**: ss, iptables, docker, rkhunter, ClamAV, ip, apt-get, auth.log (only checks enabled features)
- Exits with code 1 if any check fails
- Provides fix suggestions for failures and warnings

### `piguard version`

Print version string. Version is injected at build time via ldflags.

Output: `PiGuard v0.9.1` followed by the GitHub URL.

## See also

- [Documentation Index](README.md)
- [Getting Started](getting-started.md)
- [Troubleshooting](troubleshooting.md)
