# Schema-Driven Lifecycle

Stellar-Drive is a schema-driven framework. Every API endpoint, service method, validation rule, and storage collection is derived from JSON Schema definitions. This guide traces the full lifecycle from schema file to live API and shows how to customize every stage.

## Lifecycle Overview

```
*.schema.json files
       │
       ▼
  Schema Loading (LoadFromDir / LoadFromFile)
       │
       ▼
  Schema Registry (in-memory, versioned, thread-safe)
       │
       ├──▶ Variant Generation (Document / Create / Update)
       │
       ▼
  Engine Startup
       │
       ├──▶ DI Container (resolves repo, service, handler per schema)
       ├──▶ GenericCRUDService (validation + events + repo)
       ├──▶ FunctionRegistry (guards, validators, transforms, overrides)
       ├──▶ REST Router (Chi routes auto-mounted per schema)
       └──▶ GraphQL Schema (types, queries, mutations, subscriptions)
       │
       ▼
  Live API — requests flow through the pipeline:

  Request → Middleware → Override Check → Guards → Decode Body
          → Validators → Transforms → Service → Repository → Response
```

## Step 1: Schema Files

Schemas live as `*.schema.json` files wrapped in an envelope:

```json
{
  "name": "pet",
  "version": "1.0.0",
  "description": "A pet in the store",
  "storage": "mongo",
  "schema": {
    "type": "object",
    "properties": {
      "name":   { "type": "string" },
      "status": { "type": "string", "enum": ["available", "pending", "sold"] },
      "age":    { "type": "integer" }
    },
    "required": ["name", "status"]
  },
  "indexes": [
    { "fields": ["name"], "unique": true }
  ]
}
```

Key design rule: **version lives in the envelope body**, never in URLs.

## Step 2: Loading and Registration

At engine startup, schemas are loaded in two passes:

1. **Filesystem** — `schema.LoadFromDir()` scans the configured schema directory for `*.schema.json` files and registers each into the in-memory registry.
2. **Database** — schemas persisted in MongoDB's `_schemas` collection are loaded and registered for any names not already present. Filesystem takes precedence.

```go
// Engine startup (simplified)
schema.LoadFromDir(registry, cfg.Schemas.Dir)  // pass 1: filesystem
for _, env := range schemaStore.List(ctx) {    // pass 2: database
    if !registry.Has(env.Name) {
        registry.Register(env)
    }
}
```

The registry stores every version of every schema and maintains a `latest` pointer per name. Version comparison uses semver when possible, falling back to lexicographic ordering.

## Step 3: Variant Generation

From a single schema definition, three **variants** are generated automatically:

| Variant | Purpose | Audit Fields | Required Fields |
|---------|---------|:------------:|:---------------:|
| **Document** | Full stored record | 11 fields appended | As defined |
| **Create** | Input for `POST` | None | As defined |
| **Update** | Input for `PATCH` | None | All optional |

### Document Audit Fields

Every document stored in the database includes these system-managed fields:

| Field | Type | Description |
|-------|------|-------------|
| `entity_id` | `string` | Stable logical ID (survives updates) |
| `record_version` | `int64` | Monotonically increasing (1, 2, 3, ...) |
| `schema_version` | `string` | Schema version at creation time |
| `schema_name` | `string` | Schema name |
| `created_at` | `time.Time` | First version creation timestamp |
| `updated_at` | `time.Time` | This version's timestamp |
| `deleted_at` | `*time.Time` | Non-nil = tombstone (soft delete) |
| `created_by` | `string` | Creator identity |
| `updated_by` | `string` | Last updater identity |
| `deleted_by` | `string` | Deleter identity |
| `etag` | `string` | SHA-256 of the data payload |

### Update Variant: PATCH Semantics

In the Update variant, all fields become optional. Only fields present in the request body are changed. The service layer deep-merges the patch with the existing data before creating a new version.

## Step 4: Append-Only Versioning

Stellar-Drive never mutates documents. Every write creates a new physical record:

```
Create  → new document: EntityID=uuid1, RecordVersion=1
Update  → new document: EntityID=uuid1, RecordVersion=2 (old version untouched)
Delete  → new document: EntityID=uuid1, RecordVersion=3, DeletedAt=now (tombstone)
```

Reads always return the highest `record_version` for a given `entity_id`. Tombstoned entities return `404 Not Found`.

**List queries** use a MongoDB aggregation pipeline:
```
$sort entity_id, record_version DESC
→ $group by entity_id (picks latest)
→ $match deleted_at == null (filter tombstones)
→ $match user filters (QueryDSL)
→ $sort, $project, $facet (pagination)
```

