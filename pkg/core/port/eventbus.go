package port

import (
	"context"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
)

// EventBus is the port for in-process lifecycle event subscription and
// dispatch. The canonical implementation is event.Bus; alternative
// implementations (e.g. a no-op for tests) can be substituted at wire-up
// time.
//
// Subscribe and Publish semantics mirror those of event.Bus:
//   - Global handlers (schemaName="") fire before schema-specific handlers.
//   - Pre-event handler errors abort the operation and are returned by Publish.
//   - Post-event handler errors are logged but do not abort; Publish returns nil.
type EventBus interface {
	// Subscribe registers handler for eventType. schemaName="" is the global
	// scope and fires for every schema.
	Subscribe(eventType event.Type, schemaName string, handler event.Handler)

	// Publish dispatches evt to all matching handlers. Returns the first
	// handler error for pre-events; returns nil for post-events regardless of
	// handler errors.
	Publish(ctx context.Context, evt *event.Event) error
}
