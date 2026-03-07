# Schema Management Guide

Stellar-drive uses "schema envelopes" to define domain entities, their validation rules, persistence strategy, and metadata. This guide explains how to create, manage, and deploy schemas.

## Envelope Format

A schema envelope is a JSON/YAML document that bundles a JSON Schema with Stellar-drive-specific extensions. Every field is documented below:

### Complete Envelope Structure

```json
{
  "name": "pet",
  "version": "1.0.0",
  "description": "A pet in the Petstore API",
  "storage": "mongo",
  "collection": "pets",
  "schema": {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "$id": "https://example.com/pet.schema.json",
    "type": "object",
    "title": "Pet",
    "properties": {
      "id": {
        "type": "string",
        "description": "Unique identifier"
      },
      "name": {
        "type": "string",
        "minLength": 1,
        "maxLength": 100
      },
      "status": {
        "type": "string",
        "enum": ["available", "pending", "sold"]
      },
      "tags": {
        "type": "array",
        "items": {"type": "string"}
      },
      "price": {
        "type": "number",
        "minimum": 0
      },
      "created_at": {
        "type": "string",
        "format": "date-time"
      }
    },
    "required": ["name", "status"]
  },
  "indexes": [
    {
      "fields": ["name"],
      "unique": false,
      "sparse": false
    },
    {
      "fields": ["status", "created_at"],
      "unique": false,
      "sparse": false
    }
  ],
  "extensions": {
    "x-stellar-audit": true,
    "x-stellar-webhook-events": ["created", "updated", "deleted"],
    "x-stellar-access-level": "public"
  }
}
```

### Field Reference

#### `name` (string, required)

The schema identifier. Used to determine:
- Collection or table name (unless overridden by `collection`)
- URL path prefix: `/api/v1/{name}/` and `/api/v1/{name}/{id}`
- Event type suffix: `pet.created`, `pet.updated`, `pet.deleted`
- Route registration

Constraints:
- Must be unique across all registered schemas
- Must be lowercase alphanumeric with hyphens only: `^[a-z0-9-]+$`
- Recommended: singular noun (e.g., "pet" not "pets")

Example names: `pet`, `user`, `order`, `product-category`

#### `version` (string, required)

Semantic version (semver) identifying this schema revision. Format: `MAJOR.MINOR.PATCH`

Important: The version is NEVER included in the URL. All routes use the schema name only. Version selection happens via:
- Query parameter: `?version=1.2.0`
- Request header: `X-Schema-Version: 1.2.0`
- Default: Latest registered version

Example versions: `1.0.0`, `2.1.3`, `0.1.0-beta`

When a new version is registered:
1. Old routes continue serving old version (if queries use `version` param)
2. Default routes automatically switch to new version
3. Both versions coexist in storage
4. Can be rolled back by specifying version in client requests

#### `description` (string, optional)

Human-readable description of the schema. Appears in:
- OpenAPI schema documentation
- GraphQL schema introspection
- Schema list endpoints

Example: "A pet in the Petstore API. Can be a dog, cat, or other animal."

#### `storage` (string, optional, default: "mongo")

Specifies where data for this schema is persisted:

- `"mongo"`: MongoDB adapter (document-based, flexible schema)
- `"sql"`: SQL adapter (relational, supports PostgreSQL and SQLite)

Each schema independently chooses its storage backend. Example:

```json
{
  "name": "pet",
  "storage": "mongo"  // Uses MongoDB
}
```

```json
{
  "name": "order",
  "storage": "sql"    // Uses PostgreSQL or SQLite
}
```

The storage adapter is selected from the configured connection in `stellar.yaml` and creates tables/collections automatically.

#### `collection` (string, optional)

Override the collection or table name. If not provided, defaults to the schema `name`.

Use cases:
- Backward compatibility: name schema "pet-v2" but store in collection "pets"
- Abbreviation: name schema "inventory-item" but store in table "inv_items"

Example:

```json
{
  "name": "pet-v2",
  "collection": "pets"  // Stores in "pets" collection, not "pet-v2"
}
```

#### `schema` (object, required)

Raw JSON Schema (draft-07 or 2020-12) defining the data structure. This is the JSON Schema specification; Stellar-drive does not alter or wrap it.

Must include:
- `type`: Usually "object" for top-level documents
- `properties`: Field definitions
- `required`: Which properties are mandatory

