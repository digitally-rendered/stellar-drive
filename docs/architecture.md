# Stellar-Drive Architecture Guide

Stellar-drive is a Go JSON Schema-driven hexagonal (ports and adapters) backend framework. This guide explains the overall structure, design principles, and how components interact.

## Package Structure

```
stellar-drive/
├── cmd/                    # Cobra CLI commands
├── internal/               # Private utilities (stringutil, timeutil, jsonutil)
├── pkg/
│   ├── core/               # Domain layer (NO adapter imports)
│   │   ├── schema/         # SchemaEnvelope, SchemaDefinition, Registry, loader, resolver, validation
│   │   ├── model/          # Document, ListResult, ResponseEnvelope, pagination
│   │   ├── port/           # Interfaces: Repository, Service, EventBus, WebhookDispatcher, etc.
│   │   ├── event/          # Event types (16 lifecycle events), in-memory Bus
│   │   ├── query/          # QueryDSL: FilterNode tree, 18 operators, parser, validator
│   │   ├── service/        # GenericCRUDService (validates, fires events, delegates to repo)
│   │   ├── registry/       # Function registry (guards, validators, transforms, handlers)
│   │   ├── container/      # 4-layer DI container
│   │   └── errors/         # Domain errors (NotFound, Conflict, ValidationFailed, etc.)
│   ├── adapter/
│   │   ├── driving/        # Inbound adapters
│   │   │   ├── rest/       # Chi router, GenericHandler, SchemaAPI, middleware
│   │   │   └── graphql/    # Dynamic GraphQL schema, resolvers, pagination
│   │   └── driven/         # Outbound adapters
│   │       ├── mongo/      # MongoRepository, query translator, schema store
│   │       ├── sql/        # SQLRepository, Postgres/SQLite dialects, migrations
│   │       ├── webhook/    # HMAC dispatcher, retry, dead-letter
│   │       ├── streaming/  # Kafka/NATS/Redis stubs, event bridge
│   │       ├── policy/     # OPA evaluator
│   │       └── telemetry/  # Metrics provider, histogram, exporter
│   ├── codegen/            # JSON Schema -> Go structs, OpenAPI, GraphQL SDL
│   ├── config/             # YAML config loader
│   └── engine/             # Composition root, startup/shutdown
├── schemas/                # Example Petstore schema envelopes
└── stellar.yaml            # Example config
```

## Hexagonal Architecture Principles

Stellar-drive implements the hexagonal (ports and adapters) architecture pattern to maintain clean separation of concerns and testability.

### Core Domain Layer

The `pkg/core/` package is the heart of the framework. It contains:

- **Port Interfaces**: Define contracts for all external interactions (Repository, Service, EventBus, WebhookDispatcher, PolicyEvaluator)
- **Domain Models**: Document, ListResult, ResponseEnvelope, Event types
- **Business Logic**: GenericCRUDService orchestrates validations, events, and repository calls
- **Schema Registry**: Manages SchemaEnvelope definitions, resolution, and validation

**Critical Rule**: Core domain MUST NOT import any adapter package. All dependencies flow inward.

### Driving Adapters (Inbound)

Located in `pkg/adapter/driving/`:

- **REST**: Chi-based HTTP handler that translates HTTP requests into service calls
- **GraphQL**: Dynamic GraphQL schema generator and resolver implementation

These adapters:
- Accept inbound requests from clients
- Translate requests into domain models
- Call the Service port interfaces
- Transform domain responses into protocol-specific formats (JSON, GraphQL)
- Handle middleware, authentication, error formatting

### Driven Adapters (Outbound)

Located in `pkg/adapter/driven/`:

- **MongoDB**: Implements Repository port, handles persistence, query translation
- **SQL**: Implements Repository port for PostgreSQL and SQLite
- **Webhooks**: Implements WebhookDispatcher port with retry and dead-letter handling
- **Streaming**: Kafka/NATS/Redis stubs for event bridge functionality
- **Policy**: OPA integration for policy evaluation
- **Telemetry**: Metrics collection and export

These adapters:
- Implement domain port interfaces
- Are injected into services via the DI container
- Handle all external resource management (database connections, API calls, etc.)
- Translate domain operations into storage-specific syntax

### Engine (Composition Root)

`pkg/engine/` is the only place where driving and driven adapters are wired together with the core domain:

```
Engine
  ├── Load config
  ├── Initialize driven adapters (repositories, webhooks, etc.)
  ├── Wire core services with adapters
  ├── Register driving adapters (REST, GraphQL)
  ├── Register schemas and functions
  └── Start HTTP server
```

This keeps the dependency graph clean: core depends on nothing, adapters depend on core, engine depends on everything.

## Dependency Injection Container

The DI container uses a 4-layer resolution strategy to provide maximum flexibility:

### Resolution Order (Highest to Lowest Priority)

1. **Custom Override** (per-request or per-tenant): Temporary overrides for testing or multi-tenancy
2. **Schema-Specific**: Registered for a particular schema name (e.g., "pet", "user")
3. **Default**: Registered as the fallback for all schemas
4. **Auto-Generated**: Runtime map-based fallback for common interfaces

### Example

```go
// Register a default validator
container.Register("Validator", &DefaultValidator{}, 3) // priority=3 (default)

// Override for "pet" schema
container.Register("Validator", &PetValidator{}, 3, "pet") // priority=3, schema="pet"

// Per-request override
container.RegisterOverride("Validator", customValidator)
```

