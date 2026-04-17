# Dynamic Schema Management

Stellar-Drive supports managing schemas at runtime without server restarts. You can add, update, and remove schemas via the REST API, and the framework automatically generates CRUD endpoints, GraphQL types, and storage collections on the fly.

## Typed vs. Dynamic — pick deliberately

Schemas reach the server from two different pipelines, and the developer experience on the Go side is very different. **Know which path you're on.**

| Path | Typed Go? | Best for |
|---|---|---|
| File on disk + `stellar generate` | Yes — `<Type>Document`, `<Type>Create`, `<Type>Update`, `<Type>Repository`, `<Type>Service`, `<Type>Handler` | Known-at-compile-time domains, IDE autocomplete, compile-time safety, override hooks via `stellar generate --overrides` |
| `POST /_schemas` at runtime | **No** — reads/writes are `map[string]any` wrapped in generic `model.Document` | Tenant-defined schemas, user-supplied extensions, late-bound data shapes |

Dynamic schemas get you working REST + GraphQL endpoints immediately, but there is no typed Go surface: every consumer works with `*model.Document` and `map[string]any`. If you switch a schema from file-based to runtime-registered you lose the generated wrappers until you run `stellar generate` again against a snapshot of the envelope.

**Recommended rule of thumb**: ship the core of your product as file-based schemas (so your codebase stays type-checked) and reserve dynamic registration for genuinely user-defined extensions.

## Runtime Schema Registration

### Register a New Schema

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d '{
    "name": "product",
    "version": "1.0.0",
    "description": "A product in the catalog",
    "storage": "mongo",
    "schema": {
      "type": "object",
      "properties": {
        "title": { "type": "string" },
        "price": { "type": "number" },
        "category": { "type": "string" },
        "in_stock": { "type": "boolean" }
      },
      "required": ["title", "price"]
    },
    "indexes": [
      { "fields": ["title"], "unique": true },
      { "fields": ["category"] }
    ]
  }'
```

**What happens:**

1. The envelope is validated (name, version, and schema body are required)
2. The schema is parsed and registered in the in-memory registry
3. The envelope is persisted to MongoDB's `_schemas` collection (best-effort)
4. CRUD routes are dynamically mounted on the live Chi router
5. The new schema's endpoints are **immediately available**

After this call, you can immediately use:
```bash
# These routes are now live
POST   /api/v1/product/
GET    /api/v1/product/
GET    /api/v1/product/{entityID}
PATCH  /api/v1/product/{entityID}
DELETE /api/v1/product/{entityID}
POST   /api/v1/product/_bulk
PATCH  /api/v1/product/_bulk
DELETE /api/v1/product/_bulk
```

### List All Schemas

```bash
curl http://localhost:8080/api/v1/_schemas
```

Returns all registered schemas with their metadata:

```json
{
  "success": true,
  "data": [
    {
      "name": "pet",
      "version": "1.0.0",
      "description": "A pet in the store",
      "storage": "mongo",
      "active": true
    },
    {
      "name": "product",
      "version": "1.0.0",
      "description": "A product in the catalog",
      "storage": "mongo",
      "active": true
    }
  ]
}
```

### Get a Schema

```bash
# Get latest version
curl http://localhost:8080/api/v1/_schemas/pet

# Get specific version
curl "http://localhost:8080/api/v1/_schemas/pet?version=1.0.0"

# Or via header
curl http://localhost:8080/api/v1/_schemas/pet \
  -H "X-Schema-Version: 1.0.0"
```

Returns the full envelope including the JSON Schema body.

### Update a Schema

```bash
curl -X PUT http://localhost:8080/api/v1/_schemas/pet \
  -H "Content-Type: application/json" \
  -d '{
    "name": "pet",
    "version": "2.0.0",
    "schema": {
      "type": "object",
      "properties": {
        "name":   { "type": "string" },
        "status": { "type": "string", "enum": ["available", "pending", "sold", "adopted"] },
        "age":    { "type": "integer" },
        "breed":  { "type": "string" }
      },
      "required": ["name", "status"]
    }
  }'
```

This registers the new version. Both versions coexist in the registry. The `latest` pointer is updated if the new version is newer (semver comparison).

### Delete a Schema

```bash
curl -X DELETE http://localhost:8080/api/v1/_schemas/pet
```

Removes the schema from the in-memory registry and the persistence store. Existing data in the collection is not deleted.

## Schema Hot-Reload

When enabled, Stellar-Drive watches the schema directory for file changes and automatically reloads modified schemas.

### Enabling Hot-Reload

```yaml
# stellar.yaml
schemas:
  dir: ./schemas
  hot_reload: true