Can include:
- `$schema`: Dialect identifier (recommended)
- `$id`: Globally unique schema URI
- `title`, `description`: Metadata
- `additionalProperties`: Boolean (allow unknown fields)
- `patternProperties`, `dependentSchemas`: Advanced constraints

Example (Petstore pet):

```json
{
  "schema": {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "properties": {
      "id": {"type": "string"},
      "name": {"type": "string", "minLength": 1},
      "status": {
        "type": "string",
        "enum": ["available", "pending", "sold"]
      },
      "tags": {
        "type": "array",
        "items": {"type": "string"}
      },
      "photoUrls": {
        "type": "array",
        "items": {"type": "string"}
      }
    },
    "required": ["name"]
  }
}
```

#### `indexes` (array, optional)

Index definitions for performance optimization. Each index object includes:

```json
{
  "fields": ["field1", "field2"],  // Array of field names
  "unique": false,                 // Enforce uniqueness
  "sparse": false                  // Null values don't block uniqueness
}
```

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `fields` | array of strings | Column/field names for the index |
| `unique` | boolean | If true, enforce uniqueness on this index |
| `sparse` | boolean | If true, null values are excluded from unique constraint |

Example indexes:

```json
{
  "indexes": [
    {
      "fields": ["name"],
      "unique": false,
      "sparse": false
    },
    {
      "fields": ["email"],
      "unique": true,
      "sparse": false
    },
    {
      "fields": ["status", "created_at"],
      "unique": false,
      "sparse": false
    }
  ]
}
```

Indexes are created automatically when the schema is registered. The repository adapter (MongoDB or SQL) creates the appropriate index structure.

#### `extensions` (object, optional)

Stellar-drive-specific extensions. Keys follow the `x-stellar-*` convention:

```json
{
  "extensions": {
    "x-stellar-audit": true,
    "x-stellar-webhook-events": ["created", "updated", "deleted"],
    "x-stellar-access-level": "public",
    "x-stellar-rate-limit": {
      "requests_per_minute": 100,
      "burst": 10
    }
  }
}
```

| Extension | Type | Description |
|-----------|------|-------------|
| `x-stellar-audit` | boolean | Enable audit logging (record all changes) |
| `x-stellar-webhook-events` | array | Events to dispatch via webhooks |
| `x-stellar-access-level` | string | "public" or "private" |
| `x-stellar-rate-limit` | object | Rate limiting per IP or user |
| `x-stellar-ttl` | string | Auto-expiry duration (e.g., "24h") |

Extensions are optional and don't affect core schema validation.

## Version Selection

Version is NEVER included in URL paths. All routes use the schema name only:

```
POST   /api/v1/pet              # Create pet (uses default/latest version)
GET    /api/v1/pet/{id}          # Get pet
PUT    /api/v1/pet/{id}          # Update pet
DELETE /api/v1/pet/{id}          # Delete pet
```

To target a specific version, use one of these methods:

### Method 1: Query Parameter

```bash
curl http://localhost:8080/api/v1/pet/123?version=1.2.0
```

### Method 2: Request Header

```bash
curl -H "X-Schema-Version: 1.2.0" http://localhost:8080/api/v1/pet/123
```

### Method 3: Default (Latest)

If no version is specified, the latest registered version is used:

```bash
curl http://localhost:8080/api/v1/pet/123  # Uses latest version
```

### Version Resolution Flow

1. Check request header `X-Schema-Version`
2. Check query parameter `version`
3. Use default (latest) version

When multiple versions are registered, the system maintains backward compatibility:

- Old clients can specify `version=1.0.0` and get the 1.0.0 schema
- New clients omit version and get the latest version
- Both versions coexist in storage without conflict

## Schema CRUD API

Schemas are managed via the schema API endpoints (default prefix `/api/v1`):

### Register a Schema

**Endpoint**: `POST /_schemas`

**Request Body**: Schema envelope (JSON)

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d '{
    "name": "pet",
    "version": "1.0.0",
    "description": "A pet in the Petstore API",
    "storage": "mongo",
    "schema": {
      "$schema": "https://json-schema.org/draft/2020-12/schema",
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "status": {"type": "string", "enum": ["available","pending","sold"]}
      },
      "required": ["name"]
    }
  }'
