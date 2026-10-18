// Package monitor watches files for changes and emits debounced events
package monitor

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Event reports that a watched file changed
type Event struct {
	Path string
	Op   string
	Time time.Time
}

// Watcher watches individual files. It subscribes to the parent
// directories rather than the files themselves so that atomic replaces
// (write temp file, rename over target) are still observed
type Watcher struct {
	fsw      *fsnotify.Watcher
	paths    map[string]struct{}
	debounce time.Duration
	events   chan Event
}

// New creates a Watcher for the given file paths. Rapid bursts of
// changes to the same file are collapsed into one event after debounce
func New(paths []string, debounce time.Duration) (*Watcher, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("monitor: no paths to watch")
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("monitor: create watcher: %w", err)
	}

	w := &Watcher{
		fsw:      fsw,
		paths:    make(map[string]struct{}, len(paths)),
		debounce: debounce,
		events:   make(chan Event),
	}

	dirs := make(map[string]struct{})

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			_ = fsw.Close()

			return nil, fmt.Errorf("monitor: resolve %q: %w", p, err)
		}

		w.paths[abs] = struct{}{}
		dirs[filepath.Dir(abs)] = struct{}{}
	}

	for dir := range dirs {
		err := fsw.Add(dir)
		if err != nil {
			_ = fsw.Close()

			return nil, fmt.Errorf("monitor: watch %q: %w", dir, err)
		}
	}

	return w, nil
}

// Events returns the channel of debounced change events. It is closed
// when Run returns
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Run processes filesystem notifications until ctx is cancelled or the
// underlying watcher fails. It closes the Events channel on return
func (w *Watcher) Run(ctx context.Context) error {
	defer close(w.events)
	defer w.fsw.Close()

	var mu sync.Mutex

	pending := make(map[string]*time.Timer)
	lastOp := make(map[string]string)

	defer func() {
		mu.Lock()
		defer mu.Unlock()

		for _, t := range pending {
			t.Stop()
		}
	}()

	emit := func(path string) {
		mu.Lock()
		op := lastOp[path]
		delete(pending, path)
		delete(lastOp, path)
		mu.Unlock()

		select {
		case w.events <- Event{Path: path, Op: op, Time: time.Now()}:
		case <-ctx.Done():
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}

			path := filepath.Clean(ev.Name)

			if _, watched := w.paths[path]; !watched {
				continue
			}

			mu.Lock()

			lastOp[path] = ev.Op.String()

			if t, exists := pending[path]; exists {
				t.Reset(w.debounce)
			} else {
				p := path
				pending[path] = time.AfterFunc(w.debounce, func() { emit(p) })
			}

			mu.Unlock()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}

			return fmt.Errorf("monitor: %w", err)
		}
	}
}
