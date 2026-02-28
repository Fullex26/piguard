//go:build linux

package watchers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

const (
	inotifyBufSize = 4 * 1024
	watchMask      = unix.IN_CLOSE_WRITE |
		unix.IN_ATTRIB |
		unix.IN_DELETE_SELF |
		unix.IN_MOVE_SELF |
		unix.IN_CREATE |
		unix.IN_DELETE |
		unix.IN_MOVED_FROM |
		unix.IN_MOVED_TO
)

type watchEntry struct {
	path        string
	description string
	severity    models.Severity
	isDir       bool
}

// InotifyWatcher monitors file system paths for unauthorized changes using Linux inotify.
type InotifyWatcher struct {
	Base
	mu     sync.RWMutex
	wds    map[int]watchEntry
	hashes map[string]string // path → SHA256 baseline
}

func NewInotifyWatcher(cfg *config.Config, bus *eventbus.Bus) *InotifyWatcher {
	return &InotifyWatcher{
		Base:   Base{Cfg: cfg, Bus: bus},
		wds:    make(map[int]watchEntry),
		hashes: make(map[string]string),
	}
}

func (w *InotifyWatcher) Name() string { return "file_integrity" }

func (w *InotifyWatcher) Start(ctx context.Context) error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return fmt.Errorf("inotify_init1: %w", err)
	}
	defer unix.Close(fd)

	// Self-pipe trick: goroutine writes to [1] on ctx cancel, unblocking Poll on [0].
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	defer unix.Close(pipe[0])

	go func() {
		<-ctx.Done()
		_, _ = unix.Write(pipe[1], []byte{1})
		unix.Close(pipe[1])
	}()

	for _, wp := range w.Cfg.FileIntegrity.Paths {
		w.addWatch(fd, wp)
	}

	hostname, _ := os.Hostname()
	buf := make([]byte, inotifyBufSize)
	slog.Info("file integrity monitoring active", "watches", len(w.wds))

	for {
		fds := []unix.PollFd{
			{Fd: int32(fd), Events: unix.POLLIN},
			{Fd: int32(pipe[0]), Events: unix.POLLIN},
		}

		_, err := unix.Poll(fds, -1)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return fmt.Errorf("poll: %w", err)
		}

		// Stop pipe fired — context cancelled.
		if fds[1].Revents&unix.POLLIN != 0 {
			return nil
		}

		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}

		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EAGAIN {
				continue
			}
			return fmt.Errorf("read inotify: %w", err)
		}

		w.parseAndDispatch(buf[:n], hostname)
	}
}

func (w *InotifyWatcher) Stop() error { return nil }

func (w *InotifyWatcher) addWatch(fd int, wp config.WatchPath) {
	info, err := os.Stat(wp.Path)
	if err != nil {
		slog.Debug("file_integrity: path not found, skipping", "path", wp.Path)
		return
	}

	wd, err := unix.InotifyAddWatch(fd, wp.Path, watchMask)
	if err != nil {
		slog.Warn("file_integrity: failed to add watch", "path", wp.Path, "error", err)
		return
	}

	sev := models.SeverityWarning
	if wp.Severity == "critical" {
		sev = models.SeverityCritical
	}

	w.mu.Lock()
	w.wds[wd] = watchEntry{
		path:        wp.Path,
		description: wp.Description,
		severity:    sev,
		isDir:       info.IsDir(),
	}
	w.mu.Unlock()

	// Hash regular files for change detection.
	if !info.IsDir() {
		if h := hashFile(wp.Path); h != "" {
			w.mu.Lock()
			w.hashes[wp.Path] = h
			w.mu.Unlock()
		}
	}
}

func (w *InotifyWatcher) parseAndDispatch(buf []byte, hostname string) {
	offset := 0
	for offset < len(buf) {
		if offset+unix.SizeofInotifyEvent > len(buf) {
			break
		}
		raw := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
		nameLen := int(raw.Len)

		var name string
		if nameLen > 0 {
			end := offset + unix.SizeofInotifyEvent + nameLen
			if end > len(buf) {
				break
			}
			name = string(bytes.TrimRight(buf[offset+unix.SizeofInotifyEvent:end], "\x00"))
		}

		w.dispatchEvent(int(raw.Wd), raw.Mask, name, hostname)
		offset += unix.SizeofInotifyEvent + nameLen
	}
}

func (w *InotifyWatcher) dispatchEvent(wd int, mask uint32, name, hostname string) {
	w.mu.RLock()
	entry, ok := w.wds[wd]
	w.mu.RUnlock()
	if !ok {
		return
	}

	target := entry.path
	if name != "" {
		target = filepath.Join(entry.path, name)
	}

	var msg, details, suggested string
	var changeType string

	switch {
	case mask&unix.IN_CLOSE_WRITE != 0:
		newHash := hashFile(target)
		w.mu.Lock()
		oldHash := w.hashes[target]
		if newHash != "" {
			w.hashes[target] = newHash
		}
		w.mu.Unlock()
		// Skip if hash is unchanged (e.g. touch) or no baseline yet.
		if oldHash == "" || oldHash == newHash {
			return
		}
		changeType = "modified"
		msg = fmt.Sprintf("File modified: %s", target)
		details = fmt.Sprintf("SHA256 %s → %s", oldHash[:12], newHash[:12])
		suggested = fmt.Sprintf("Review changes: sudo diff /dev/null %s", target)

	case mask&unix.IN_ATTRIB != 0:
		info, err := os.Stat(target)
		if err != nil {
			return
		}
		changeType = "attrib"
		msg = fmt.Sprintf("File permissions changed: %s", target)
		details = fmt.Sprintf("Mode: %s", info.Mode())
		suggested = "Verify ownership: ls -la " + target

	case mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0:
		changeType = "deleted"
		msg = fmt.Sprintf("Watched file deleted or moved: %s", target)
		suggested = "Investigate: sudo journalctl -n 50"

	case mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0:
		changeType = "created"
		msg = fmt.Sprintf("File created in watched directory: %s", target)
		details = fmt.Sprintf("In: %s", entry.path)
		suggested = "Review: ls -la " + target

	case mask&(unix.IN_DELETE|unix.IN_MOVED_FROM) != 0:
		changeType = "removed"
		msg = fmt.Sprintf("File removed from watched directory: %s", target)
		details = fmt.Sprintf("From: %s", entry.path)

	default:
		return
	}

	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("fim-%d-%s", time.Now().UnixNano(), changeType),
		Type:      models.EventFileChanged,
		Severity:  entry.severity,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   msg,
		Details:   details,
		Suggested: suggested,
		Source:    w.Name(),
	})
}

func hashFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
