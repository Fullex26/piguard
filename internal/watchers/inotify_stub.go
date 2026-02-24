//go:build !linux

package watchers

import (
	"context"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
)

// InotifyWatcher is a no-op on non-Linux platforms (inotify is Linux-only).
type InotifyWatcher struct {
	Base
}

func NewInotifyWatcher(cfg *config.Config, bus *eventbus.Bus) *InotifyWatcher {
	return &InotifyWatcher{Base: Base{Cfg: cfg, Bus: bus}}
}

func (w *InotifyWatcher) Name() string                    { return "file_integrity" }
func (w *InotifyWatcher) Start(ctx context.Context) error { <-ctx.Done(); return nil }
func (w *InotifyWatcher) Stop() error                     { return nil }
