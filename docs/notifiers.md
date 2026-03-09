# Notifier Setup Guide

## Overview

PiGuard supports four notification channels: **Telegram**, **Discord**, **ntfy.sh**, and **generic Webhooks**. You can enable any combination, but at least one must be configured -- `config.Validate()` enforces this at startup and will refuse to run otherwise.

All notifiers receive every event that passes deduplication and quiet-hours filtering. Notifiers run synchronously within the daemon's event-handling loop; errors are logged but never crash the daemon.

Every notifier implements the same interface:

| Method             | Purpose                                          |
|--------------------|--------------------------------------------------|
| `Name() string`    | Returns the notifier identifier (e.g. "telegram") |
| `Send(event) error` | Delivers a structured event notification         |
| `SendRaw(message) error` | Sends a pre-formatted string (summaries, test messages) |
| `Test() error`     | Sends a test notification to verify configuration |

---

## Telegram

### Setup Steps

1. Open Telegram and message **@BotFather**. Send `/newbot` and follow the prompts to name your bot. You will receive a **bot token**.
2. Message **@userinfobot** to get your **Chat ID**. If you want alerts in a group, add the bot to the group and use the group's chat ID instead.
3. Save credentials in the env file:
   ```bash
   # /etc/piguard/env
   PIGUARD_TELEGRAM_TOKEN=123456:ABC-DEF...
   PIGUARD_TELEGRAM_CHAT_ID=-1001234567890
   ```
4. Add the Telegram section to your config:
   ```yaml
   notifications:
     telegram:
       enabled: true
       bot_token: "${PIGUARD_TELEGRAM_TOKEN}"
       chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"
       interactive: true
   ```
5. Verify the connection:
   ```bash
   sudo piguard test
   ```

### Interactive Mode

When `interactive: true`, the Telegram bot registers as a watcher (`TelegramBotWatcher`) and responds to slash commands such as `/docker`, `/storage`, `/services`, `/doctor`, `/updates`, `/update`, `/report`, and `/pilog`. Destructive actions require inline-keyboard confirmation. See [telegram-bot.md](telegram-bot.md) for the full command reference.

### Alert Format

Telegram alerts use HTML formatting:

```
<emoji> PiGuard -- raspberrypi

New port 0.0.0.0:8080 (docker-proxy, container: nginx)
Exposed to network -- consider restricting to 127.0.0.1

<lightbulb> Check if this port should be exposed
```

Event fields (`Details`, `Suggested`) are appended when present. Port info is rendered as a sorted key-value list for port-related events.

---

## Discord

### Setup Steps

1. Open Discord and go to **Server Settings** > **Integrations** > **Webhooks**.
2. Click **New Webhook**, choose the target channel, and copy the **Webhook URL**.
3. Save the URL in the env file:
   ```bash
   # /etc/piguard/env
   PIGUARD_DISCORD_WEBHOOK=https://discord.com/api/webhooks/...
   ```
4. Add the Discord section to your config:
   ```yaml
   notifications:
     discord:
       enabled: true
       webhook_url: "${PIGUARD_DISCORD_WEBHOOK}"
   ```
5. Verify the connection:
   ```bash
   sudo piguard test
   ```

### Alert Format

Discord notifications use rich embeds with color-coded severity:

| Severity | Color  | Hex       |
|----------|--------|-----------|
| Info     | Blue   | `#3498db` |
| Warning  | Orange | `#f39c12` |
| Critical | Red    | `#e74c3c` |

Each embed includes a title (severity emoji + hostname), description (event message), and optional **Details** and **Suggested** fields. Raw messages are sent as plain `content`.

---

## ntfy.sh

### Setup Steps

1. Choose a unique topic name (e.g., `piguard-mypi`). Anyone who knows the topic name can subscribe, so pick something not easily guessable.
2. Install the [ntfy app](https://ntfy.sh) on your phone and subscribe to the topic.
3. Add the ntfy section to your config:
   ```yaml
   notifications:
     ntfy:
       enabled: true
       topic: "piguard-mypi"
       server: "https://ntfy.sh"    # or your self-hosted server
       token: ""                     # optional, for private topics
   ```
4. Verify the connection:
   ```bash
   sudo piguard test
   ```

**Note:** The ntfy topic and server are not secrets -- they are written directly in the config YAML, not in the env file. If you use a private/authenticated topic, set the `token` field in the config or store it via an environment variable.

### Alert Format

ntfy maps PiGuard severity levels to notification priority and tags:

| Severity | Priority | Tag              |
|----------|----------|------------------|
| Critical | urgent   | `rotating_light` |
| Warning  | high     | `warning`        |
| Info     | default  | `shield`         |

The notification title is `PiGuard -- <hostname>`. The body contains the event message, details, and suggested action (if present).

---

## Webhook

### Setup Steps

1. Set up an HTTP endpoint that accepts POST requests with a JSON body.
2. Save the URL in the env file:
   ```bash
   # /etc/piguard/env
   PIGUARD_WEBHOOK_URL=https://your-endpoint.example.com/piguard
   ```
3. Add the webhook section to your config:
   ```yaml
   notifications:
     webhook:
       enabled: true
       url: "${PIGUARD_WEBHOOK_URL}"
       method: "POST"
   ```
   The `method` field defaults to `POST` if omitted.
4. Verify the connection:
   ```bash
   sudo piguard test
   ```

### JSON Payload Schema

**Event notifications** (`Send`) marshal the full `models.Event` struct:

```json
{
  "id": "uuid-string",
  "type": "port.opened",
  "severity": 1,
  "hostname": "raspberrypi",
  "timestamp": "2024-01-15T10:30:00Z",
  "message": "New port 0.0.0.0:8080",
  "details": "Process: docker-proxy (PID 1234)",
  "suggested": "Check if this port should be exposed",
  "source": "netlink",
  "port": {
    "address": "0.0.0.0:8080",
    "protocol": "tcp",
    "pid": 1234,
    "process_name": "docker-proxy",
    "container_name": "nginx",
    "container_id": "abc123...",
    "is_exposed": true
  }
}
```

**Raw messages** (`SendRaw`) send a simple envelope:

```json
{
  "message": "PiGuard test notification"
}
```

**Headers sent with every request:**

| Header         | Value                |
|----------------|----------------------|
| `Content-Type` | `application/json`   |
| `User-Agent`   | `PiGuard/0.1`        |

---

## Multiple Notifiers

You can enable any combination of notifiers. All enabled notifiers receive all events independently. Example config with Telegram and ntfy both active:

```yaml
notifications:
  telegram:
    enabled: true
    bot_token: "${PIGUARD_TELEGRAM_TOKEN}"
    chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"
    interactive: true
  ntfy:
    enabled: true
    topic: "piguard-mypi"
    server: "https://ntfy.sh"
```

If one notifier fails to deliver, the others still receive the event. Errors are logged at the `warn` level.

---

## See Also

- [docs/README.md](README.md) -- documentation index
- [docs/getting-started.md](getting-started.md) -- installation and first-run guide
- [telegram-bot.md](telegram-bot.md) -- interactive Telegram bot command reference
