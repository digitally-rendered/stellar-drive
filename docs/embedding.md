# Embedding stellar-drive

Stellar-drive ships as a CLI **and** as a Go library. The CLI is useful for quick demos, migrations, and local development, but most production services embed the engine directly via `engine.New(...)`. This guide shows that path end-to-end and calls out every extension point.

## When to embed vs. use the CLI

| Scenario | Path |
|---|---|
| Prototype, CI smoke test, or a pure-CRUD service | `stellar run` |
| Any of: custom handlers, auth middleware, per-schema policy, injected telemetry, non-CRUD endpoints | Embed |
| Schemas change at runtime (multi-tenant, user-defined) | Either — same code runs |

Embedding is the default in the rest of this document. The runnable reference implementation lives in [`examples/petstore/`](../examples/petstore/) — clone it as a starting point.

## Minimum viable embed

```go
package main

import (
    "context"
    "log/slog"
    "os"

    "github.com/digitally-rendered/stellar-drive/pkg/config"
    "github.com/digitally-rendered/stellar-drive/pkg/engine"
)

func main() {
    cfg, err := config.Load("stellar.yaml")
    if err != nil {
        slog.Error("load config", "error", err)
        os.Exit(1)
    }

    eng := engine.New(cfg)
    if err := eng.Start(context.Background()); err != nil {
        slog.Error("engine stopped", "error", err)
        os.Exit(1)
    }
}
```

This is exactly what `stellar init` scaffolds. It boots the full REST + GraphQL surface, loads schemas from `cfg.Schemas.Dir`, connects the storage backend named by `storage.default`, and blocks until the context is cancelled. No extension points are wired.

## Extension points

All extensions flow through `engine.Option` values passed to `engine.New`. The option functions are pure wiring — they never start goroutines or side-effect anything; that happens inside `engine.Start`.

### 1. Hook into the CRUD pipeline

The `FunctionRegistry` holds your guards, validators, transforms, and handler overrides.

```go
import "github.com/digitally-rendered/stellar-drive/pkg/core/registry"

funcReg := registry.NewFunctionRegistry()

// Deny delete unless an admin header is set.
funcReg.RegisterGuard("pet", registry.OpDelete, registry.Scope{},
    func(ctx context.Context, r *http.Request) error {
        if r.Header.Get("X-Role") != "admin" {
            return errors.New("admin only")
        }
        return nil
    })

// Custom validator that runs after JSON Schema validation.
funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{},
    minNameLength)

// Transform input before persistence.
funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{},
    canonicalise)

// Replace the default list handler entirely.
funcReg.RegisterHandler("pet", registry.OpList, registry.Scope{}, listPets)

eng := engine.New(cfg, engine.WithFunctionRegistry(funcReg))
```

`Scope{}` matches every version and channel. Narrow the scope by filling in `Version` (schema version, e.g. `"2.0.0"`) or `Channel` (`"rest"`, `"graphql"`).

Use [`stellar generate --overrides`](codegen.md#overrides) to scaffold a per-schema `*_overrides.go` stub with these four hook types pre-declared.

See [registry.md](registry.md) for the full pipeline order and the end-to-end RBAC example.

### 2. Subscribe to lifecycle events

The event bus fires pre-/post- events for every CRUD operation. Pre-events can abort; post-events are for side-effects.

```go
import "github.com/digitally-rendered/stellar-drive/pkg/core/event"

bus := event.NewBus()
bus.Subscribe(event.PostCreate, "pet", func(ctx context.Context, e *event.Event) error {
    slog.Info("pet created", "schema", e.SchemaName)
    return nil
})

eng := engine.New(cfg, engine.WithEventBus(bus))
```

Pass `""` as the schema name to receive events for every schema, or use `bus.SubscribeAll("pet", h)` to receive every event type for a single schema. Ordering and failure semantics are documented in [events.md](events.md).

### 3. Add HTTP middleware

Custom middleware runs **after** the built-in stack (recovery, request ID, logging, security headers, CORS, rate limit). This ordering is deliberate — your middleware sees a request that has already passed the security perimeter.

```go
eng := engine.New(cfg,
    engine.WithMiddleware(
        prometheusMetrics,
        openTelemetryTracing,
    ),
)
```

### 4. Inject a policy evaluator

For OPA/Rego policy enforcement you can supply a pre-configured `port.PolicyEvaluator`:

```go
eng := engine.New(cfg, engine.WithPolicyEvaluator(myEvaluator))
```

When provided, the engine skips its default evaluator construction from config. Use this to inject a remote OPA client, a mock evaluator for tests, or a wrapped evaluator with metrics.

### 5. Override the schema directory

`WithSchemaDir` overrides the path read from `cfg.Schemas.Dir`. Useful when the directory is runtime-determined (e.g., tenant-scoped):

```go
eng := engine.New(cfg, engine.WithSchemaDir("/var/lib/tenants/"+tenantID))
```

## Start and shutdown

`engine.Start(ctx)` blocks until either the context is cancelled or the engine encounters a fatal error. Wire a signal handler to trigger graceful shutdown:

```go
ctx, cancel := signal.NotifyContext(context.Background(),
    os.Interrupt, syscall.SIGTERM)
defer cancel()

if err := eng.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
    slog.Error("engine stopped", "error", err)
    os.Exit(1)
}
```

Graceful shutdown drains in-flight HTTP requests for `cfg.Server.GracefulShutdown` (default 15s) before closing the storage connections.

## Testing an embedded engine

The `pkg/testing/sdtest` package provides an in-memory repository and a helper `TestEngine` that wires a full engine without a real database. Use this in integration tests to exercise your hooks end-to-end:

```go
import "github.com/digitally-rendered/stellar-drive/pkg/testing/sdtest"

eng := sdtest.NewTestEngine(t,
    sdtest.WithSchemas("../../schemas"),
    sdtest.WithFunctionRegistry(myRegistry),
)
defer eng.Close()

resp := eng.POST("/api/v1/pets", `{"name":"rex","photo_urls":[]}`)
require.Equal(t, 201, resp.Code)
```

See [`pkg/testing/sdtest/`](../pkg/testing/sdtest/) for the full API.

## Related docs

- [Registry](registry.md) — guards, validators, transforms, handler overrides
- [Custom endpoints](custom-endpoints.md) — non-CRUD routes via handler overrides and middleware
- [Events](events.md) — bus semantics, ordering, failure behaviour
- [Configuration](configuration.md) — every field in stellar.yaml
- [`examples/petstore/`](../examples/petstore/) — runnable reference project