```

**Response** (201 Created):

```json
{
  "data": {
    "name": "pet",
    "version": "1.0.0",
    "description": "A pet in the Petstore API",
    "registered_at": "2024-08-08T10:30:00Z",
    "routes": [
      "POST /api/v1/pet",
      "GET /api/v1/pet",
      "GET /api/v1/pet/{id}",
      "PUT /api/v1/pet/{id}",
      "DELETE /api/v1/pet/{id}"
    ]
  }
}
```

### List All Schemas

**Endpoint**: `GET /_schemas`

Returns the latest version of each registered schema.

```bash
curl http://localhost:8080/api/v1/_schemas
```

**Response**:

```json
{
  "data": [
    {
      "name": "pet",
      "version": "1.0.0",
      "description": "A pet in the Petstore API",
      "storage": "mongo",
      "registered_at": "2024-08-08T10:30:00Z"
    },
    {
      "name": "user",
      "version": "2.1.0",
      "description": "A user account",
      "storage": "sql",
      "registered_at": "2024-08-07T14:20:00Z"
    }
  ],
  "total": 2
}
```

### Get Schema by Name

**Endpoint**: `GET /_schemas/{name}`

Retrieves the latest version of a specific schema.

```bash
curl http://localhost:8080/api/v1/_schemas/pet
```

**Response**:

```json
{
  "data": {
    "name": "pet",
    "version": "1.0.0",
    "description": "A pet in the Petstore API",
    "storage": "mongo",
    "schema": {
      "$schema": "https://json-schema.org/draft/2020-12/schema",
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "status": {"type": "string", "enum": ["available","pending","sold"]}
      },
      "required": ["name"]
    },
    "indexes": [],
    "extensions": {}
  }
}
```

### Update Schema (New Version)

**Endpoint**: `PUT /_schemas/{name}`

Creates a new version of an existing schema. The `name` must match an existing schema.

```bash
curl -X PUT http://localhost:8080/api/v1/_schemas/pet \
  -H "Content-Type: application/json" \
  -d '{
    "name": "pet",
    "version": "1.1.0",
    "description": "A pet in the Petstore API (updated)",
    "storage": "mongo",
    "schema": {
      "$schema": "https://json-schema.org/draft/2020-12/schema",
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "status": {"type": "string", "enum": ["available","pending","sold","adopted"]},
        "breed": {"type": "string"}
      },
      "required": ["name"]
    }
  }'
```

**Response** (201 Created):

```json
{
  "data": {
    "name": "pet",
    "version": "1.1.0",
    "description": "A pet in the Petstore API (updated)",
    "registered_at": "2024-08-08T11:00:00Z",
    "previous_version": "1.0.0",
    "routes_updated": [
      "GET /api/v1/pet (now returns v1.1.0)"
    ]
  }
}
```

### Deactivate Schema

**Endpoint**: `DELETE /_schemas/{name}`

Soft-deletes a schema. The schema definition is marked as inactive but data is preserved.

```bash
curl -X DELETE http://localhost:8080/api/v1/_schemas/pet
```

**Response** (200 OK):

```json
{
  "data": {
    "name": "pet",
    "message": "Schema deactivated",
    "deactivated_at": "2024-08-08T12:00:00Z",
    "data_preserved": true
  }
}
```

After deactivation:
- New routes are not registered
- Existing routes are disabled (return 410 Gone)
- Data is preserved in storage
- Can be re-registered at any time

## Schema Lifecycle

When a schema envelope is POSTed to `POST /_schemas`, the following sequence occurs:

### 1. Envelope Structure Validation

```
Validate required fields: name, version, schema
Validate name format: lowercase alphanumeric + hyphens
Validate version format: semantic versioning (MAJOR.MINOR.PATCH)
Return 400 if invalid
```

### 2. JSON Schema Validation

```
Parse inner schema object
Validate against JSON Schema specification (draft-07 or 2020-12)
Check for required properties definition
Return 400 if invalid schema
```

### 3. Parse into SchemaDefinition

```
Convert SchemaEnvelope to internal SchemaDefinition struct
Resolve all schema references ($ref)
Build property descriptor map
Cache compiled JSON schema validator
```

### 4. Persist to Storage

```
Save envelope to _schemas collection/table
Assign unique schema_id
Record registered_at timestamp
Return 201 Created
```

### 5. Register in In-Memory SchemaRegistry

```
Add SchemaDefinition to registry map by name
Cache validator for fast lookups
Emit schema_registered event
```

### 6. Create CRUD Endpoints Dynamically

```
Register routes:
  POST   /api/v1/{name}           -> Create
  GET    /api/v1/{name}           -> List
  GET    /api/v1/{name}/{id}      -> Read
  PUT    /api/v1/{name}/{id}      -> Update
  DELETE /api/v1/{name}/{id}      -> Delete

