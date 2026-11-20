package reload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hyphen-ani/pangolin/internal/config"
)

type ApplyFunc func(cfg *config.Config, hash string) error

type Reloader struct {
	path   string
	apply  ApplyFunc
	logger *slog.Logger

	mu       sync.Mutex
	lastHash string
}

func New(path string, apply ApplyFunc, logger *slog.Logger) *Reloader {
	return &Reloader{path: path, apply: apply, logger: logger}
}

func (r *Reloader) Load() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		return false, err
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])[:12]
	if hash == r.lastHash {
		return false, err
	}

	cfg, err := config.Parse(bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("%s: %w", r.path, err)
	}
	if err := r.apply(cfg, hash); err != nil {
		return false, fmt.Errorf("%s: %w", r.path, err)
	}
	r.lastHash = hash
	return true, nil
}

func (r *Reloader) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	/**
	Watch the directory, not the file. Editors save by writing a temp
	file and renaming it over the original, and Kubernetes updates a
	mounted ConfigMap by swapping a symlink. Both replace the file's
	inode, which silently breaks a watch placed on the file itself.
	*/
	dir := filepath.Dir(r.path)
	if err := w.Add(dir); err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	debounce := time.NewTimer(time.Hour)
	debounce.Stop()

	r.logger.Info("watching config for changes", "path", r.path)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			r.logger.Debug("config directory event", "event", ev.String())
			debounce.Reset(250 * time.Millisecond)
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			r.logger.Warn("config watcher error", "err", err)
		case <-hup:
			r.logger.Info("SIGHUP received, reloading config")
			r.reload("signal")
		case <-debounce.C:
			r.reload("file change")
		}
	}
}

func (r *Reloader) reload(trigger string) {
	changed, err := r.Load()
	switch {
	case err != nil:
		r.logger.Error("config reload failed; keeping current config", "trigger", trigger, "err", err)
	case !changed:
		r.logger.Debug("config unchanged, nothing to do", "trigger", trigger)
	}
}
