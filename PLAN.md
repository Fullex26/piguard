# v0.7 — Security Hardening + UX Polish

Four features that strengthen PiGuard's security coverage and improve the Telegram bot experience.

---

## 1. AuthLogWatcher — SSH brute force + failed sudo detection

**Why:** `EventSSHBruteForce` is defined in `events.go` but nothing fires it. SSH brute force is the most common real threat on an internet-facing Pi.

**Design:**
- New watcher: `internal/watchers/auth_log.go`
- Tails `/var/log/auth.log` (configurable path)
- Detects patterns:
  - `Failed password for .* from <IP>` — count per IP in a sliding window
  - `COMMAND.*sudo` with `authentication failure` — failed sudo attempt
  - `Accepted publickey` / `Accepted password` — successful login (Info, opt-in)
- Fires `ssh.bruteforce` (Critical) when failed count from a single IP exceeds threshold (default: 5 in 5 minutes)
- New event type: `EventSudoFailure` ("sudo.failure", Warning)
- Config section: `auth_log: { enabled, path, brute_force_threshold, brute_force_window }`
- Injectable `readFn` for testability (same pattern as SecToolsWatcher log tailing)
- Linux-only: uses inotify or polling; stub on non-Linux

**Tests:**
- Line parsing (table-driven: failed password, accepted key, sudo failure, noise lines)
- Brute force threshold (5 failures from same IP triggers, 4 doesn't)
- Window expiry (old failures age out)
- Different IPs don't cross-contaminate counts

## 2. Quiet Hours Enforcement

**Why:** `quiet_hours` config (start/end) exists but is never checked in the notification path.

**Design:**
- Add `isQuietHour(now time.Time) bool` to `internal/daemon/daemon.go`
- In `handleEvent()`, after dedup check: if `isQuietHour(now)` and severity is not Critical, skip notification (still save to SQLite)
- Critical events always bypass quiet hours (same philosophy as dedup bypass)
- Log suppressed notifications at Debug level

**Tests:**
- Time within quiet hours (23:30 with 23:00-07:00) — suppressed
- Time outside quiet hours (12:00) — not suppressed
- Critical during quiet hours — not suppressed
- Wrap-around midnight logic (start > end means overnight window)

## 3. Telegram Inline Keyboard Buttons

**Why:** Typing `/reboot CONFIRM` or `/docker remove nginx CONFIRM` is clunky on mobile. Inline keyboards are a native Telegram feature that makes dangerous actions safer and easier.

**Design:**
- Use Telegram `sendMessage` with `reply_markup` containing `InlineKeyboardMarkup`
- Confirmation flow:
  1. User sends `/reboot` → bot replies with warning text + [Confirm Reboot] button
  2. User taps button → bot receives `callback_query` with data like `reboot:confirm`
  3. Bot executes action and sends result
- Add callback query handling to `TelegramBotWatcher.poll()` (register `callback_query` in `allowed_updates`)
- Commands to convert: `/reboot`, `/update`, `/docker remove`, `/docker prune`, `/storage images|volumes|apt|all`
- Data format: `action:arg1:arg2` (e.g., `docker_remove:nginx`, `storage:images`)
- `answerCallbackQuery` call to dismiss the loading spinner

**Implementation notes:**
- New helper: `sendReplyWithKeyboard(text string, buttons [][]InlineButton)`
- New struct: `InlineButton { Text, CallbackData string }`
- Callback data is limited to 64 bytes by Telegram — use short codes
- Security: validate callback chat ID matches configured chat ID

## 4. Enhanced Weekly Trend Reports

**Why:** The daily summary is a point-in-time snapshot. A weekly report shows whether things are getting better or worse.

**Design:**
- New config field: `alerts.weekly_report` (default: `"sunday:20:00"`, empty = disabled)
- Query `store.GetEventCountByType(days int) map[EventType]int`
- New store method: `GetEventCountByType(days int)` — `SELECT type, COUNT(*) FROM events WHERE timestamp > ? GROUP BY type`
- Report includes:
  - Total events this week vs last week (trend arrow)
  - Top 3 event types by frequency
  - Uptime percentage (calculate from connectivity events or `/proc/uptime`)
  - Update status (last auto-update result, packages pending)
- Format as Telegram HTML with a clean summary layout
- Fires as a `summary.weekly` event (new EventType)

---

## Files Summary

### New files
- `internal/watchers/auth_log.go` + `auth_log_test.go`

### Modified files
- `pkg/models/events.go` — add `EventSudoFailure`, `EventWeeklySummary`, `EventSSHLogin`
- `internal/config/config.go` — add `AuthLogConfig`, `weekly_report` field
- `internal/daemon/daemon.go` — register AuthLogWatcher, add quiet hours check, add weekly report scheduler
- `internal/watchers/telegram_bot.go` — callback query handling, inline keyboard helpers, `/report` command
- `internal/store/store.go` — add `GetEventCountByType(days)`
- `configs/default.yaml` — add `auth_log:` and `weekly_report:` sections
- `internal/doctor/doctor.go` — add auth log file check

### Release checklist
- CHANGELOG.md, README.md, CLAUDE.md, Todo.txt — standard checklist