```

Or via environment:

```bash
STELLAR_SCHEMAS_HOT_RELOAD=true stellar-drive run
```

### How It Works

The watcher uses a polling mechanism (not filesystem events) for maximum portability:

1. **Poll interval**: Scans the directory every 500ms for file changes (mtime + size comparison)
2. **Debounce**: Rapid successive writes to the same file are collapsed — waits 500ms after the last write before processing
3. **New/modified files**: Loaded and registered into the in-memory registry
4. **Deleted files**: Schema is removed from the registry

### Configuring the Watcher

```go
// Programmatic configuration
eng := engine.New(cfg,
    engine.WithSchemaDir("./schemas"),
)

// The watcher is started automatically when hot_reload is true
```

The watcher options (poll interval, debounce delay) can be customized when constructing the watcher programmatically:

```go
watcher := schema.NewSchemaWatcher(dir, registry,
    schema.WithPollInterval(1 * time.Second),
    schema.WithDebounce(1 * time.Second),
    schema.WithOnReload(func(name, version string) {
        log.Printf("Schema reloaded: %s@%s", name, version)
    }),
)
watcher.Start(ctx)
```

### Hot-Reload Scope

The watcher updates the **in-memory registry** only. It does not dynamically mount new HTTP routes — that is handled by the Schema Management API's `RegisterSchema` endpoint. For new schema files added while the server is running, use the REST API to register them if you need immediate route availability.

Modified existing schemas are reloaded and the updated definition is used for subsequent requests (validation, codegen lookups, etc.).

## Schema Versioning

### Version Resolution

The registry supports multiple versions of the same schema:

```json
{ "name": "pet", "version": "1.0.0", "schema": { ... } }
{ "name": "pet", "version": "2.0.0", "schema": { ... } }
```

- **API requests** use the latest version for validation by default
- **Version selection** is available via query parameter or header:
  - `?version=1.0.0`
  - `X-Schema-Version: 1.0.0`

### Version Comparison

Versions are compared using:
1. **Semver** (if the version string is valid semver, e.g., `1.0.0`, `2.1.3`)
2. **Lexicographic** fallback (for non-semver strings like `draft`, `beta`)

The `latest` pointer is only updated when a strictly newer version is registered.

### Schema Evolution Strategy

Since Stellar-Drive uses append-only versioning for documents, schema evolution is straightforward:

1. **Adding fields**: New fields are optional by default. Old documents missing these fields are valid.
2. **Removing fields**: Remove from the schema. Old documents with extra fields are preserved in storage but not validated.
3. **Changing types**: Register a new schema version. Use transforms to migrate data on read/write.

```go
// Transform that migrates data from v1 to v2 format
funcReg.RegisterTransform("pet", registry.OpCreate,
    registry.Scope{Version: "2.0.0"},
    func(ctx context.Context, schema string, data map[string]any) (map[string]any, error) {
        // v2 split "name" into "first_name" and "last_name"
        if name, ok := data["name"].(string); ok {
            parts := strings.SplitN(name, " ", 2)
            data["first_name"] = parts[0]
            if len(parts) > 1 {
                data["last_name"] = parts[1]
            }
            delete(data, "name")
        }
        return data, nil
    },
)
```

## Per-Schema Storage Routing

Each schema can be individually mapped to a different storage backend (MongoDB or SQL).

### Configuration Methods

**Method 1: In the schema envelope file**

```json
{
  "name": "orders",
  "version": "1.0.0",
  "storage": "sql",
  "schema": { ... }
}
```

**Method 2: In the project config**

```yaml
# stellar.yaml
storage:
  default: mongo
  schemas:
    orders: sql
    analytics: sql
```

**Method 3: Programmatic**

```go
storageCfg := storage.NewConfig(storage.WithDefault(storage.Mongo))
storageCfg.Set("orders", storage.SQL)
storageCfg.Set("analytics", storage.SQL)
```

### Resolution Priority

1. Explicit override in `storage.schemas` config map
2. `storage` field in the schema envelope itself
3. Project-wide default from `storage.default`

### Example: Mixed Storage

```yaml
# stellar.yaml
storage:
  default: mongo

mongo:
  uri: mongodb://localhost:27017
  database: stellar_drive

sql:
  driver: postgres
  dsn: "host=localhost user=app dbname=stellar sslmode=disable"
```

```
schemas/
  pet.schema.json        → storage: "mongo"  (default)
  user.schema.json       → storage: "mongo"  (default)
  order.schema.json      → storage: "sql"    (envelope override)
  analytics.schema.json  → storage: "sql"    (config override)
```

Each backend handles its own CRUD, versioning, and query translation independently.

## Distributed Schema Sync

For multi-instance deployments, schemas registered on one instance need to be visible on others.

### MongoDB Change Streams

The `MongoSchemaStore` implements a `Watch()` method that returns a channel of `SchemaEvent` values using MongoDB change streams:

```go
type Store interface {
    Save(ctx context.Context, envelope *SchemaEnvelope) error
    Get(ctx context.Context, name, version string) (*SchemaEnvelope, error)
    List(ctx context.Context) ([]*SchemaEnvelope, error)
    ListVersions(ctx context.Context, name string) ([]*SchemaEnvelope, error)
    Delete(ctx context.Context, name string) error
    Watch(ctx context.Context) (<-chan SchemaEvent, error)
}
```

The `SchemaEvent` type carries the operation (insert, update, delete) and the affected envelope. Consuming instances can subscribe to the watch channel to keep their in-memory registries synchronized.

### Sync Flow

```
Instance A: POST /_schemas (registers "product")
       │
       ├── registry.Register(envelope)     ← in-memory
       ├── schemaStore.Save(ctx, envelope) ← MongoDB _schemas collection
       │
       ▼
