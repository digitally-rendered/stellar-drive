# DI Container

The dependency injection container provides per-schema resolution of repositories, services, and HTTP handlers. It enables schema-specific overrides while providing sensible global defaults, ensuring that each schema can have its own persistence backend, business logic, or custom HTTP handling without affecting others.

The container lives in `pkg/core/container/`.

## Overview

The container resolves three dependency types:

| Dependency | Interface | Purpose |
|------------|-----------|---------|
| **Repository** | `port.Repository` | Storage adapter (MongoDB, SQL) for document persistence |
| **Service** | `port.Service` | Business logic layer (validation, events, delegation to repository) |
| **Handler** | `http.Handler` | Custom HTTP handler that replaces default CRUD handling entirely |

Additionally, the container holds an optional `EventBus` reference accessible to all consumers.

## Resolution Priority

When resolving a dependency for a given schema name, the container follows this order:

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (highest) | Schema-specific override | Registered via `RegisterRepository`, `RegisterService`, or `RegisterHandler` |
| 2 | Global default | Set via `WithDefaultRepository` or `WithDefaultService` |
| 3 (lowest) | nil | No dependency available; caller must handle the absent dependency |

For handlers, there is no global default -- a handler is either registered for a specific schema or absent.

## Construction

Create a container using `container.New()` with functional options:

```go
c := container.New(
    container.WithDefaultRepository(mongoRepo),
    container.WithDefaultService(crudService),
    container.WithEventBus(eventBus),
)
```

Options are applied in order, so later options override earlier ones.

## Functional Options

### WithDefaultRepository

Sets the fallback repository used when no schema-specific repository has been registered.

```go
container.WithDefaultRepository(repo port.Repository) Option
```

Example:

```go
mongoRepo := mongo.NewRepository(db)
c := container.New(
    container.WithDefaultRepository(mongoRepo),
)

// All schemas resolve to mongoRepo unless overridden.
repo := c.ResolveRepository("pet")    // returns mongoRepo
repo = c.ResolveRepository("order")   // returns mongoRepo
```

### WithDefaultService

Sets the fallback service used when no schema-specific service has been registered.

```go
container.WithDefaultService(svc port.Service) Option
```

Example:

```go
crudSvc := service.NewGenericCRUD(schemaRegistry, mongoRepo, eventBus)
c := container.New(
    container.WithDefaultService(crudSvc),
)
```

### WithEventBus

Sets the EventBus on the container, making it accessible via `c.EventBus()`.

```go
container.WithEventBus(bus port.EventBus) Option
```

Example:

```go
bus := event.NewBus()
c := container.New(
    container.WithEventBus(bus),
)

// Later, retrieve the bus:
eventBus := c.EventBus()
```

### WithRepository

Registers a schema-specific repository override at construction time. Takes precedence over the default repository for the named schema.

```go
container.WithRepository(schemaName string, repo port.Repository) Option
```

Example:

```go
c := container.New(
    container.WithDefaultRepository(mongoRepo),
    container.WithRepository("audit_log", sqlRepo), // audit_log uses SQL instead
)
```

### WithService

Registers a schema-specific service override at construction time. Takes precedence over the default service for the named schema.

```go
container.WithService(schemaName string, svc port.Service) Option
```

Example:

```go
c := container.New(
    container.WithDefaultService(defaultSvc),
    container.WithService("pet", petService), // pet uses custom service
)
```

## Resolve Methods

### ResolveRepository

Returns the repository for a schema name. Falls back to the default repository when no schema-specific override has been registered. Returns nil when neither is set.

```go
func (c *Container) ResolveRepository(schemaName string) port.Repository
```

Example:

```go
repo := c.ResolveRepository("pet")
if repo == nil {
    return errors.Internal("no repository configured for schema", nil)
}

doc, err := repo.Create(ctx, "pet", data)
```

### ResolveService

Returns the service for a schema name. Falls back to the default service when no schema-specific override has been registered. Returns nil when neither is set.

```go
func (c *Container) ResolveService(schemaName string) port.Service
```

Example:

```go
svc := c.ResolveService("pet")
if svc == nil {
    return errors.Internal("no service configured for schema", nil)
}

doc, err := svc.Create(ctx, "pet", input)
```

### ResolveHandler

Returns the custom HTTP handler for a schema name, if one has been registered. The second return value is `false` when no handler is set.

```go
func (c *Container) ResolveHandler(schemaName string) (http.Handler, bool)
```

When a handler is registered for a schema, it **completely replaces** the default CRUD HTTP handling. The handler receives the raw `http.Request` and is responsible for all response writing, validation, and error handling.

Example:

```go
if handler, ok := c.ResolveHandler("pet"); ok {
    // Custom handler takes over -- default CRUD is not used.
    handler.ServeHTTP(w, r)
    return
}

// No custom handler; use default CRUD pipeline.
svc := c.ResolveService("pet")
```

## Runtime Registration

Dependencies can be registered after construction. This is useful for dynamically loaded schemas or runtime reconfiguration.

