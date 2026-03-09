# Getting Started with PiGuard

A step-by-step guide to installing and configuring PiGuard on your Raspberry Pi or Linux system.

## Prerequisites

- **Hardware**: Raspberry Pi (3B+, 4, 5, Zero 2 W) or any arm64/armv7/amd64 Linux system — see [compatibility.md](compatibility.md) for the full support matrix
- **OS**: Raspberry Pi OS (Bookworm/Bullseye), Ubuntu 22.04+, or Debian 11+
- **Root/sudo access** for installing binaries, creating config directories, and managing systemd services
- **Internet connection** for sending notifications via Telegram, ntfy.sh, Discord, or webhooks

## Installation

### One-Line Install (Recommended)

```bash
curl -sSL https://raw.githubusercontent.com/Fullex26/piguard/main/scripts/install.sh | sudo bash
```

This script will:

1. Detect your system architecture (arm64, armv7, or amd64)
2. Download the latest PiGuard release from GitHub
3. Verify the SHA-256 checksum to ensure integrity
4. Install the binary to `/usr/local/bin/piguard`
5. Create `/etc/piguard` (configuration) and `/var/lib/piguard` (data) directories
6. Install the systemd service unit
7. Launch the interactive setup wizard on fresh installs

### From GitHub Releases (Manual)

Download the binary for your architecture from the [releases page](https://github.com/Fullex26/piguard/releases/latest):

**arm64 (Raspberry Pi 4, Pi 5):**

```bash
curl -Lo piguard https://github.com/Fullex26/piguard/releases/latest/download/piguard-linux-arm64
```

**armv7 (Raspberry Pi 3):**

```bash
curl -Lo piguard https://github.com/Fullex26/piguard/releases/latest/download/piguard-linux-armv7
```

**amd64 (x86 Linux):**

```bash
curl -Lo piguard https://github.com/Fullex26/piguard/releases/latest/download/piguard-linux-amd64
```

Then install manually:

```bash
chmod 755 piguard
sudo mv piguard /usr/local/bin/piguard

# Create directories
sudo mkdir -p /etc/piguard /var/lib/piguard

# Download and install the systemd service file
curl -Lo piguard.service https://raw.githubusercontent.com/Fullex26/piguard/main/configs/piguard.service
sudo mv piguard.service /etc/systemd/system/piguard.service
sudo systemctl daemon-reload
```

### Build from Source

```bash
git clone https://github.com/Fullex26/piguard.git
cd piguard
make build        # or make build-pi for arm64 cross-compile
sudo make install
```

> **Note:** Requires Go 1.24+. No C toolchain is needed — PiGuard uses a pure-Go SQLite implementation via `modernc.org/sqlite`.

## Initial Configuration

### Interactive Setup Wizard

The recommended way to configure PiGuard:

```bash
sudo piguard setup
```

The wizard walks you through the following steps:

1. **Choose mode** — Simple (recommended for most users) or Advanced (full control over all settings)
2. **Choose notification channel** — Telegram, Discord, ntfy.sh, or Webhook
3. **Enter credentials** — bot tokens, chat IDs, webhook URLs, etc. (input is masked for secrets)
4. **Security tools** (optional) — if ClamAV or rkhunter are detected on your system, the wizard offers to enable log monitoring for malware/rootkit alerts
5. **Advanced mode only** — customize disk/memory/temperature thresholds, quiet hours, deduplication cooldowns, and watcher intervals
6. **Test notification** — sends a test message to verify your notification channel works
7. **Enable service** — optionally enables and starts the systemd service immediately

The wizard writes two files:

| File | Purpose | Permissions |
|------|---------|-------------|
| `/etc/piguard/config.yaml` | Main configuration (non-secret values, env var placeholders like `${PIGUARD_TELEGRAM_TOKEN}`) | 0644 |
| `/etc/piguard/env` | Credentials as `KEY=VALUE` pairs, loaded by systemd `EnvironmentFile` | 0600 |

This separation keeps secrets out of the config file and restricts credential access to root only.

### Minimum Manual Config

If you prefer to configure PiGuard by hand, create `/etc/piguard/config.yaml` with a minimal Telegram setup:

```yaml
notifications:
  telegram:
    enabled: true
    bot_token: "${PIGUARD_TELEGRAM_TOKEN}"
    chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"
```

Then create `/etc/piguard/env`:

```
PIGUARD_TELEGRAM_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
PIGUARD_TELEGRAM_CHAT_ID=-1001234567890
```

Set permissions:

```bash
sudo chmod 0600 /etc/piguard/env
```

At least one notification channel must be enabled, or PiGuard will refuse to start.

## Verify Installation

Run these commands to confirm everything is working:

```bash
# Check prerequisites and config validity
sudo piguard doctor

# Send a test notification to all configured channels
sudo piguard test

# Check the systemd service status
systemctl status piguard
```

`piguard doctor` will flag any missing dependencies, config errors, or permission issues.

## Start PiGuard

Enable and start the service:

```bash
sudo systemctl enable --now piguard
```

Follow the logs in real time:

```bash
journalctl -u piguard -f
```

PiGuard will begin monitoring immediately. You should see startup messages confirming which watchers are active and which notification channels are connected.

## Next Steps

- [configuration.md](configuration.md) — Full reference for all config options, thresholds, and watcher settings
- [telegram-bot.md](telegram-bot.md) — Interactive Telegram bot commands (`/docker`, `/storage`, `/services`, `/update`, and more)
- [watchers.md](watchers.md) — Detailed documentation for each watcher module

---

**See also:** [docs/README.md](README.md) | [configuration.md](configuration.md) | [compatibility.md](compatibility.md)
