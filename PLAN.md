# v0.6 — Auto-Updater Plan

## Design Decisions

1. **No cron library** — Go's stdlib `time` package is sufficient. The daemon already uses `time.After` for daily summaries (`runDailySummary` in daemon.go). The auto-updater follows the same pattern: check every minute, fire when `HH:MM` + weekday match.

2. **Schedule format**: simple `day_of_week` + `time` fields rather than cron syntax. Keeps config readable, no new dependency. Example: `day: "sunday"`, `time: "03:00"`.

3. **Watcher, not daemon goroutine** — implemented as an `AutoUpdateWatcher` following the established pattern (Base, injectable exec function, Start loop with ticker). This keeps it consistent with all other watchers and makes it testable.

4. **Telegram commands**:
   - `/updates` — check what's available (`apt list --upgradable`) without applying
   - `/update CONFIRM` — trigger an on-demand upgrade from your phone

5. **Reboot handling**: after upgrade, check `/var/run/reboot-required`. If present, alert with a warning message suggesting `/reboot CONFIRM`. No auto-reboot (too risky as a default).

6. **Event types**: `EventSystemUpdated` (Info, successful upgrade with package count), `EventSystemUpdateFailed` (Warning, apt returned error).

## Files to Create

### 1. `internal/watchers/auto_update.go`

```go
type AutoUpdateWatcher struct {
    Base
    dayOfWeek time.Weekday
    timeHHMM  string // "03:00"
    runApt    func(args ...string) (string, error) // injectable for tests
}
```

- `NewAutoUpdateWatcher(cfg, bus)` — parses config, sets `runApt` to real `exec.Command`
- `Start(ctx)` — ticker every 60s, checks if `now.Weekday() == dayOfWeek && HH:MM == timeHHMM`
- `runUpgrade()` — runs `apt-get update`, then `apt-get upgrade -y`, parses output for upgraded package count, publishes event. Checks `/var/run/reboot-required` after.
- Helper: `parseUpgradeCount(output string) int` — extracts count from apt output ("X upgraded, Y newly installed...")

### 2. `internal/watchers/auto_update_test.go`

- TestAutoUpdateWatcher_Name
- TestAutoUpdateWatcher_ParseUpgradeCount (table-driven: "3 upgraded", "0 upgraded", empty, etc.)
- TestAutoUpdateWatcher_RunUpgrade_Success (stub runApt, verify Info event with package count)
- TestAutoUpdateWatcher_RunUpgrade_Failure (stub runApt error, verify Warning event)
- TestAutoUpdateWatcher_RunUpgrade_RebootRequired (stub runApt + reboot-required file exists)
- TestAutoUpdateWatcher_ScheduleMatch (verify weekday+time matching logic)
- TestAutoUpdateWatcher_CheckAvailable (verify the "check only" path used by `/updates`)

## Files to Modify

### 3. `pkg/models/events.go`
Add:
```go
EventSystemUpdated     EventType = "system.updated"       // Successful apt upgrade
EventSystemUpdateFailed EventType = "system.update_failed" // apt upgrade error
```

### 4. `internal/config/config.go`
Add:
```go
type AutoUpdateConfig struct {
    Enabled   bool   `yaml:"enabled"`
    DayOfWeek string `yaml:"day_of_week"` // "sunday", "monday", etc. or "daily"
    Time      string `yaml:"time"`        // "03:00" (24h format)
}
```
Add field to `Config` struct: `AutoUpdate AutoUpdateConfig \`yaml:"auto_update"\``
Add to `DefaultConfig()`:
```go
AutoUpdate: AutoUpdateConfig{
    Enabled:   false, // opt-in — user must explicitly enable
    DayOfWeek: "sunday",
    Time:      "03:00",
},
```

### 5. `internal/daemon/daemon.go`
Register:
```go
if cfg.AutoUpdate.Enabled {
    d.watchers = append(d.watchers, watchers.NewAutoUpdateWatcher(cfg, bus))
}
```

### 6. `internal/watchers/telegram_bot.go`
Add to `handleCommand` switch:
```go
case "/updates":
    response = w.cmdUpdates()
case "/update":
    response = w.cmdUpdate(parts)
```

Add `cmdUpdates()` — runs `apt list --upgradable 2>/dev/null`, formats as Telegram HTML.
Add `cmdUpdate(parts)` — requires CONFIRM guard, runs `apt-get update && apt-get upgrade -y`, returns summary with package count.

Update `cmdHelp()` to include the new commands under a "System" or new "Updates" section.

### 7. `configs/default.yaml`
Add:
```yaml
# ── Auto-update ──
auto_update:
  enabled: false
  day_of_week: "sunday"  # or "daily" for every day
  time: "03:00"          # 24-hour format
```

### 8. `internal/doctor/doctor.go`
Add `checkAutoUpdate()` — if enabled, verify `apt-get` is available.

### 9. Release checklist files
- CHANGELOG.md — add v0.7.0 section
- README.md — mark v0.7 [x], update "What It Monitors"
- CLAUDE.md — no new watcher to list (AutoUpdateWatcher is a maintenance watcher, not a security monitor), but add `/update` and `/updates` to CLI subcommands if relevant
- Todo.txt — clear the auto-updater item