## Step 5: Engine Wiring

The engine is the composition root. At startup it:

1. Connects to MongoDB
2. Creates the default `MongoRepository` and `SchemaStore`
3. Loads schemas (filesystem + database)
4. Optionally starts the hot-reload watcher
5. Creates `GenericCRUDService` (default service for all schemas)
6. Builds the DI container with defaults
7. Assembles the middleware stack
8. Mounts REST routes for every registered schema
9. Optionally mounts GraphQL, health probes, and audit API
10. Starts the HTTP server

### DI Container Resolution

For each schema, the container resolves three components in priority order:

| Component | Resolution Order |
|-----------|-----------------|
| **Repository** | Schema-specific override → Global default → nil |
| **Service** | Schema-specific override → Global default → nil |
| **Handler** | Schema-specific override → nil |

If the service resolves to nil at mount time, the router auto-creates a `GenericCRUDService` backed by the default repository.

```go
// Override the service for a specific schema
container := container.New(
    container.WithDefaultRepository(mongoRepo),
    container.WithDefaultService(defaultService),
    container.WithService("orders", customOrderService),
)
```

## Step 6: The Request Pipeline

Every REST request flows through this pipeline:

### 1. Middleware Chain

```
Recovery → RequestID → Logging → SecurityHeaders → ETag
→ CORS → ContentNegotiation → Cache → Policy → [custom]
```

Each middleware is toggled via configuration. See [middleware.md](middleware.md) for details.

### 2. FunctionRegistry Pipeline

After middleware, the `GenericHandler` executes the FunctionRegistry pipeline:

```
┌─────────────────────────────────────────────────────────┐
│  1. Handler Override  — if registered, runs INSTEAD of  │
│     everything below (full bypass)                      │
├─────────────────────────────────────────────────────────┤
│  2. Guards            — access control checks           │
│     First error → 403 Forbidden, abort                  │
├─────────────────────────────────────────────────────────┤
│  3. Body Decode       — parse JSON request body         │
├─────────────────────────────────────────────────────────┤
│  4. Validators        — business rule validation        │
│     First error → 400 Bad Request, abort                │
├─────────────────────────────────────────────────────────┤
│  5. Transforms        — data enrichment/mutation        │
│     Chained: output of N becomes input of N+1           │
├─────────────────────────────────────────────────────────┤
│  6. Service Call      — schema validation + events      │
│     + repository CRUD                                   │
├─────────────────────────────────────────────────────────┤
│  7. Response          — JSON envelope                   │
└─────────────────────────────────────────────────────────┘
```

### 3. Service Layer

The `GenericCRUDService` wraps every operation with:

1. **Schema validation** — validates input against the JSON Schema definition
2. **Pre-event** — publishes `PreCreate`, `PreUpdate`, etc. (can abort on error)
3. **Repository call** — delegates to the storage adapter
4. **Post-event** — publishes `PostCreate`, `PostUpdate`, etc. (fire-and-forget)

## Custom Overrides

The FunctionRegistry lets you customize behavior at four levels, scoped by schema name, operation, version, and channel.

### Scope and Specificity

Each override is registered with a `Scope`:

```go
type Scope struct {
    Version string // schema version (empty = any)
    Channel string // "rest" or "graphql" (empty = any)
}
```

When multiple overrides match, the most specific wins:

| Score | Condition |
|:-----:|-----------|
| 4 | Both version AND channel match |
| 3 | Only channel matches |
| 2 | Only version matches |
| 1 | Universal (both empty) |

For handlers, only the highest-scoring match runs. For guards, validators, and transforms, **all matching entries run** (sorted by specificity, most specific first).

### Handler Override

Completely replaces the default CRUD handler for an operation:

```go
funcReg.RegisterHandler("pet", registry.OpCreate, registry.Scope{},
    func(w http.ResponseWriter, r *http.Request) {
        // Full control — parse body, call services, write response
        data := map[string]any{"name": "Custom Pet"}
        doc, err := petService.Create(r.Context(), "pet", data)
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        json.NewEncoder(w).Encode(doc)
    },
)
```

### Guards

Run before the body is decoded. Use for access control, rate limiting, or request-level checks:

```go
funcReg.RegisterGuard("pet", registry.OpDelete, registry.Scope{},
    func(ctx context.Context, r *http.Request) error {
        user := middleware.UserFromContext(ctx)
        if user == nil || user["role"] != "admin" {
            return fmt.Errorf("only admins can delete pets")
        }
        return nil
    },
)
```

### Validators

Run after body decode. Use for business rule validation that goes beyond JSON Schema:

```go
funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{},
    func(ctx context.Context, schemaName string, data map[string]any) error {
        name, _ := data["name"].(string)
        if len(name) < 2 {
            return fmt.Errorf("pet name must be at least 2 characters")
        }
        return nil
    },
)
```

### Transforms

Run after validation. Modify or enrich data before it reaches the service layer. Transforms chain — the output of one becomes the input of the next:

```go
funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{},
    func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
        // Normalize the pet name
        if name, ok := data["name"].(string); ok {
            data["name"] = strings.TrimSpace(strings.Title(strings.ToLower(name)))
        }
        // Add server-generated fields
        data["registered_at"] = time.Now().UTC().Format(time.RFC3339)
        return data, nil
    },
)
```

### Channel-Specific Overrides

Register different behavior for REST vs GraphQL:

```go
// Only applies to REST requests
funcReg.RegisterGuard("pet", registry.OpCreate,
    registry.Scope{Channel: "rest"},
    rateLimitGuard,
)

// Only applies to GraphQL requests
funcReg.RegisterValidator("pet", registry.OpCreate,
    registry.Scope{Channel: "graphql"},
    graphqlSpecificValidator,
)
```

### Version-Specific Overrides

Target a specific schema version:

```go
// Only applies when schema version is "2.0.0"
funcReg.RegisterTransform("pet", registry.OpCreate,
    registry.Scope{Version: "2.0.0"},
    v2MigrationTransform,
)
```

## Event Hooks

The event system provides lifecycle hooks at the service layer. See [events.md](events.md) for the full reference.

**Pre-events** can abort operations:

```go
bus.Subscribe(event.PreCreate, "pet", func(ctx context.Context, evt *event.Event) error {
    data := evt.Input.(map[string]any)
    if data["name"] == "forbidden" {
        return fmt.Errorf("this pet name is not allowed")
    }
    return nil
})
```

**Post-events** are fire-and-forget (errors are logged, not propagated):

```go
bus.Subscribe(event.PostCreate, "pet", func(ctx context.Context, evt *event.Event) error {
    doc := evt.Result.(*model.Document)
    log.Printf("Pet created: %s (entity: %s)", doc.Data["name"], doc.EntityID)
    return nil
})
```

## Complete Example: Custom Pet Workflow

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/digitally-rendered/stellar-drive/pkg/config"
    "github.com/digitally-rendered/stellar-drive/pkg/core/event"
    "github.com/digitally-rendered/stellar-drive/pkg/core/model"
    "github.com/digitally-rendered/stellar-drive/pkg/core/registry"
    "github.com/digitally-rendered/stellar-drive/pkg/engine"
)

func main() {
    cfg, _ := config.Load("stellar.yaml")

    // Create a custom function registry
    funcReg := registry.NewFunctionRegistry()

    // Guard: only authenticated users can create pets
    funcReg.RegisterGuard("pet", registry.OpCreate, registry.Scope{},
        func(ctx context.Context, r *http.Request) error {
            if r.Header.Get("Authorization") == "" {
                return fmt.Errorf("authentication required")
            }
            return nil
        },
    )

    // Validator: pet names must be unique-ish
    funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{},
        func(ctx context.Context, schema string, data map[string]any) error {
            name, _ := data["name"].(string)
            if strings.Contains(strings.ToLower(name), "test") {
                return fmt.Errorf("pet names cannot contain 'test'")
            }
            return nil
        },
    )

    // Transform: normalize data before storage
    funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{},
        func(ctx context.Context, schema string, data map[string]any) (map[string]any, error) {
            data["registered_at"] = time.Now().UTC().Format(time.RFC3339)
            return data, nil
        },
    )

    eng := engine.New(cfg,
        engine.WithSchemaDir("./schemas"),
        engine.WithFunctionRegistry(funcReg),
    )

    // Subscribe to post-create events for notifications
    eng.EventBus().Subscribe(event.PostCreate, "pet", func(ctx context.Context, evt *event.Event) error {
        doc := evt.Result.(*model.Document)
        fmt.Printf("New pet registered: %s\n", doc.Data["name"])
        return nil
    })

    eng.Start(context.Background())
}
```

## Related Guides

- [Schema Envelopes](schema-envelopes.md) — envelope format, versioning, indexes
- [Function Registry](registry.md) — detailed override API reference
- [Events](events.md) — all 16 event types and handler semantics
- [Container](container.md) — DI container and service resolution
- [Dynamic Schemas](dynamic-schemas.md) — runtime schema management and hot-reload
- [REST API Reference](api-reference.md) — endpoint reference
- [GraphQL](graphql.md) — GraphQL adapter guide
