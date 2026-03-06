package logging

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/Fullex26/piguard/internal/config"
)

// RotatingWriter writes to a log file with simple size-based rotation (1 backup).
type RotatingWriter struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	maxBytes int64
	written  int64
}

// NewRotatingWriter opens or creates the log file.
func NewRotatingWriter(path string, maxSizeMB int) (*RotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &RotatingWriter{
		file:     f,
		path:     path,
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
		written:  info.Size(),
	}, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.written+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, fmt.Errorf("rotating log: %w", err)
		}
	}

	n, err := w.file.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *RotatingWriter) rotate() error {
	w.file.Close()
	_ = os.Remove(w.path + ".1")
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	w.file = f
	w.written = 0
	return nil
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// TailLines returns the last n lines from the log file.
func (w *RotatingWriter) TailLines(n int) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// Setup configures slog with the given config and verbose flag.
// Returns a *RotatingWriter if file logging is enabled (caller must Close it),
// or nil if only stderr logging is used.
func Setup(cfg config.LoggingConfig, verbose bool) (*RotatingWriter, error) {
	level := parseLevel(cfg.Level)
	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var writer io.Writer = os.Stderr
	var rw *RotatingWriter

	if cfg.File != "" {
		maxSize := cfg.MaxSizeMB
		if maxSize <= 0 {
			maxSize = 10
		}
		var err error
		rw, err = NewRotatingWriter(cfg.File, maxSize)
		if err != nil {
			return nil, fmt.Errorf("opening log file %s: %w", cfg.File, err)
		}
		writer = io.MultiWriter(os.Stderr, rw)
	}

	handler := slog.NewTextHandler(writer, opts)
	slog.SetDefault(slog.New(handler))

	ActiveWriter = rw
	return rw, nil
}

// ActiveWriter holds the current RotatingWriter (if file logging is active).
// Set by Setup(); read by Telegram /pilog command.
var ActiveWriter *RotatingWriter

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
