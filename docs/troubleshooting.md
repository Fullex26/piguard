# Troubleshooting & FAQ

## Diagnostic Tools

### piguard doctor

Run `sudo piguard doctor` for a comprehensive health check. It verifies:

- Config file is loaded and valid
- At least one notifier is enabled
- systemd service is running
- SQLite event store is accessible
- Required system tools are available (ss, iptables, docker, etc. — only for enabled features)

Each check shows OK/Warn/Fail with a fix suggestion.

### journalctl

View PiGuard service logs:

```bash
journalctl -u piguard -f          # Follow live logs
journalctl -u piguard --since today
journalctl -u piguard -n 100      # Last 100 lines
```

### /pilog (Telegram)

Send `/pilog` in Telegram to see the last 30 lines of PiGuard's log file (requires `logging.file` to be configured).

### Verbose mode

Run with debug logging: `sudo piguard run -v`

## Common Issues

### "at least one notification channel must be enabled"

**Cause**: No notifier has `enabled: true` in config.

**Fix**: Enable at least one notification channel in `/etc/piguard/config.yaml` and set its credentials. Run `sudo piguard setup` to configure interactively.

### Telegram bot not responding to commands

**Cause**: `interactive` is set to false, or bot lacks message permissions in the group.

**Fix**:

1. Ensure `notifications.telegram.interactive: true` in config
2. If using a group chat, make sure the bot is a member and has permission to read messages
3. Check that the chat_id matches where you're sending commands
4. Restart PiGuard after config changes: `sudo systemctl restart piguard`

### No alerts after installation

**Cause**: Multiple possible causes.

**Fix**:

1. Check service status: `systemctl status piguard`
2. Check config: `sudo piguard doctor`
3. Send test notification: `sudo piguard test`
4. Check quiet hours — non-critical events are suppressed during quiet hours (default 23:00-07:00)
5. Deduplication — the same event won't fire again within the cooldown period (default 15 minutes)
6. Check `alerts.min_severity` — if set to "critical", warning-level events won't notify

### Permission denied errors

**Cause**: PiGuard needs root or specific capabilities.

**Fix**:

- Run as root via systemd (default install does this)
- For Docker: `sudo usermod -aG docker root` (or whichever user runs PiGuard)
- For iptables: Needs `CAP_NET_ADMIN` or root
- For auth.log: Needs read access to `/var/log/auth.log`

### High memory usage

**Cause**: PiGuard typically uses ~20MB. Higher usage may indicate many unique dedup keys.

**Fix**: Dedup cleanup runs hourly and prunes old keys. If usage exceeds 50MB, check for event storms (e.g., file integrity watching a frequently-modified directory).

### Docker watcher not detecting containers

**Cause**: Docker not installed, not running, or permission denied.

**Fix**:

1. `sudo piguard doctor` — check Docker status
2. Verify Docker is running: `sudo systemctl status docker`
3. Verify Docker is accessible: `docker ps`
4. If you don't use Docker, set `docker.enabled: false` in config

### File integrity false positives

**Cause**: System updates or legitimate config changes trigger alerts.

**Fix**: File integrity events have a cooldown (default 5 minutes). For files that change frequently, either remove them from `file_integrity.paths` or increase the cooldown.

### Network watcher ARP aging noise

**Cause**: ARP entries age out naturally, causing "device left" alerts.

**Fix**: Set `network.alert_on_leave: false` (default) to only alert on new devices. Use `network.ignore_macs` to suppress alerts for known devices like your router.

### Auto-update not running on schedule

**Cause**: Schedule mismatch or PiGuard wasn't running at the scheduled time.

**Fix**:

1. Check `auto_update.day_of_week` — must be a weekday name (e.g., "sunday") or "daily"
2. Check `auto_update.time` — 24-hour format (e.g., "03:00")
3. PiGuard must be running at the scheduled time — it does not retroactively run missed updates

## FAQ

### Does PiGuard work without Docker?

Yes. Set `docker.enabled: false` in config. All other watchers work independently. The `/docker` Telegram commands will not be available.

### Can I run PiGuard on x86 Linux?

Yes. Use `make build-amd64` or download `piguard-linux-amd64` from releases. All watchers work on any Linux system.

### How do I silence specific alert types?

There's no per-type silencing yet. You can:

- Adjust `alerts.min_severity` to filter by severity level
- Use `ports.ignore` to ignore specific port patterns
- Use `network.ignore_macs` to ignore specific devices
- Disable specific watchers entirely via their `enabled` flag

### Where are events stored?

SQLite database at `/var/lib/piguard/events.db`. Events are pruned after 30 days. View with `sudo piguard status` or use any SQLite viewer.

### Can I use multiple notification channels?

Yes. Enable as many as you want — all enabled notifiers receive all events.

### How do I back up PiGuard?

Back up these files:

- `/etc/piguard/config.yaml` — configuration
- `/etc/piguard/env` — credentials
- `/var/lib/piguard/events.db` — event history (optional)

---

## See also

- [Documentation index](README.md)
- [CLI reference](cli.md)
- [Getting started](getting-started.md)
