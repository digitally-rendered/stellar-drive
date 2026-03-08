# Getting Started

This guide walks you through installing stellar-drive, creating your first schema-driven API, and making requests against it. By the end, you will have a running REST API backed by MongoDB with full CRUD endpoints generated from a JSON Schema envelope.

## Prerequisites

Before you begin, ensure you have the following installed:

- **Go 1.24+**: [Download Go](https://go.dev/dl/)
- **MongoDB 6.0+**: Local or remote instance. Install locally via Homebrew (`brew install mongodb-community`) or use [MongoDB Atlas](https://www.mongodb.com/atlas) for a hosted instance.

Verify your Go installation:

```bash
go version
# go version go1.24.0 darwin/arm64
```

Verify MongoDB is running:

```bash
mongosh --eval "db.runCommand({ ping: 1 })"
# { ok: 1 }
```

## Installation

Install the stellar-drive CLI:

```bash
go install github.com/digitally-rendered/stellar-drive/cmd/stellar-drive@latest
```

Verify the installation:

```bash
stellar-drive version
```

## Initialize a Project

Create a new stellar-drive project:

```bash
stellar-drive init myproject
cd myproject
```

This generates the following structure:

```
myproject/
├── schemas/          # JSON Schema envelope files
├── stellar.yaml      # Configuration file
└── go.mod            # Go module file
```

## Create a Schema

Add a schema for a "pet" resource:

```bash
stellar-drive schema add pet
```

This creates a schema envelope file at `schemas/pet.schema.json`. Edit it to define your data model:

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
      "category": {
        "type": "object",
        "properties": {
          "name": { "type": "string", "example": "Dogs" }
        }
      },
      "photo_urls": {
        "type": "array",
        "items": { "type": "string" }
      },
      "tags": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "name": { "type": "string" }
          }
        }
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

### Schema Envelope Structure

Every schema in stellar-drive is wrapped in an **envelope** that carries metadata alongside the JSON Schema itself:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Logical resource name (used in URLs and collection resolution) |
| `version` | string | Semantic version of the schema definition |
| `description` | string | Human-readable description |
| `storage` | string | Persistence backend (`"mongo"` or `"sql"`) |
| `collection` | string | MongoDB collection or SQL table name |
| `schema` | object | Standard JSON Schema (draft-07) defining the data model |
| `indexes` | array | Database indexes to create on startup |

Version lives in the envelope body, not in URLs. See `docs/schema-envelopes.md` for the full envelope specification.

## Configure

Edit `stellar.yaml` to point at your MongoDB instance and configure the server:

```yaml
project:
  name: myproject
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
  database: myproject
  max_pool_size: 100

middleware:
  cors:
    allowed_origins:
      - "*"
  security_headers: true
  rate_limit:
    requests_per_second: 100
    burst: 50
```

Environment variables with the `STELLAR_` prefix override YAML values. For example:

```bash
export STELLAR_MONGO_URI=mongodb://user:pass@remote-host:27017
export STELLAR_SERVER_PORT=9090
```

See `docs/configuration.md` for the full configuration reference.

## Run the Server

Start stellar-drive:

```bash
stellar-drive run
```

You should see output indicating that schemas were loaded and the server is listening:

```
{"level":"info","msg":"schema registered","schema":"pet","version":"1.0.0"}
{"level":"info","msg":"indexes ensured","schema":"pet","collection":"pets"}
{"level":"info","msg":"server listening","addr":"0.0.0.0:8080"}
```

Stellar-drive automatically generates REST endpoints for every registered schema under the configured `api_prefix`.

## Make API Calls

With the server running, you can interact with the pet API using curl.

### Create a Pet

```bash
curl -X POST http://localhost:8080/api/v1/pet/ \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Buddy",
    "status": "available",
    "photo_urls": ["https://example.com/buddy.jpg"],
    "category": {"name": "Dogs"},
    "tags": [{"name": "friendly"}]
  }'
```

Response (201 Created):

```json
{
  "data": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "Buddy",
    "status": "available",
    "photo_urls": ["https://example.com/buddy.jpg"],
    "category": {"name": "Dogs"},
    "tags": [{"name": "friendly"}],
    "record_version": 1,
    "created_at": "2025-08-01T10:00:00Z",
    "updated_at": "2025-08-01T10:00:00Z"
  }
}
```

### List All Pets

```bash
curl http://localhost:8080/api/v1/pet/
```

Response (200 OK):

```json
{
  "data": [
    {
      "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "name": "Buddy",
      "status": "available",
      "photo_urls": ["https://example.com/buddy.jpg"],
      "record_version": 1,
      "created_at": "2025-08-01T10:00:00Z",
      "updated_at": "2025-08-01T10:00:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0,
  "has_more": false
}
```

You can filter, sort, and paginate list results using the QueryDSL. See `docs/query-dsl.md` for the full syntax.

```bash
# Filter by status, sort by name, limit to 10
curl -G http://localhost:8080/api/v1/pet/ \
  --data-urlencode 'where={"status":{"_eq":"available"}}' \
  --data-urlencode 'sort=name' \
  --data-urlencode 'limit=10'
```

### Get a Single Pet

```bash
curl http://localhost:8080/api/v1/pet/a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

Response (200 OK):

```json
{
  "data": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "Buddy",
    "status": "available",
    "photo_urls": ["https://example.com/buddy.jpg"],
    "category": {"name": "Dogs"},
    "tags": [{"name": "friendly"}],
    "record_version": 1,
    "created_at": "2025-08-01T10:00:00Z",
    "updated_at": "2025-08-01T10:00:00Z"
  }
}
```

### Update a Pet

```bash
curl -X PATCH http://localhost:8080/api/v1/pet/a1b2c3d4-e5f6-7890-abcd-ef1234567890 \
  -H "Content-Type: application/json" \
  -d '{
    "status": "sold"
  }'
```

Response (200 OK):

```json
{
  "data": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "name": "Buddy",
    "status": "sold",
    "photo_urls": ["https://example.com/buddy.jpg"],
    "category": {"name": "Dogs"},
    "tags": [{"name": "friendly"}],
    "record_version": 2,
    "created_at": "2025-08-01T10:00:00Z",
    "updated_at": "2025-08-01T10:05:00Z"
  }
}
```

Updates are append-only: the original version 1 record is preserved in storage. The `record_version` field increments with each update. See `docs/architecture.md` for details on append-only versioning.

### Delete a Pet

```bash
curl -X DELETE http://localhost:8080/api/v1/pet/a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

Response (204 No Content).

Deletes are soft deletes. A new version of the document is created with a `deleted_at` timestamp. The document no longer appears in GET or LIST responses, but the full history is preserved in storage.

## Next Steps

Now that you have a running API, explore the rest of the documentation:

- **[Architecture](architecture.md)** -- Hexagonal design, dependency flow, and request lifecycle
- **[Schema Envelopes](schema-envelopes.md)** -- Full envelope specification, variant generation, and `$ref` resolution
- **[QueryDSL](query-dsl.md)** -- Filtering, sorting, and pagination syntax
- **[Configuration](configuration.md)** -- Complete `stellar.yaml` reference with all options
- **[Events](events.md)** -- Lifecycle event system for custom hooks and audit trails
- **[Errors](errors.md)** -- Domain error types, HTTP status mapping, and error response format
- **[Registry](registry.md)** -- Custom guards, validators, transforms, and handler overrides
- **[Container](container.md)** -- Per-schema dependency injection for repositories, services, and handlers
