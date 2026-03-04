package event

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Handler processes a lifecycle event.
//
// Returning a non-nil error from a pre-event handler aborts the operation and
// propagates the error to the caller. Returning a non-nil error from a
// post-event handler is logged but does not abort; Publish still returns nil.
type Handler func(ctx context.Context, evt *Event) error

// handlerEntry pairs a handler with its schema scope. An empty schemaName
// means the handler is global and fires for every schema.
type handlerEntry struct {
	schemaName string
	handler    Handler
}

// Bus is an in-process event bus for lifecycle events. It is safe for
// concurrent use. Zero value is not usable; construct with NewBus.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Type][]handlerEntry
}

// NewBus returns an initialised, empty Bus.
func NewBus() *Bus {
	return &Bus{
		handlers: make(map[Type][]handlerEntry),
	}
}

// Subscribe registers handler for the given event type.
//
// schemaName="" is the global scope: the handler fires for every schema.
// Global handlers are always invoked before schema-specific handlers.
// Multiple calls to Subscribe append handlers in order within each scope.
func (b *Bus) Subscribe(eventType Type, schemaName string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handlerEntry{
		schemaName: schemaName,
		handler:    handler,
	})
}

// SubscribeAll registers handler for every known event type with the given
// schema scope. It is a convenience wrapper over Subscribe.
func (b *Bus) SubscribeAll(schemaName string, handler Handler) {
	for _, t := range AllTypes() {
		b.Subscribe(t, schemaName, handler)
	}
}

// Publish dispatches evt to all matching handlers.
//
// Handler selection follows two rules:
//  1. Global handlers (schemaName="") always run before schema-specific ones.
//  2. Within each scope, handlers run in subscription order.
//
// For pre-events the first error returned by any handler short-circuits
// execution and is returned to the caller. For post-events all matching
// handlers are called regardless of errors; any errors are logged via
// slog.Default() and Publish returns nil.
//
// Publish guarantees evt.Metadata is non-nil before any handler is called.
func (b *Bus) Publish(ctx context.Context, evt *Event) error {
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}

	b.mu.RLock()
	entries := b.handlers[evt.Type]
	// Copy the slice under the read lock so handlers cannot cause a deadlock
	// if they call Subscribe/Clear on the same bus.
	snapshot := make([]handlerEntry, len(entries))
	copy(snapshot, entries)
	b.mu.RUnlock()

	// Partition: globals first, then schema-specific matches.
	var globals, specific []handlerEntry
	for _, e := range snapshot {
		if e.schemaName == "" {
			globals = append(globals, e)
		} else if e.schemaName == evt.SchemaName {
			specific = append(specific, e)
		}
	}

	ordered := append(globals, specific...) //nolint:gocritic // intentional new slice

	if evt.Type.IsPre() {
		return b.dispatchPre(ctx, evt, ordered)
	}
	b.dispatchPost(ctx, evt, ordered)
	return nil
}

// dispatchPre calls handlers in order and returns the first error encountered.
func (b *Bus) dispatchPre(ctx context.Context, evt *Event, entries []handlerEntry) error {
	for _, e := range entries {
		if err := e.handler(ctx, evt); err != nil {
			return fmt.Errorf("pre-event handler failed for %s/%s: %w", evt.SchemaName, evt.Type, err)
		}
	}
	return nil
}

// dispatchPost calls all handlers regardless of errors and logs any failures.
func (b *Bus) dispatchPost(ctx context.Context, evt *Event, entries []handlerEntry) {
	for _, e := range entries {
		if err := e.handler(ctx, evt); err != nil {
			slog.Default().ErrorContext(ctx, "post-event handler error",
				"schema", evt.SchemaName,
				"event", string(evt.Type),
				"error", err,
			)
		}
	}
}

// Clear removes all registered handlers. Primarily useful in tests.
func (b *Bus) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = make(map[Type][]handlerEntry)
}