When resolving `container.Get("Validator", requestCtx, "pet")`:
1. Check if per-request override exists -> use it
2. Check if "pet"-specific registration exists -> use it
3. Check default registration -> use it
4. Fall back to auto-generated -> use it

## Data Persistence: Append-Only Versioning

Stellar-drive uses immutable, append-only persistence to maintain a complete audit trail:

### Create

```
1. Generate new UUID: entity_id
2. Initialize record_version = 1
3. Insert row/document with data
4. Return created_at timestamp
```

### Update

```
1. Read current document by entity_id (latest record_version)
2. Increment record_version by 1
3. Deep-merge new data with current state
4. Insert new row/document (never modify existing)
5. Return merged document with new updated_at
```

### Delete (Soft Delete)

```
1. Read current document by entity_id
2. Increment record_version by 1
3. Insert new row/document with deleted_at timestamp set
4. All future reads/lists filter deleted documents
```

### Read (Single)

```
1. Query by entity_id
2. Retrieve row/document with maximum record_version
3. Filter out if deleted_at is set
4. Return document
```

### List (Multiple)

```
1. Group all rows by entity_id
2. For each entity_id, select row with maximum record_version
3. Filter out rows where deleted_at is set
4. Apply QueryDSL filters and sorting
5. Apply pagination
6. Return ListResult with total count and has_more flag
```

### Indexing

Compound unique index on `(entity_id, record_version)` ensures:
- No two rows have the same entity_id and record_version
- Efficient lookups for "latest version of entity"
- Fast scans when aggregating versions

## Function Registry

The function registry allows registration of custom business logic with granular specificity:

### Registrable Function Types

- **Guards**: Pre-check authorization before operation (returns bool, error)
- **Validators**: Schema-level and operation-level validation
- **Transforms**: Pre-store and post-fetch transformations
- **Handlers**: Custom operation handlers (can replace default CRUD)

### Specificity Scoring

When multiple registrations match, highest score wins:

| Specificity | Score | Example |
|-------------|-------|---------|
| version + channel | 4 | v1.2.0 + CreateEvent for "pet" schema |
| channel only | 3 | CreateEvent for "pet" schema |
| version only | 2 | v1.2.0 for any schema |
| universal | 1 | global handler for all operations |

### Example

```go
// Register universal guard (score=1)
registry.RegisterGuard("OwnershipGuard", nil, nil, ownershipGuard)

// Register for "pet" schema only (score=3)
registry.RegisterGuard("OwnershipGuard", nil, &"pet", petOwnershipGuard)

// Register for v1.0.0 only (score=2)
registry.RegisterGuard("OwnershipGuard", &"1.0.0", nil, v1OwnershipGuard)

// Register for v2.0.0 + "pet" schema (score=4)
registry.RegisterGuard("OwnershipGuard", &"2.0.0", &"pet", v2PetOwnershipGuard)
```

When executing a "pet" schema with version "2.0.0":
- v2PetOwnershipGuard wins (score=4)

When executing a different schema with version "1.0.0":
- v1OwnershipGuard wins (score=2)

## Request/Response Flow

### REST API Request

```
1. HTTP Request arrives at Chi router
2. REST adapter extracts path params, query params, body
3. Adapter calls GenericCRUDService method
4. Service loads schema definition from registry
5. Service resolves guards via DI container
6. Guards check authorization
7. Service resolves validators
8. Validators check data integrity
9. Service delegates to Repository
10. Repository translates to MongoDB/SQL queries
11. Repository persists and returns Document
12. Service fires event to EventBus
13. EventBus notifies webhooks, streaming adapters
14. Service transforms response (post-fetch transforms)
15. Adapter marshals Document into JSON
16. HTTP 200 with ResponseEnvelope
```

### Error Handling

Domain errors (ValidationFailed, NotFound, Conflict, etc.) are caught at the adapter layer and converted to appropriate HTTP status codes:

```
NotFound        -> 404
ValidationFailed -> 422 with field errors
Conflict        -> 409
PermissionDenied -> 403
```

## Event System

The core domain fires 16 lifecycle events:

- **PreCreate**, **PostCreate**
- **PreUpdate**, **PostUpdate**
- **PreDelete**, **PostDelete**
- **PreValidate**, **PostValidate**
- **PreTransform**, **PostTransform**
- **SchemaRegistered**, **SchemaUpdated**, **SchemaDeactivated**
- **ContainerResolved**

Events are handled by:
- WebhookDispatcher (sends HTTP callbacks to configured URLs)
- Streaming adapter (publishes to Kafka/NATS/Redis)
- Telemetry adapter (records metrics)
- Custom handlers registered in the registry

## Schema Distribution

In a multi-instance deployment:

1. Instance A: POST new schema to /_schemas
2. Schema persisted to MongoDB _schemas collection
3. MongoDB change stream triggers on Instance B
4. Instance B loads schema envelope automatically
5. Instance B registers routes and indexes
6. All instances are now in sync

This enables zero-downtime schema updates across a cluster.

## Configuration via stellar.yaml

Every aspect of the framework is configured in `stellar.yaml`:

- Server settings (port, timeouts, CORS, rate limiting)
- Database connections (MongoDB URI, SQL connection string)
- Authentication (JWT secret, issuer)
- Feature flags (GraphQL, webhooks, streaming, telemetry, policy)
- Schema loading (directory, hot reload)

Environment variables with `STELLAR_` prefix override YAML values.

See `docs/configuration.md` for the complete reference.
