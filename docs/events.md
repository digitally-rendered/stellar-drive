# Event System

Stellar-drive provides an in-process event bus that fires lifecycle events before and after every CRUD operation. Events enable custom business logic -- audit logging, cache invalidation, notifications, side effects -- without modifying the core service layer.

## Overview

The event system is built on three components:

- **Event**: The payload passed to handlers, carrying the operation type, schema name, input data, and result.
- **Handler**: A function that processes an event and may return an error to influence the operation.
- **Bus**: The in-process dispatcher that routes events to matching handlers.

All event types, the `Event` struct, and the `Bus` live in `pkg/core/event/`.

## Event Types

Stellar-drive defines 16 lifecycle event types covering all CRUD operations, including bulk variants:

| Event Type | Constant | Fires When |
|------------|----------|------------|
| `pre_create` | `event.PreCreate` | Before a document is created |
| `post_create` | `event.PostCreate` | After a document is created |
| `pre_get` | `event.PreGet` | Before a document is fetched by ID |
| `post_get` | `event.PostGet` | After a document is fetched by ID |
| `pre_list` | `event.PreList` | Before a list query is executed |
| `post_list` | `event.PostList` | After a list query returns results |
| `pre_update` | `event.PreUpdate` | Before a document is updated |
| `post_update` | `event.PostUpdate` | After a document is updated |
| `pre_delete` | `event.PreDelete` | Before a document is soft-deleted |
| `post_delete` | `event.PostDelete` | After a document is soft-deleted |
| `pre_bulk_create` | `event.PreBulkCreate` | Before a bulk create operation |
| `post_bulk_create` | `event.PostBulkCreate` | After a bulk create operation |
| `pre_bulk_update` | `event.PreBulkUpdate` | Before a bulk update operation |
| `post_bulk_update` | `event.PostBulkUpdate` | After a bulk update operation |
| `pre_bulk_delete` | `event.PreBulkDelete` | Before a bulk delete operation |
| `post_bulk_delete` | `event.PostBulkDelete` | After a bulk delete operation |

You can retrieve all 16 types programmatically with `event.AllTypes()`.

The `IsPre()` method on a `Type` reports whether the event fires before the operation:

```go
if evt.Type.IsPre() {
    // This is a pre-operation event.
}
```

## Event Struct

Every handler receives an `*event.Event` with the following fields:

```go
type Event struct {
    Type       Type            // Lifecycle event type (e.g. PreCreate, PostGet)
    SchemaName string          // Name of the schema being operated on (e.g. "pet")
    Timestamp  time.Time       // When the event was created
    Input      any             // Caller-supplied data (create payload, update patch, query, IDs)
    Result     any             // Operation result (only populated for post-events)
    Metadata   map[string]any  // Request-scoped ambient data (never nil)
}
```

### Input and Result

For **pre-events**, `Input` carries the raw caller-supplied data and `Result` is nil. The specific type of `Input` depends on the operation:

| Operation | Input Type | Description |
|-----------|-----------|-------------|
| Create | `map[string]any` | The document data to create |
| Get | `string` | The entity ID to fetch |
| List | `*query.Query` | The query parameters |
| Update | `map[string]any` | The patch data |
| Delete | `string` | The entity ID to delete |
| BulkCreate | `[]map[string]any` | Slice of document data |
| BulkUpdate | `[]model.BulkUpdateItem` | Slice of update items |
| BulkDelete | `[]string` | Slice of entity IDs |

For **post-events**, both `Input` and `Result` are populated. `Result` holds the value produced by the operation (a `*model.Document`, `*model.ListResult`, etc.).

### Metadata

`Metadata` carries ambient request-scoped context such as a request ID, authenticated user, or trace information. The Bus guarantees that `Metadata` is never nil -- if the caller passes nil, it is initialised to an empty map before any handler is invoked.

## Handler Signature

A handler is any function matching the `event.Handler` type:

```go
type Handler func(ctx context.Context, evt *event.Event) error
```

The `ctx` parameter carries the request context, including any deadlines, cancellation signals, and values set by middleware.

## Pre vs Post Semantics

The distinction between pre-event and post-event handlers is critical:

### Pre-Event Handlers

- Fire **before** the operation is executed.
- A non-nil error **aborts** the operation and propagates the error to the caller.
- The first error short-circuits execution: remaining handlers are not called.
- Use pre-events for validation, authorization, rate limiting, or request enrichment.

### Post-Event Handlers

- Fire **after** the operation completes successfully.
- A non-nil error is **logged** via `slog.Default()` but does not abort anything.
- All matching handlers are called regardless of individual errors.
- `Publish` returns nil even if post-event handlers fail.
- Use post-events for audit logging, cache invalidation, notifications, and analytics.

## EventBus API

The `Bus` is constructed with `event.NewBus()` and provides four methods:

### Subscribe

```go
func (b *Bus) Subscribe(eventType Type, schemaName string, handler Handler)
```

Registers a handler for a specific event type. The `schemaName` parameter controls scope:

- **Schema-specific** (`schemaName="pet"`): The handler fires only for events on the "pet" schema.
- **Global** (`schemaName=""`): The handler fires for events on every schema.

Multiple calls to `Subscribe` append handlers in order within each scope.

### SubscribeAll

```go
func (b *Bus) SubscribeAll(schemaName string, handler Handler)
```

Convenience wrapper that registers the handler for all 16 event types with the given schema scope. Equivalent to calling `Subscribe` once for each type returned by `AllTypes()`.

### Publish

```go
func (b *Bus) Publish(ctx context.Context, evt *Event) error
```

Dispatches an event to all matching handlers. Handler selection follows two rules:

1. **Global handlers** (schemaName="") always run before schema-specific handlers.
2. **Within each scope**, handlers run in subscription order.

For pre-events, the first handler error short-circuits and is returned. For post-events, all handlers run and errors are logged. Returns nil for post-events regardless of handler errors.

### Clear

```go
func (b *Bus) Clear()
```

Removes all registered handlers. Primarily useful in tests to reset state between test cases.

## Execution Order

When `Publish` is called, handlers are partitioned and ordered as follows:

1. **Global handlers** (schemaName="") that match the event type, in subscription order.
2. **Schema-specific handlers** that match both the event type and schema name, in subscription order.

This guarantees that cross-cutting concerns (logging, metrics) registered globally always run before schema-specific business logic.

```go
bus := event.NewBus()

// Global audit handler -- fires for all schemas, runs first.
bus.Subscribe(event.PostCreate, "", auditHandler)

// Pet-specific handler -- fires only for "pet", runs after global.
bus.Subscribe(event.PostCreate, "pet", petNotificationHandler)

// Another global handler -- fires for all schemas, runs after auditHandler.
bus.Subscribe(event.PostCreate, "", metricsHandler)
```

When a `PostCreate` event fires for the "pet" schema, execution order is:

1. `auditHandler` (global, subscribed first)
2. `metricsHandler` (global, subscribed second)
3. `petNotificationHandler` (schema-specific)

## Thread Safety

The `Bus` uses `sync.RWMutex` internally. All methods are safe for concurrent use. The handler slice is copied under the read lock before dispatching, so handlers cannot cause a deadlock if they call `Subscribe` or `Clear` on the same bus.

## Code Examples

### Audit Logging

Log every mutation across all schemas:

```go
package main

import (
    "context"
    "log/slog"

    "github.com/digitally-rendered/stellar-drive/pkg/core/event"
)

func newAuditHandler(logger *slog.Logger) event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        logger.InfoContext(ctx, "audit",
            "event", string(evt.Type),
            "schema", evt.SchemaName,
            "timestamp", evt.Timestamp,
        )
        return nil
    }
}

func setupAudit(bus *event.Bus, logger *slog.Logger) {
    handler := newAuditHandler(logger)

    // Subscribe to all post-mutation events globally.
    bus.Subscribe(event.PostCreate, "", handler)
    bus.Subscribe(event.PostUpdate, "", handler)
    bus.Subscribe(event.PostDelete, "", handler)
}
```

### Pre-Create Validation

Block creation of pets with restricted names:

```go
func restrictedNameGuard(restricted map[string]bool) event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        data, ok := evt.Input.(map[string]any)
        if !ok {
            return nil
        }

        name, _ := data["name"].(string)
        if restricted[name] {
            return errors.ValidationFailed(errors.ErrorDetail{
                Field:   "name",
                Message: "this name is restricted",
                Code:    "restricted",
            })
        }
        return nil
    }
}

func setup(bus *event.Bus) {
    restricted := map[string]bool{"admin": true, "system": true}
    bus.Subscribe(event.PreCreate, "pet", restrictedNameGuard(restricted))
}
```

### Data Enrichment

Automatically set a `region` field on every new order based on request metadata:

```go
func enrichOrderRegion() event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        data, ok := evt.Input.(map[string]any)
        if !ok {
            return nil
        }

        region, _ := evt.Metadata["region"].(string)
        if region != "" {
            data["region"] = region
        }
        return nil
    }
}

func setup(bus *event.Bus) {
    bus.Subscribe(event.PreCreate, "order", enrichOrderRegion())
}
```

### SubscribeAll for Metrics

Track every operation on a specific schema:

```go
func schemaMetrics(counter *atomic.Int64) event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        counter.Add(1)
        return nil
    }
}

func setup(bus *event.Bus) {
    counter := &atomic.Int64{}
    bus.SubscribeAll("pet", schemaMetrics(counter))
}
```

## Ordering and Concurrency Guarantees

Event semantics are narrow on purpose. Read this before building anything that depends on "event B arrives after event A."

### Per-operation ordering (guaranteed)

Within a single CRUD call, the bus dispatches in this order:

1. Every matching `SubscribeAll` handler, in registration order.
2. Every handler registered for the specific `(eventType, schemaName)` pair, in registration order.
3. Handlers registered for the specific event type with `schemaName = ""` (global), in registration order.

Pre-events are fully synchronous — the service awaits each handler and the first error aborts both the remaining pre-handlers and the operation. Post-events are also synchronous, but handler errors are logged, not propagated.

### Cross-operation ordering (**not** guaranteed)

Two requests arriving concurrently hit the bus concurrently. `pet.post_create` for request A and `pet.post_update` for request B can interleave in any order. The framework does not serialise operations per entity, per schema, or globally.

If you need causal ordering across requests (e.g., "a `post_update` for entity X must not be processed before its `post_create`"), you must build that into your handler: buffer by `EntityID`, use a message queue downstream, or gate on record version.

### Mutation semantics

`Event.Input` and `Event.Metadata` are the raw maps passed by the service layer. Pre-event handlers can mutate these maps and the mutation is visible to later handlers and to the repository call that follows. This is the intended mechanism for data enrichment (see the enrichment example below). Post-event handlers can mutate `Event.Result` but by then the repository has already persisted the document, so mutations affect only the response path.

Two handlers running back-to-back within a single operation see each other's writes deterministically. Two handlers running in different operations do not share state through the event unless you put it there.

### Failure behaviour

| Event phase | Handler returns error | Effect |
|---|---|---|
| Pre | non-nil | Operation aborts, error propagates to caller, remaining pre-handlers skipped, post-event does **not** fire |
| Post | non-nil | Logged, remaining post-handlers still run, caller sees success |

This asymmetry is load-bearing: post-events are for side-effects (metrics, webhooks, cache invalidation) that must not be able to fail the write after the database has already accepted it.

## Integration with Services

The `GenericCRUDService` in `pkg/core/service/` fires events automatically during its operation lifecycle. You do not need to publish events manually when using the service layer. The service:

1. Publishes the pre-event with the caller's input.
2. If the pre-event returns an error, the operation is aborted.
3. Executes the operation via the repository.
4. Publishes the post-event with both the input and the result.

This means any handler registered on the bus is invoked transparently for every CRUD call, whether it originates from the REST adapter, GraphQL adapter, or direct service calls.

## Related Documentation

- **[Architecture](architecture.md)** -- Request/response flow showing where events fire in the pipeline
- **[Registry](registry.md)** -- Function registry for guards, validators, and transforms (complementary to events)
- **[Errors](errors.md)** -- Domain errors returned when pre-event handlers abort operations
