package schema

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// fileFingerprint tracks the identity of a file on disk. Two files with the
// same mtime and size are treated as identical without reading their contents.
type fileFingerprint struct {
	ModTime time.Time
	Size    int64
}

// ReloadCallback is invoked after a schema file is successfully loaded into the
// registry. name is the schema name, version is the schema version string.
type ReloadCallback func(name, version string)

// WatcherOption configures a SchemaWatcher.
type WatcherOption func(*SchemaWatcher)

// WithPollInterval sets how often the watcher scans the directory for changes.
// Default is 500ms.
func WithPollInterval(d time.Duration) WatcherOption {
	return func(w *SchemaWatcher) {
		w.pollInterval = d
	}
}

// WithDebounce sets how long the watcher waits after detecting a change before
// actually reloading the file. Rapid successive writes collapse into one reload.
// Default is 500ms.
func WithDebounce(d time.Duration) WatcherOption {
	return func(w *SchemaWatcher) {
		w.debounceDelay = d
	}
}

// WithOnReload registers a callback that is invoked after each successful reload.
func WithOnReload(cb ReloadCallback) WatcherOption {
	return func(w *SchemaWatcher) {
		w.onReload = cb
	}
}

// SchemaWatcher polls a directory for *.schema.json file changes and keeps the
// Registry in sync. New files are registered, modified files are re-registered,
// and deleted files are removed. A debounce timer collapses rapid writes.
//
// All internal map access is protected by mu. The polling goroutine is the only
// writer to fingerprints and pending; callers interact only through Start/Stop.
type SchemaWatcher struct {
	dir           string
	registry      *Registry
	onReload      ReloadCallback
	pollInterval  time.Duration
	debounceDelay time.Duration

	mu           sync.Mutex
	fingerprints map[string]fileFingerprint // abs path -> fingerprint
	pending      map[string]*time.Timer     // abs path -> pending debounce timer

	cancel context.CancelFunc
}

// NewSchemaWatcher creates a watcher that monitors dir and keeps registry
// up-to-date. Call Start to begin polling.
func NewSchemaWatcher(dir string, registry *Registry, opts ...WatcherOption) *SchemaWatcher {
	w := &SchemaWatcher{
		dir:           dir,
		registry:      registry,
		pollInterval:  500 * time.Millisecond,
		debounceDelay: 500 * time.Millisecond,
		fingerprints:  make(map[string]fileFingerprint),
		pending:       make(map[string]*time.Timer),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start takes an initial snapshot of the directory and spawns a goroutine that
// polls for changes until the context is cancelled or Stop is called. Start is
// non-blocking; the polling runs in the background.
//
// Calling Start a second time without a preceding Stop is a no-op on the old
// goroutine; a new cancel is installed so Stop will cancel the new one.
func (w *SchemaWatcher) Start(ctx context.Context) {
	watchCtx, cancel := context.WithCancel(ctx)

	w.mu.Lock()
	if w.cancel != nil {
		// Cancel any prior goroutine before replacing the cancel func.
		w.cancel()
	}
	w.cancel = cancel
	w.mu.Unlock()

	// Seed the fingerprint map so we don't fire spurious events on startup.
	w.snapshot()

	go w.poll(watchCtx)
}

// Stop cancels the background polling goroutine and drains all pending debounce
// timers. It is safe to call Stop more than once or before Start.
func (w *SchemaWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}

	for path, timer := range w.pending {
		timer.Stop()
		delete(w.pending, path)
	}
}

// poll runs inside the background goroutine and calls diff on each tick.
func (w *SchemaWatcher) poll(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.diff()
		}
	}
}

// snapshot reads the current directory state into w.fingerprints without
// scheduling any reloads. Called once on startup so the initial state is
// baseline rather than treated as new files.
func (w *SchemaWatcher) snapshot() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		path := filepath.Join(w.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		w.fingerprints[path] = fileFingerprint{
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}
	}
}

// diff compares the current directory state against the stored fingerprints and
// schedules upserts or deletes as needed.
func (w *SchemaWatcher) diff() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	// Build the set of files currently on disk.
	current := make(map[string]fileFingerprint)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		path := filepath.Join(w.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		current[path] = fileFingerprint{
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Detect new and modified files.
	for path, fp := range current {
		prev, exists := w.fingerprints[path]
		if !exists || prev.ModTime != fp.ModTime || prev.Size != fp.Size {
			w.fingerprints[path] = fp
			w.scheduleUpsert(path)
		}
	}

	// Detect deleted files.
	for path := range w.fingerprints {
		if _, ok := current[path]; !ok {
			delete(w.fingerprints, path)
			w.scheduleDelete(path)
		}
	}
}

// scheduleUpsert sets (or resets) the debounce timer for path so that rapid
// successive writes collapse into one reload. Must be called with w.mu held.
func (w *SchemaWatcher) scheduleUpsert(path string) {
	if t, ok := w.pending[path]; ok {
		t.Stop()
	}
	w.pending[path] = time.AfterFunc(w.debounceDelay, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		w.handleUpsert(path)
	})
}

// scheduleDelete sets (or resets) the debounce timer for a file removal.
// Must be called with w.mu held.
func (w *SchemaWatcher) scheduleDelete(path string) {
	if t, ok := w.pending[path]; ok {
		t.Stop()
	}
	w.pending[path] = time.AfterFunc(w.debounceDelay, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		w.handleDelete(path)
	})
}

// handleUpsert reads path from disk, parses the SchemaEnvelope, and registers
// it with the registry. On success the onReload callback is invoked.
func (w *SchemaWatcher) handleUpsert(path string) {
	envelope, err := LoadFromFile(path)
	if err != nil {
		return
	}

	// Mark the schema active when loaded from a file on disk.
	envelope.Active = true

	if _, err := w.registry.Register(envelope); err != nil {
		return
	}

	if w.onReload != nil {
		w.onReload(envelope.Name, envelope.Version)
	}
}

// handleDelete derives the schema name from the filename and removes it from
// the registry. The filename convention is "<name>.schema.json".
func (w *SchemaWatcher) handleDelete(path string) {
	base := filepath.Base(path)
	// Strip ".schema.json" suffix to get the schema name.
	name := strings.TrimSuffix(base, ".schema.json")
	if name == "" || name == base {
		return
	}
	w.registry.Remove(name)
}