### RegisterRepository

Registers a schema-specific repository override. Subsequent `ResolveRepository` calls for the same schema name will use the new value.

```go
func (c *Container) RegisterRepository(schemaName string, repo port.Repository)
```

Example:

```go
// Schema "invoice" loaded at runtime; register its dedicated SQL repository.
sqlRepo := sql.NewRepository(pgDB, "invoices")
c.RegisterRepository("invoice", sqlRepo)
```

### RegisterService

Registers a schema-specific service override.

```go
func (c *Container) RegisterService(schemaName string, svc port.Service)
```

Example:

```go
// Pet schema needs custom validation logic.
petSvc := service.NewGenericCRUD(schemaReg, petRepo, eventBus, service.WithStrictValidation())
c.RegisterService("pet", petSvc)
```

### RegisterHandler

Registers a custom HTTP handler for a schema name. When set, the handler completely replaces the default CRUD HTTP handling for that schema.

```go
func (c *Container) RegisterHandler(schemaName string, h http.Handler)
```

Example:

```go
// Register a custom handler that serves pet data from a cache.
c.RegisterHandler("pet", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Custom implementation: read from cache, bypass service/repo.
    data, err := petCache.Get(r.Context(), r.URL.Path)
    if err != nil {
        http.Error(w, "not found", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
}))
```

## Accessors

### EventBus

Returns the EventBus configured on this container, or nil if none was set.

```go
func (c *Container) EventBus() port.EventBus
```

### DefaultRepository

Returns the global fallback repository.

```go
func (c *Container) DefaultRepository() port.Repository
```

## Thread Safety

`Container` uses `sync.RWMutex` internally. All resolve, register, and accessor methods are safe for concurrent use. Resolution acquires a read lock; registration acquires a write lock.

## Typical Usage in Engine Startup

The container is created during engine startup and wired with all adapters:

```go
func buildContainer(cfg *config.Config) *container.Container {
    // Create driven adapters.
    mongoRepo := mongo.NewRepository(mongoClient, cfg.Mongo.Database)
    bus := event.NewBus()

    // Create default service.
    schemaReg := schema.NewRegistry()
    defaultSvc := service.NewGenericCRUD(schemaReg, mongoRepo, bus)

    // Build container with defaults.
    c := container.New(
        container.WithDefaultRepository(mongoRepo),
        container.WithDefaultService(defaultSvc),
        container.WithEventBus(bus),
    )

    return c
}
```

## Multi-Database Example

Use the container to route different schemas to different persistence backends:

```go
func buildMultiDBContainer() *container.Container {
    // MongoDB for document-oriented schemas.
    mongoRepo := mongo.NewRepository(mongoClient, "stellar_docs")

    // PostgreSQL for relational schemas.
    sqlRepo := sql.NewRepository(pgDB)

    // Default everything to MongoDB.
    c := container.New(
        container.WithDefaultRepository(mongoRepo),
        container.WithDefaultService(service.NewGenericCRUD(reg, mongoRepo, bus)),
    )

    // Override specific schemas to use SQL.
    c.RegisterRepository("order", sqlRepo)
    c.RegisterRepository("invoice", sqlRepo)

    // Custom service for orders that uses the SQL repo.
    orderSvc := service.NewGenericCRUD(reg, sqlRepo, bus)
    c.RegisterService("order", orderSvc)

    return c
}
```

With this setup:

- `ResolveRepository("pet")` returns `mongoRepo` (default)
- `ResolveRepository("order")` returns `sqlRepo` (schema-specific override)
- `ResolveRepository("invoice")` returns `sqlRepo` (schema-specific override)
- `ResolveService("pet")` returns the default MongoDB-backed service
- `ResolveService("order")` returns the custom SQL-backed order service

## Custom Handler Override Example

Replace the default CRUD handling entirely for a specific schema:

```go
func registerLegacyPetHandler(c *container.Container) {
    legacyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            // Custom GET logic that reads from a legacy system.
            legacyData := legacyPetSystem.Fetch(r.Context(), r.URL.Path)
            json.NewEncoder(w).Encode(legacyData)
        case http.MethodPost:
            // Custom POST logic with legacy validation rules.
            var input map[string]any
            json.NewDecoder(r.Body).Decode(&input)
            result := legacyPetSystem.Create(r.Context(), input)
            w.WriteHeader(http.StatusCreated)
            json.NewEncoder(w).Encode(result)
        default:
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
    })

    c.RegisterHandler("pet", legacyHandler)
}
```

When a custom handler is registered, the REST adapter detects it via `ResolveHandler` and routes all requests for that schema directly to the handler, skipping the service layer, event bus, and all middleware hooks.

## Related Documentation

- **[Architecture](architecture.md)** -- 4-layer DI container in the hexagonal design
- **[Registry](registry.md)** -- Function registry for guards, validators, transforms (complementary to the container)
- **[Events](events.md)** -- EventBus stored in the container and shared across services
- **[Configuration](configuration.md)** -- Database connection settings used when constructing repositories