Hook handlers into Chi router
Enable middleware for these routes
```

### 7. Create Storage Indexes

```
For each index definition in envelope:
  Create index in MongoDB or SQL database
  Set unique constraint if specified
  Mark as sparse if applicable
```

### 8. Emit Event

```
Fire schema_registered event to EventBus
Notify webhooks if configured
Update telemetry metrics
```

## Distributed Sync

In a multi-instance Stellar-drive deployment, schemas are automatically synchronized:

### Sync Mechanism

1. **Instance A** registers a new schema via `POST /_schemas`
2. **Schema saved** to MongoDB `_schemas` collection
3. **MongoDB change stream** triggers on `_schemas` collection
4. **Instance B** (via change stream listener) detects new schema
5. **Instance B** loads envelope automatically
6. **Instance B** registers routes and creates indexes
7. **Instance B** adds schema to in-memory registry

All instances are now in sync without manual intervention.

### High Availability

This sync mechanism enables:
- Zero-downtime schema deployments
- Cluster-wide consistency
- No direct service-to-service calls
- Self-healing on new instance joins
- Automatic rollback on validation errors

### Change Stream Requirements

- MongoDB 3.6+ (change streams introduced)
- Replication enabled (change streams require a replica set or sharded cluster)
- Sufficient oplog size to capture all schema operations

## Storage Routing

Each schema independently selects its storage backend in the `storage` field:

### Example: Multiple Backends

```json
[
  {
    "name": "pet",
    "storage": "mongo",
    "schema": { ... }
  },
  {
    "name": "order",
    "storage": "sql",
    "schema": { ... }
  },
  {
    "name": "product",
    "storage": "mongo",
    "schema": { ... }
  }
]
```

Configuration in `stellar.yaml`:

```yaml
mongo:
  uri: mongodb://localhost:27017
  database: stellar_db

sql:
  driver: postgres
  dsn: "postgres://user:pass@localhost/stellar_db"
```

Routing logic:
- "pet" and "product" schemas -> MongoDB
- "order" schema -> PostgreSQL
- Unknown backend -> error

This allows:
- Gradual migration (old schemas on MongoDB, new on SQL)
- Schema-specific optimization (documents for "product", relational for "order")
- Cost optimization (slow/cheap storage for archives, fast for active schemas)

## Complete Example: Pet Envelope

Save as `schemas/petstore-pet-v1.0.0.json`:

```json
{
  "name": "pet",
  "version": "1.0.0",
  "description": "A pet in the Petstore API",
  "storage": "mongo",
  "collection": "pets",
  "schema": {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "$id": "https://example.com/petstore/pet.schema.json",
    "type": "object",
    "title": "Pet",
    "description": "A pet object in the store",
    "properties": {
      "id": {
        "type": "string",
        "description": "Unique pet identifier (UUID)"
      },
      "name": {
        "type": "string",
        "description": "Pet's name",
        "minLength": 1,
        "maxLength": 100
      },
      "status": {
        "type": "string",
        "description": "Pet status in the store",
        "enum": ["available", "pending", "sold"]
      },
      "price": {
        "type": ["number", "null"],
        "description": "Price if available for purchase",
        "minimum": 0
      },
      "tags": {
        "type": "array",
        "description": "Tags for the pet",
        "items": {"type": "string"}
      },
      "photoUrls": {
        "type": "array",
        "description": "URLs to pet photos",
        "items": {"type": "string", "format": "uri"}
      },
      "created_at": {
        "type": "string",
        "description": "Creation timestamp",
        "format": "date-time"
      },
      "updated_at": {
        "type": "string",
        "description": "Last update timestamp",
        "format": "date-time"
      }
    },
    "required": ["name", "status"],
    "additionalProperties": false
  },
  "indexes": [
    {
      "fields": ["status"],
      "unique": false,
      "sparse": false
    },
    {
      "fields": ["name"],
      "unique": false,
      "sparse": false
    },
    {
      "fields": ["created_at"],
      "unique": false,
      "sparse": false
    }
  ],
  "extensions": {
    "x-stellar-audit": true,
    "x-stellar-webhook-events": ["created", "updated", "deleted"],
    "x-stellar-access-level": "public"
  }
}
```

Register with:

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d @schemas/petstore-pet-v1.0.0.json
```
