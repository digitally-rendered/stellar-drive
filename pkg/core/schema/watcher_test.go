package schema

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSchema returns a minimal valid schema envelope JSON for the given name
// and version. It matches the SchemaEnvelope structure expected by Register.
func testSchema(name, version string) []byte {
	env := map[string]any{
		"name":    name,
		"version": version,
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			},
		},
		"active":     true,
		"created_at": "0001-01-01T00:00:00Z",
		"updated_at": "0001-01-01T00:00:00Z",
	}
	b, _ := json.Marshal(env)
	return b
}

// writeSchemaFile writes bytes to "<dir>/<name>.schema.json".
func writeSchemaFile(t *testing.T, dir, schemaName string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, schemaName+".schema.json")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

// newFastWatcher returns a watcher with short intervals suitable for tests.
func newFastWatcher(dir string, reg *Registry, opts ...WatcherOption) *SchemaWatcher {
	base := []WatcherOption{
		WithPollInterval(20 * time.Millisecond),
		WithDebounce(30 * time.Millisecond),
	}
	return NewSchemaWatcher(dir, reg, append(base, opts...)...)
}

// waitFor polls cond up to timeout, returning true if cond ever returns true.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSchemaWatcher_DetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	w := newFastWatcher(dir, reg)
	w.Start(context.Background())
	defer w.Stop()

	// Write the file after the watcher has started.
	writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))

	ok := waitFor(t, 2*time.Second, func() bool {
		return reg.Has("test_widget")
	})
	assert.True(t, ok, "expected test_widget to be registered after new file appeared")
}

func TestSchemaWatcher_DetectsModification(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	// Write v1 before the watcher starts so it is snapshotted, not treated as new.
	writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))

	w := newFastWatcher(dir, reg)
	w.Start(context.Background())
	defer w.Stop()

	// Register v1 manually so the registry knows about it before modification.
	_, err := reg.Register(&SchemaEnvelope{
		Name:    "test_widget",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
		Active:  true,
	})
	require.NoError(t, err)

	// Give the watcher a moment to take its first snapshot after Start, then
	// overwrite the file with v2.
	time.Sleep(60 * time.Millisecond)
	writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "2.0.0"))

	ok := waitFor(t, 2*time.Second, func() bool {
		def, err := reg.Get("test_widget", "2.0.0")
		return err == nil && def != nil
	})
	assert.True(t, ok, "expected test_widget v2.0.0 to be registered after modification")
}

func TestSchemaWatcher_DetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	// Pre-populate the registry so there is something to remove.
	_, err := reg.Register(&SchemaEnvelope{
		Name:    "test_widget",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
		Active:  true,
	})
	require.NoError(t, err)

	// Write the file so the watcher snapshots it.
	path := writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))

	w := newFastWatcher(dir, reg)
	w.Start(context.Background())
	defer w.Stop()

	// Let the watcher take its snapshot.
	time.Sleep(60 * time.Millisecond)

	require.NoError(t, os.Remove(path))

	ok := waitFor(t, 2*time.Second, func() bool {
		return !reg.Has("test_widget")
	})
	assert.True(t, ok, "expected test_widget to be removed after file deletion")
}

func TestSchemaWatcher_Debounce(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	var reloadCount atomic.Int32
	w := newFastWatcher(dir, reg,
		WithOnReload(func(name, version string) {
			reloadCount.Add(1)
		}),
	)
	w.Start(context.Background())
	defer w.Stop()

	// Write the file many times in rapid succession within the debounce window.
	for i := 0; i < 10; i++ {
		writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the debounce to settle.
	time.Sleep(200 * time.Millisecond)

	// Allow some tolerance: debounce collapses writes, so we expect 1 or 2 reloads
	// (one per distinct file change that makes it through), never 10.
	count := int(reloadCount.Load())
	assert.Less(t, count, 5, "debounce should collapse rapid writes; got %d reloads", count)
	assert.GreaterOrEqual(t, count, 1, "expected at least one reload")
}

func TestSchemaWatcher_CallsOnReload(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	type callRecord struct {
		name    string
		version string
	}
	var mu sync.Mutex
	var calls []callRecord

	w := newFastWatcher(dir, reg,
		WithOnReload(func(name, version string) {
			mu.Lock()
			calls = append(calls, callRecord{name, version})
			mu.Unlock()
		}),
	)
	w.Start(context.Background())
	defer w.Stop()

	writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))

	ok := waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) > 0
	})
	require.True(t, ok, "expected onReload callback to be invoked")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "test_widget", calls[0].name)
	assert.Equal(t, "1.0.0", calls[0].version)
}

func TestSchemaWatcher_StopCancelsPolling(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	var reloadCount atomic.Int32
	w := newFastWatcher(dir, reg,
		WithOnReload(func(name, version string) {
			reloadCount.Add(1)
		}),
	)
	w.Start(context.Background())

	// Write a file and let it be detected.
	writeSchemaFile(t, dir, "test_widget", testSchema("test_widget", "1.0.0"))
	waitFor(t, 2*time.Second, func() bool {
		return reg.Has("test_widget")
	})

	countAfterFirstLoad := reloadCount.Load()

	// Stop the watcher.
	w.Stop()

	// Let potential in-flight timers drain.
	time.Sleep(150 * time.Millisecond)

	countAfterStop := reloadCount.Load()

	// Write another file after Stop.
	writeSchemaFile(t, dir, "another_widget", testSchema("another_widget", "1.0.0"))
	time.Sleep(200 * time.Millisecond)

	finalCount := reloadCount.Load()

	// The count must not grow after Stop (allow for at most one timer that fired
	// right as Stop was called — hence using countAfterStop, not countAfterFirstLoad).
	assert.Equal(t, countAfterStop, finalCount,
		"no additional reloads expected after Stop; count before stop %d, after stop %d, final %d",
		countAfterFirstLoad, countAfterStop, finalCount)
	assert.False(t, reg.Has("another_widget"), "another_widget must not be registered after Stop")
}