MongoDB Change Stream fires
       │
       ▼
Instance B: schemaStore.Watch(ctx) ← receives SchemaEvent
       │
       └── registry.Register(envelope) ← in-memory sync
```

## `$ref` Resolution

Schemas can reference each other using `$ref`:

### Internal References

Reference a definition within the same schema:

```json
{
  "schema": {
    "type": "object",
    "properties": {
      "address": { "$ref": "#/definitions/Address" }
    },
    "definitions": {
      "Address": {
        "type": "object",
        "properties": {
          "city": { "type": "string" },
          "zip": { "type": "string" }
        }
      }
    }
  }
}
```

### Cross-Schema References

Reference another registered schema:

```json
{
  "name": "order",
  "schema": {
    "type": "object",
    "properties": {
      "pet": { "$ref": "pet#/definitions/PetSummary" },
      "customer": { "$ref": "user" }
    }
  }
}
```

Three `$ref` patterns are supported:

| Pattern | Meaning |
|---------|---------|
| `#/definitions/Foo` | Walk JSON Pointer within current schema |
| `SchemaName#/definitions/Foo` | Look up another schema, walk the pointer |
| `SchemaName` | Resolve to the entire body of another schema |

References are resolved recursively up to a depth of 32. Circular references are detected and prevented.

### Viewing the Dependency Graph

Use the MCP tool or inspect programmatically:

```bash
# Via MCP
stellar-drive mcp
# Then call: tools/call with name="get_schema_dag"
```

## CLI Commands for Schema Management

### Scaffold a New Schema

```bash
stellar-drive schema add product
```

Creates `schemas/product.schema.json` with a minimal template:

```json
{
  "name": "product",
  "version": "1.0.0",
  "storage": "mongo",
  "schema": {
    "type": "object",
    "properties": {
      "name": { "type": "string" }
    },
    "required": ["name"]
  },
  "indexes": [
    { "fields": ["name"], "unique": true }
  ]
}
```

### List Schemas

```bash
stellar-drive schema list
```

```
NAME      VERSION  STORAGE  STATUS   UPDATED
----      -------  -------  ------   -------
order     1.0.0    sql      active   2024-01-15T10:00:00Z
pet       1.0.0    mongo    active   2024-01-15T09:00:00Z
product   1.0.0    mongo    active   2024-01-15T11:00:00Z
```

Unparseable files show `PARSE ERROR: ...` as their status.

### Validate Schemas

```bash
# Validate all
stellar-drive schema validate

# Validate one
stellar-drive schema validate pet
```

```
OK   pet@1.0.0
OK   order@1.0.0
FAIL product@1.0.0: required field "name" missing from properties

2 passed, 1 failed
```

Returns non-zero exit code if any schema fails — useful in CI pipelines.

### Validation Rules

`ValidateEnvelope()` checks:
- Non-empty `name`
- Non-empty `version`
- Non-nil `schema` body

`ValidateData()` checks (against a schema definition):
- Required fields are present
- Field types match JSON Schema types
- Enum values are valid
- String format constraints (date-time, email, etc.)
- Numeric range constraints (minimum, maximum)

## Schema Lifecycle Summary

```
                    ┌─────────────────────────────┐
                    │   Schema Sources             │
                    │                               │
                    │  *.schema.json files          │
                    │  MongoDB _schemas collection  │
                    │  REST API POST /_schemas      │
                    │  MCP create_schema tool       │
                    └───────────┬───────────────────┘
                                │
                                ▼
                    ┌─────────────────────────────┐
                    │  Schema Registry (in-memory) │
                    │  Thread-safe, versioned      │
                    │  Latest pointer per name     │
                    └───────────┬───────────────────┘
                                │
                    ┌───────────┼───────────┐
                    ▼           ▼           ▼
               REST Routes  GraphQL    Validation
               (auto-mounted) Schema   (per-request)
                            (auto-gen)
```

Schemas can enter the registry from four sources. Once registered, they drive the entire API surface automatically.

## Related Guides

- [Schema Envelopes](schema-envelopes.md) — envelope format and field reference
- [Schema Lifecycle](schema-lifecycle.md) — end-to-end request pipeline
- [MCP Server](mcp-server.md) — AI-accessible schema tools
- [Code Generation](codegen.md) — generating typed Go code from schemas
- [Configuration](configuration.md) — storage routing and hot-reload config
