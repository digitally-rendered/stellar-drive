# Stellar-Drive

[![CI Status](https://github.com/digitally-rendered/stellar-drive/actions/workflows/ci.yml/badge.svg)](https://github.com/digitally-rendered/stellar-drive/actions)
[![Go Version](https://img.shields.io/github/go-mod/go-version/digitally-rendered/stellar-drive)](https://github.com/digitally-rendered/stellar-drive)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

JSON Schema-driven hexagonal backend with auto-generated CRUD APIs, versioned append-only persistence, and 16 lifecycle events. A Go port of the Python [slip-stream](https://github.com/digitally-rendered/slip-stream) framework.

## Overview

Stellar-Drive turns JSON Schema into live APIs. Drop a schema envelope file in your `schemas/` directory, run `stellar run`, and you have a full CRUD REST API with built-in versioning and query capabilities.

Schemas are first-class runtime objects—post new schemas to the `/_schemas` API to dynamically register them on a running instance. Version metadata lives in the schema envelope, not the URL, making API versioning seamless and backward-compatible.

Every write creates a new version in append-only storage (MongoDB or SQL). Deletes are soft-deletes that mark the latest version as deleted without destroying history. The core domain has zero dependencies on adapters, maintaining clean hexagonal architecture throughout.

## Features

- **Runtime Schema Management** - POST/GET/PUT/DELETE schemas via `/_schemas` API; register new types without restarting
- **Append-Only Versioned Persistence** - Every write creates a new version; soft deletes preserve history; MongoDB and SQL adapters
- **QueryDSL with 18 Operators** - Hasura-style JSON query language: `_eq`, `_neq`, `_gt`, `_gte`, `_lt`, `_lte`, `_in`, `_nin`, `_like`, `_ilike`, `_contains`, `_starts_with`, `_ends_with`, `_exists`, `_is_null`, `_and`, `_or`, `_not`
- **MongoDB Adapter** - Aggregation pipelines, change streams for distributed schema sync, full-text search support
- **SQL Adapter** - Postgres and SQLite dialect abstraction; prepared statements and connection pooling
- **GraphQL Endpoint** - Dynamic schema-driven GraphQL with subscription support
- **Code Generation** - Generates typed Go structs, repository/service/handler wrappers, OpenAPI 3.1 spec, GraphQL SDL
- **16 Lifecycle Events** - Pre/post hooks for create/get/list/update/delete + bulk variants; pre-events can abort operations
- **Webhooks** - HMAC-SHA256 signing, exponential backoff retry, dead-letter queue for failed deliveries
- **JWT Authentication** - Built-in middleware with configurable claims and issuer validation
- **Rate Limiting** - Per-IP token bucket implementation; burst and sustained rates configurable
- **CORS & Security Headers** - Configurable cross-origin access; secure headers included by default
- **Request ID Tracking** - Automatic request ID injection and correlation logging
- **Structured Logging** - `slog` integration for JSON-formatted logs
- **Cobra CLI** - Commands for project init, server run, schema management, code generation, and validation
- **Distributed Schema Sync** - MongoDB change stream watchers propagate schema updates across instances

## Quick Start

### Install

```bash
go install github.com/digitally-rendered/stellar-drive@latest
```

### Initialize a Project

```bash
stellar init my-api
cd my-api
```

This creates a new directory with a starter `stellar.yaml` config and `schemas/` directory.

### Add a Schema

```bash
stellar schema add user
```

Or manually create `schemas/user.schema.json`:

```json
{
  "name": "user",
  "version": "1.0.0",
  "description": "User account",
  "storage": "mongo",
  "collection": "users",
  "schema": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "required": ["name", "email"],
    "properties": {
      "name": { "type": "string" },
      "email": { "type": "string", "format": "email" }
    }
  },
  "indexes": [
    { "fields": ["email"], "unique": true }
  ]
}
```

### Start the Server

```bash
stellar run
```

By default, this connects to MongoDB at `mongodb://localhost:27017` and listens on `:8080`. Override via `stellar.yaml` or flags.

### Use the API

Create a user:

```bash
curl -X POST http://localhost:8080/api/v1/user \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Alice",
    "email": "alice@example.com"
  }'
```

Response:

```json
{
  "entity_id": "65a1b2c3d4e5f6g7h8i9j0k1",
  "version": 1,
  "created_at": "2024-03-04T12:00:00Z",
  "updated_at": "2024-03-04T12:00:00Z",
  "deleted_at": null,
  "data": {
    "name": "Alice",
    "email": "alice@example.com"
  }
}
```

List users:

```bash
curl http://localhost:8080/api/v1/user
```

Get by ID:

```bash
curl http://localhost:8080/api/v1/user/65a1b2c3d4e5f6g7h8i9j0k1
```

Update:

```bash
curl -X PATCH http://localhost:8080/api/v1/user/65a1b2c3d4e5f6g7h8i9j0k1 \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice Smith"}'
```

Delete (soft):

```bash
curl -X DELETE http://localhost:8080/api/v1/user/65a1b2c3d4e5f6g7h8i9j0k1
```

## Schema Envelopes

A schema envelope is a JSON file that bundles the JSON Schema with metadata. Place these in your `schemas/` directory.

```json
{
  "name": "pet",
  "version": "1.0.0",
  "description": "Pet entity from OA3 Petstore",
  "storage": "mongo",
  "collection": "pets",
  "schema": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "required": ["name", "photo_urls"],
    "properties": {
      "name": {
        "type": "string",
        "description": "Name of the pet",
        "example": "doggie"
      },
      "photo_urls": {
        "type": "array",
        "items": { "type": "string" }
      },
      "status": {
        "type": "string",
        "description": "Pet status in the store",
        "enum": ["available", "pending", "sold"],
        "default": "available"
      }
    }
  },
  "indexes": [
    { "fields": ["status"], "unique": false },
    { "fields": ["name"], "unique": false }
  ]
}
```

### Fields

- **name** - Schema identifier; becomes the collection/table name. Must be lowercase alphanumeric + underscore.
- **version** - Semantic version string; used to track schema evolution. Appears in metadata, not in URL.
- **description** - Human-readable description; included in generated OpenAPI and GraphQL docs.
- **storage** - `"mongo"` or `"sql"` (if multiple adapters are enabled).
- **collection** - MongoDB collection name. Ignored if storage is `"sql"`.
- **table** - SQL table name. Ignored if storage is `"mongo"`.
- **schema** - Raw JSON Schema Draft-7. All standard keywords supported (type, required, properties, items, enum, etc.).
- **indexes** - Array of index definitions. Each has `fields` (array of field names) and `unique` (boolean).

## Runtime Schema Management

Register a new schema without restarting:

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d @schemas/review.schema.json
```

List all registered schemas:

```bash
curl http://localhost:8080/api/v1/_schemas
```

Get a specific schema:

```bash
curl http://localhost:8080/api/v1/_schemas/review
```

Get a specific version:

```bash
curl http://localhost:8080/api/v1/_schemas/review?version=1.2.0
```

Or use the header:

```bash
curl -H "X-Schema-Version: 1.2.0" http://localhost:8080/api/v1/_schemas/review
```

Update a schema (increments version if you provide a new one):

```bash
curl -X PATCH http://localhost:8080/api/v1/_schemas/review \
  -H "Content-Type: application/json" \
  -d '{
    "version": "1.2.0",
    "schema": { ... }
  }'
```

Delete a schema (soft delete if in use, error otherwise):

```bash
curl -X DELETE http://localhost:8080/api/v1/_schemas/review
```

## QueryDSL

Stellar-Drive uses a Hasura-style JSON query language. All list endpoints accept a `where` query parameter:

```bash
curl 'http://localhost:8080/api/v1/pet?where={"status":{"_eq":"available"}}'
```

### Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `_eq` | Equal | `{"status": {"_eq": "available"}}` |
| `_neq` | Not equal | `{"status": {"_neq": "sold"}}` |
| `_gt` | Greater than | `{"age": {"_gt": 5}}` |
| `_gte` | Greater or equal | `{"price": {"_gte": 100}}` |
| `_lt` | Less than | `{"age": {"_lt": 10}}` |
| `_lte` | Less or equal | `{"price": {"_lte": 500}}` |
| `_in` | In list | `{"status": {"_in": ["available", "pending"]}}` |
| `_nin` | Not in list | `{"status": {"_nin": ["sold"]}}` |
| `_like` | SQL LIKE | `{"name": {"_like": "Ali%"}}` |
| `_ilike` | Case-insensitive LIKE | `{"name": {"_ilike": "ali%"}}` |
| `_contains` | Contains substring | `{"bio": {"_contains": "engineer"}}` |
| `_starts_with` | Starts with | `{"name": {"_starts_with": "Al"}}` |
| `_ends_with` | Ends with | `{"name": {"_ends_with": "son"}}` |
| `_exists` | Field exists | `{"phone": {"_exists": true}}` |
| `_is_null` | Is null | `{"deleted_at": {"_is_null": true}}` |
| `_and` | Logical AND | `{"_and": [{"age": {"_gte": 18}}, {"status": {"_eq": "active"}}]}` |
| `_or` | Logical OR | `{"_or": [{"status": {"_eq": "available"}}, {"status": {"_eq": "pending"}}]}` |
| `_not` | Logical NOT | `{"_not": {"status": {"_eq": "sold"}}}` |

### Sorting

Sort by one or more fields using the `sort` parameter. Prefix with `-` for descending, `+` or no prefix for ascending:

```bash
curl 'http://localhost:8080/api/v1/pet?sort=-created_at,+name'
```

Equivalent:

```bash
curl 'http://localhost:8080/api/v1/pet?sort=-created_at,name'
```

### Pagination

Use `limit` and `offset`:

```bash
curl 'http://localhost:8080/api/v1/pet?limit=20&offset=40'
```

Cursor-based pagination via `cursor` parameter (if enabled in config):

```bash
curl 'http://localhost:8080/api/v1/pet?limit=20&cursor=eyJpZCI6IjY1YTFiMmMzZDRlNWY2ZzciLCJ2IjoxfQ=='
```

### Full Query Example

```bash
curl 'http://localhost:8080/api/v1/pet?where={"status":{"_eq":"available"},"age":{"_gte":2}}&sort=-created_at&limit=10&offset=0'
```

## Architecture

Stellar-Drive follows the hexagonal architecture pattern:

```
                    ┌─────────────────────────────┐
                    │        Driving Adapters       │
                    │  REST (Chi)  │  GraphQL       │
                    └──────┬───────┴───────┬────────┘
                           │               │
                    ┌──────▼───────────────▼────────┐
                    │         Core Domain           │
                    │  Schema Registry │ QueryDSL    │
                    │  Event Bus │ CRUD Service      │
                    │  Function Registry │ Container │
                    └──────┬───────────────┬────────┘
                           │               │
                    ┌──────▼───────┬───────▼────────┐
                    │       Driven Adapters          │
                    │ MongoDB │ SQL │ Webhooks       │
                    │ Streaming │ Policy │ Telemetry │
                    └────────────────────────────────┘
```

The core domain (`pkg/core/`) has **zero dependencies** on adapters. All adapters implement port interfaces defined in `pkg/core/port/`. This ensures:

- The domain logic is testable without external services
- Adapters can be swapped without changing the core
- Dependencies flow inward; adapters depend on the core, never vice versa

## Configuration

Create a `stellar.yaml` file to configure the server:

```yaml
project:
  name: my-api
  version: "1.0.0"

server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  graceful_shutdown: 15s
  api_prefix: /api/v1

schemas:
  dir: ./schemas
  hot_reload: false

mongo:
  uri: mongodb://localhost:27017
  database: my_api
  max_pool_size: 100

sql:
  driver: postgres          # postgres or sqlite3
  dsn: "postgres://user:pass@localhost/mydb"
  max_open_conns: 25
  max_idle_conns: 5

auth:
  type: jwt
  jwt:
    secret: "your-secret-key"
    issuer: "my-api"
    algorithms:
      - HS256

middleware:
  cors:
    allowed_origins:
      - "http://localhost:3000"
      - "https://app.example.com"
    allowed_methods:
      - GET
      - POST
      - PATCH
      - DELETE
      - OPTIONS
    allowed_headers:
      - Content-Type
      - Authorization
    expose_headers:
      - X-Request-ID
    allow_credentials: true
    max_age: 3600

  security_headers: true

  rate_limit:
    enabled: true
    requests_per_second: 100
    burst: 50

graphql:
  enabled: true
  path: /graphql
  playground: true

webhooks:
  enabled: true
  max_retries: 3
  backoff_multiplier: 2.0
  initial_backoff: 1s
  max_backoff: 60s

streaming:
  enabled: false
  adapter: kafka            # kafka, nats, or redis
  brokers:
    - localhost:9092

telemetry:
  enabled: false
  exporter: json            # json or otel
  endpoint: /metrics

policy:
  enabled: false
  dir: ./policies
```

## CLI Reference

```bash
stellar init <project>
# Initialize a new Stellar-Drive project.
# Flags: --dir (output directory, default current dir)

stellar run
# Start the server.
# Flags: --config (YAML config file, default stellar.yaml)
#        --port (override server.port)

stellar schema add <name>
# Create a new schema template.
# Flags: --template (starter type, default "basic")

stellar schema list
# List all schemas in the schemas directory.
# Flags: --output (json or table, default table)

stellar schema validate <path>
# Validate a schema file against the envelope format.
# Flags: --strict (enforce all optional fields)

stellar generate
# Generate code from schemas: structs, repositories, handlers, OpenAPI, GraphQL.
# Flags: --schemas (input directory, default ./schemas)
#        --output (output directory, default ./generated)
#        --go (generate Go code, default true)
#        --openapi (generate OpenAPI spec, default true)
#        --graphql (generate GraphQL SDL, default true)
```

## Code Generation

Generate typed Go code, OpenAPI specs, and GraphQL schemas:

```bash
stellar generate --schemas ./schemas --output ./generated
```

For each schema, this produces:

- `Document` struct with `EntityID`, `Version`, `CreatedAt`, `UpdatedAt`, `DeletedAt`, `Data`
- `Create` struct with input validation
- `Update` struct with patch semantics
- `Repository` wrapper with type-safe methods
- `Service` wrapper with business logic hooks
- `HTTPHandler` for REST endpoints
- `GraphQLResolver` for GQL queries and mutations

A single `openapi.json` spec covers all schemas and endpoints.

## Event System

Lifecycle events allow you to hook into CRUD operations. 16 events are available:

| Event | When | Can Abort |
|-------|------|-----------|
| `pre_create` | Before insert | Yes |
| `post_create` | After insert | No |
| `pre_get` | Before fetch | Yes |
| `post_get` | After fetch | No |
| `pre_list` | Before list query | Yes |
| `post_list` | After list query | No |
| `pre_update` | Before patch | Yes |
| `post_update` | After patch | No |
| `pre_delete` | Before soft-delete | Yes |
| `post_delete` | After soft-delete | No |
| `pre_bulk_create` | Before bulk insert | Yes |
| `post_bulk_create` | After bulk insert | No |
| `pre_bulk_update` | Before bulk patch | Yes |
| `post_bulk_update` | After bulk patch | No |
| `pre_bulk_delete` | Before bulk delete | Yes |
| `post_bulk_delete` | After bulk delete | No |

Pre-event handlers can return an error to abort the operation. Post-event handlers are fire-and-forget; errors are logged but do not affect the operation.

Register handlers programmatically:

```go
eventBus.Subscribe(event.PreCreate, func(ctx context.Context, e *event.Event) error {
  if _, ok := e.Input.(map[string]any); !ok {
    return fmt.Errorf("invalid input type")
  }
  return nil
})
```

## Development

### Build

```bash
make build
```

Compiles the CLI and all packages.

### Test

```bash
make test          # Unit tests
make test-int      # Integration tests (requires MongoDB)
make test-e2e      # End-to-end tests
make test-all      # All tests
```

### Lint and Vet

```bash
make lint          # golangci-lint
make vet           # go vet
```

### Code Generation

```bash
make generate      # Run all go:generate directives
```

### Coverage

```bash
make coverage      # Generate and open coverage report
```

### Dependencies

```bash
make tidy          # go mod tidy
```

## Project Layout

```
stellar-drive/
├── cmd/                 # CLI commands (Cobra)
├── pkg/
│   ├── adapter/        # Driven adapters (MongoDB, SQL, Webhooks, etc.)
│   │   ├── driven/
│   │   └── driving/
│   ├── codegen/        # Code generation
│   ├── config/         # Configuration parsing
│   ├── core/           # Core domain (no external deps)
│   │   ├── model/
│   │   ├── schema/
│   │   ├── query/      # QueryDSL
│   │   ├── event/      # Event bus
│   │   ├── port/       # Port interfaces
│   │   ├── registry/   # Schema registry
│   │   ├── service/    # CRUD service
│   │   └── container/  # Dependency container
│   ├── engine/         # Server initialization
│   └── vending/        # Vendored deps or utilities
├── schemas/            # Example schema envelopes
├── example/            # Example projects
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Contributing

Contributions are welcome. Please follow the existing code style and include tests for new features.

Run linters before submitting:

```bash
make lint
make vet
make test
```

## License

TBD
