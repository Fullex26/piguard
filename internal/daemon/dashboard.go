package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/pkg/models"
)

type dashboardSummary struct {
	Events24h       int            `json:"events_24h"`
	LastAlert       string         `json:"last_alert"`
	ByType          map[string]int `json:"by_type"`
	BySeverity      map[string]int `json:"by_severity"`
	RecentOutages   int            `json:"recent_outages"`
	NewDevices      int            `json:"new_devices"`
	ContainerStarts int            `json:"container_starts"`
	GeneratedAt     time.Time      `json:"generated_at"`
}

func startDashboard(ctx context.Context, cfg config.DashboardConfig, db *store.Store) *http.Server {
	listen := strings.TrimSpace(cfg.Listen)
	if listen == "" {
		listen = "127.0.0.1:20213"
	}

	mux := http.NewServeMux()
	registerDashboardHandlers(mux, db)

	server := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("dashboard shutdown failed", "error", err)
		}
	}()

	go func() {
		slog.Info("starting dashboard", "listen", listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("dashboard failed", "listen", listen, "error", err)
		}
	}()

	return server
}

func registerDashboardHandlers(mux *http.ServeMux, db *store.Store) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		hours := parsePositiveInt(r.URL.Query().Get("hours"), 24)
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
		if limit > 100 {
			limit = 100
		}

		events, err := db.GetRecentEvents(hours)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(events) > limit {
			events = events[:limit]
		}
		writeJSON(w, events)
	})

	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		summary, err := buildDashboardSummary(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, summary)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		renderDashboard(w, db)
	})
}

func parsePositiveInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func buildDashboardSummary(db *store.Store) (dashboardSummary, error) {
	events, err := db.GetRecentEvents(24)
	if err != nil {
		return dashboardSummary{}, err
	}
	lastAlert, err := db.GetLastAlertTime()
	if err != nil {
		return dashboardSummary{}, err
	}

	summary := dashboardSummary{
		Events24h:   len(events),
		LastAlert:   lastAlert,
		ByType:      make(map[string]int),
		BySeverity:  make(map[string]int),
		GeneratedAt: time.Now(),
	}

	for _, event := range events {
		summary.ByType[string(event.Type)]++
		summary.BySeverity[event.Severity.String()]++
		switch event.Type {
		case models.EventConnectivityLost:
			summary.RecentOutages++
		case models.EventNetworkNewDevice:
			summary.NewDevices++
		case models.EventContainerStart:
			summary.ContainerStarts++
		}
	}

	return summary, nil
}

func renderDashboard(w http.ResponseWriter, db *store.Store) {
	summary, err := buildDashboardSummary(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, err := db.GetRecentEvents(24)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(events) > 25 {
		events = events[:25]
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PiGuard</title>
<style>
:root { color-scheme: light dark; --bg: #f7f7f4; --fg: #202124; --muted: #666b70; --line: #d9d9d0; --panel: #ffffff; --accent: #176b5b; --warn: #a15c00; --crit: #b42318; }
@media (prefers-color-scheme: dark) { :root { --bg: #111312; --fg: #ededeb; --muted: #a1a5a8; --line: #303633; --panel: #181c1a; --accent: #55b7a1; --warn: #ffb454; --crit: #ff8074; } }
* { box-sizing: border-box; }
body { margin: 0; font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: var(--bg); color: var(--fg); }
main { max-width: 1120px; margin: 0 auto; padding: 28px 20px 40px; }
header { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; margin-bottom: 22px; }
h1 { font-size: 28px; line-height: 1.1; margin: 0; letter-spacing: 0; }
h2 { font-size: 16px; margin: 0 0 10px; }
.muted { color: var(--muted); }
.grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin-bottom: 22px; }
.metric, section { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; }
.metric { padding: 14px; min-height: 86px; }
.label { display: block; color: var(--muted); font-size: 12px; margin-bottom: 8px; }
.value { display: block; font-size: 24px; font-weight: 650; }
section { padding: 16px; margin-top: 12px; }
table { width: 100%%; border-collapse: collapse; table-layout: fixed; }
th, td { padding: 9px 8px; border-top: 1px solid var(--line); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
th { color: var(--muted); font-weight: 600; font-size: 12px; }
.sev-info { color: var(--accent); }
.sev-warning { color: var(--warn); }
.sev-critical { color: var(--crit); font-weight: 650; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { border: 1px solid var(--line); border-radius: 999px; padding: 4px 9px; background: transparent; }
@media (max-width: 760px) { main { padding: 20px 12px 28px; } header { display: block; } .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } th:nth-child(1), td:nth-child(1) { display: none; } }
</style>
</head>
<body>
<main>
<header>
<div>
<h1>PiGuard</h1>
<div class="muted">Network and host event recorder</div>
</div>
<div class="muted">Generated %s</div>
</header>
<div class="grid">
<div class="metric"><span class="label">Events 24h</span><span class="value">%d</span></div>
<div class="metric"><span class="label">Last Alert</span><span class="value">%s</span></div>
<div class="metric"><span class="label">Outages</span><span class="value">%d</span></div>
<div class="metric"><span class="label">New Devices</span><span class="value">%d</span></div>
</div>`,
		html.EscapeString(summary.GeneratedAt.Format("2006-01-02 15:04:05")),
		summary.Events24h,
		html.EscapeString(summary.LastAlert),
		summary.RecentOutages,
		summary.NewDevices,
	)

	_, _ = fmt.Fprint(w, `<section><h2>Event Types</h2><div class="chips">`)
	for _, item := range sortedCounts(summary.ByType) {
		_, _ = fmt.Fprintf(w, `<span class="chip">%s %d</span>`, html.EscapeString(item.key), item.value)
	}
	_, _ = fmt.Fprint(w, `</div></section>`)

	_, _ = fmt.Fprint(w, `<section><h2>Recent Events</h2><table><thead><tr><th>Time</th><th>Severity</th><th>Type</th><th>Message</th></tr></thead><tbody>`)
	for _, event := range events {
		sev := event.Severity.String()
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td class="sev-%s">%s</td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(event.Timestamp.Format("15:04:05")),
			html.EscapeString(sev),
			html.EscapeString(sev),
			html.EscapeString(string(event.Type)),
			html.EscapeString(event.Message),
		)
	}
	_, _ = fmt.Fprint(w, `</tbody></table></section></main></body></html>`)
}

type countItem struct {
	key   string
	value int
}

func sortedCounts(counts map[string]int) []countItem {
	items := make([]countItem, 0, len(counts))
	for key, value := range counts {
		items = append(items, countItem{key: key, value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].value == items[j].value {
			return items[i].key < items[j].key
		}
		return items[i].value > items[j].value
	})
	return items
}
