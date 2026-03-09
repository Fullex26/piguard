# Upgrading PiGuard

## Upgrade Methods

### Re-run Install Script (Recommended)

```bash
curl -sSL https://raw.githubusercontent.com/Fullex26/piguard/main/scripts/install.sh | sudo bash
```

The script detects an existing installation, keeps your config, downloads the latest release with checksum verification, and updates the binary and systemd service.

### Manual Binary Replace

```bash
sudo systemctl stop piguard
# Download the new binary for your architecture
sudo mv piguard-linux-arm64 /usr/local/bin/piguard
sudo chmod 755 /usr/local/bin/piguard
sudo systemctl start piguard
piguard version
```

### Build from Source

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard
git checkout v0.9.1  # or latest tag
make build           # or make build-pi for arm64
sudo systemctl stop piguard
sudo cp bin/piguard /usr/local/bin/piguard
sudo systemctl start piguard
```

## Configuration Migration

PiGuard uses sane defaults for all config fields. When new config sections are added in newer versions, they default to `enabled: false` or safe values — **no manual migration is required**.

If you want to enable new features after upgrading, add the relevant config section to your `/etc/piguard/config.yaml`. See [configuration.md](configuration.md) for the full reference, or run `sudo piguard setup` to reconfigure interactively.

## Version-Specific Notes

### v0.9.x

- Added `auto_update.auto_reboot` and `auto_update.reboot_delay_minutes` config fields
- No migration needed — defaults to `false` and `5`

### v0.8.x

- Added `auth_log` config section for SSH brute-force detection
- Added `logging` config section for file-based logging
- No migration needed — both default to disabled

### v0.7.x

- Added `connectivity` config section
- Added `auto_update` config section
- No migration needed — connectivity defaults to enabled, auto_update to disabled

### v0.6.x

- Added `network` config section for LAN device monitoring
- Added `alerts.weekly_report` config field
- No migration needed — network defaults to disabled

### v0.5.x

- Added `security_tools` config section
- Added `docker` config section
- No migration needed — docker defaults to enabled, security_tools to disabled

### v0.1–v0.4

- Initial releases with core port, firewall, and system monitoring
- No migration concerns

## Rollback

To roll back to a previous version:

1. Download the previous release binary from the [Releases page](https://github.com/Fullex26/piguard/releases)
2. Replace the binary and restart (same steps as manual upgrade)

Configuration is backward-compatible — older versions ignore unknown config fields. The SQLite event store at `/var/lib/piguard/events.db` is forward-only but does not need to be reset for rollback.

## Checking Current Version

```bash
piguard version
```

---

**See also:** [Documentation Index](README.md) | [Getting Started](getting-started.md) | [Configuration Reference](configuration.md)
