# Custom Endpoints

Stellar-drive generates CRUD routes automatically from your schemas, but real services always need some non-CRUD endpoints: a webhook receiver, a search endpoint that hits an external service, an aggregation that joins three schemas, a `/healthz` that reports business-level health. This guide shows the three ways to add them.

## When to use which approach

| You want to... | Use |
|---|---|
| Replace an existing CRUD operation with custom logic | Handler override via `FunctionRegistry.RegisterHandler` |
| Add a brand-new path (e.g. `POST /api/v1/pets/_search`) that doesn't map to CRUD | `engine.WithRoutes` (see below) |
| Cross-cut every request (auth, tracing, metrics) | `engine.WithMiddleware` |

All three compose. A custom route can call guards from the registry. A handler override can live alongside middleware. Pick the narrowest tool for the job.

## 1. Override an existing CRUD operation

Handler overrides replace the default service-layer handler for a specific `(schemaName, operation, scope)` triple. When one resolves, the default pipeline (guards → validators → transforms → service call) is bypassed entirely — you own the request.

```go
import (
    "github.com/digitally-rendered/stellar-drive/pkg/core/registry"
)

funcReg := registry.NewFunctionRegistry()

funcReg.RegisterHandler("pet", registry.OpList, registry.Scope{},
    func(w http.ResponseWriter, r *http.Request) {
        // Bypass the generic list, call an external search engine.
        results, err := searchIndex.Query(r.Context(), r.URL.Query().Get("q"))
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(results)
    })

eng := engine.New(cfg, engine.WithFunctionRegistry(funcReg))
```

### Scope narrowing

Scope lets you override only for specific versions or channels:

```go
// Only apply to v2.0.0 over REST.
funcReg.RegisterHandler("pet", registry.OpList,
    registry.Scope{Version: "2.0.0", Channel: "rest"},
    listPetsV2)
```

Resolution prefers the most-specific scope. An empty scope is the fallback.

### Keeping part of the pipeline

A handler override bypasses guards/validators/transforms registered for the **same** operation. If you want to keep them, call them yourself:

```go
funcReg.RegisterHandler("pet", registry.OpCreate, registry.Scope{},
    func(w http.ResponseWriter, r *http.Request) {
        // Re-run guards even though we're overriding the handler.
        for _, g := range funcReg.ResolveGuards("pet", registry.OpCreate, "", "rest") {
            if err := g(r.Context(), r); err != nil {
                http.Error(w, err.Error(), http.StatusForbidden)
                return
            }
        }
        // ...custom create logic...
    })
```

## 2. Add a brand-new route

For endpoints that don't map to a CRUD operation, mount them through `engine.WithRoutes`. Your routes are added to the main Chi router under `cfg.Server.APIPrefix` (e.g. `/api/v1`) and share the built-in middleware stack.

```go
eng := engine.New(cfg,
    engine.WithRoutes(func(r chi.Router) {
        r.Post("/pets/_search", petSearchHandler)
        r.Get("/status", statusHandler)
        r.Mount("/webhooks", webhookRouter())
    }),
)
```

Custom routes are mounted **before** the schema-driven CRUD routes, so `POST /pets/_search` never conflicts with the generic `POST /pets/`.

### Calling into the framework from a custom route

Your custom handler receives the standard `*http.Request`. To reach into stellar-drive's services, inject the things you need at engine construction time:

```go
type searchDeps struct {
    petSvc   port.Service
    policy   port.PolicyEvaluator
}

func buildSearch(deps searchDeps) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := deps.policy.Evaluate(r.Context(), "pet", "list", r); err != nil {
            http.Error(w, err.Error(), http.StatusForbidden)
            return
        }
        // ...
    }
}
```

`engine.New` returns an `*Engine` that exposes accessor methods (`SchemaRegistry()`, `PolicyEvaluator()`, `Container()`) for this pattern. Build your deps after `engine.New` but before `engine.Start`.

## 3. Middleware

Middleware is the right tool when you want to cross-cut every request regardless of path. Authentication, tracing, request counting, tenant resolution all belong here.

```go
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        user, err := authenticate(r)
        if err != nil {
            http.Error(w, "unauthorised", http.StatusUnauthorized)
            return
        }
        ctx := context.WithValue(r.Context(), userKey, user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

eng := engine.New(cfg, engine.WithMiddleware(authMiddleware))
```

Custom middleware is inserted **after** the built-in stack (recovery, request ID, logging, security headers, CORS, rate limit). This means your middleware sees a request that already has a request ID and has passed the rate limiter.

## Patterns

### Webhook receiver

```go
eng := engine.New(cfg,
    engine.WithRoutes(func(r chi.Router) {
        r.Post("/webhooks/stripe", verifyStripeSignature(handleStripeEvent))
    }),
)
```

Webhooks typically need to skip the rate limiter and CORS — pair this with a narrower middleware group using `r.Group`.

### Aggregation endpoint

```go
eng := engine.New(cfg,
    engine.WithRoutes(func(r chi.Router) {
        r.Get("/reports/daily", func(w http.ResponseWriter, r *http.Request) {
            pets, _ := petSvc.List(r.Context(), nil)
            orders, _ := orderSvc.List(r.Context(), nil)
            renderReport(w, pets, orders)
        })
    }),
)
```

### Business-level health check

The default `/healthz` checks storage connectivity. To add business checks:

```go
eng := engine.New(cfg,
    engine.WithRoutes(func(r chi.Router) {
        r.Get("/readyz/business", func(w http.ResponseWriter, r *http.Request) {
            if recentOrderCount(r.Context()) == 0 {
                http.Error(w, "no orders in last 5m", http.StatusServiceUnavailable)
                return
            }
            w.WriteHeader(http.StatusOK)
        })
    }),
)
```

## Related docs

- [Embedding](embedding.md) — using stellar-drive as a library
- [Registry](registry.md) — guards, validators, transforms alongside overrides
- [Middleware](middleware.md) — the built-in middleware stack
- [`examples/petstore/`](../examples/petstore/) — wired reference project
