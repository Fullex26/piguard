package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fullexpi/piguard/pkg/models"
	_ "modernc.org/sqlite"
)

const DefaultDBPath = "/var/lib/piguard/events.db"

// Store persists events and state in SQLite
type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return s, nil
}

// Close closes the database
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			severity INTEGER NOT NULL,
			hostname TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			message TEXT NOT NULL,
			details TEXT,
			suggested TEXT,
			source TEXT,
			payload TEXT,
			notified BOOLEAN DEFAULT FALSE
		);

		CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
		CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
		CREATE INDEX IF NOT EXISTS idx_events_severity ON events(severity);

		CREATE TABLE IF NOT EXISTS baselines (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}

// SaveEvent persists an event to the database
func (s *Store) SaveEvent(event models.Event) error {
	payload, _ := json.Marshal(event)
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO events (id, type, severity, hostname, timestamp, message, details, suggested, source, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Type, event.Severity, event.Hostname, event.Timestamp,
		event.Message, event.Details, event.Suggested, event.Source, string(payload),
	)
	return err
}

// GetRecentEvents returns events from the last N hours
func (s *Store) GetRecentEvents(hours int) ([]models.Event, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE timestamp > ?
		ORDER BY timestamp DESC
		LIMIT 100`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var event models.Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// GetLastAlertTime returns when the last alert was sent
func (s *Store) GetLastAlertTime() (string, error) {
	var timestamp time.Time
	err := s.db.QueryRow(`
		SELECT timestamp FROM events
		WHERE severity > 0
		ORDER BY timestamp DESC
		LIMIT 1`).Scan(&timestamp)
	if err != nil {
		return "never", nil
	}

	diff := time.Since(timestamp)
	if diff < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(diff.Minutes())), nil
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(diff.Hours())), nil
	}
	return fmt.Sprintf("%d days ago", int(diff.Hours()/24)), nil
}

// GetEventCount returns the number of events in the last N hours
func (s *Store) GetEventCount(hours int) (int, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp > ?`, since).Scan(&count)
	return count, err
}

// SetState stores a key-value pair
func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO state (key, value) VALUES (?, ?)`, key, value)
	return err
}

// GetState retrieves a stored value
func (s *Store) GetState(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM state WHERE key = ?`, key).Scan(&value)
	return value, err
}

// Prune removes events older than N days
func (s *Store) Prune(days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	result, err := s.db.Exec(`DELETE FROM events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
