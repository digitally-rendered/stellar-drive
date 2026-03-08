# GraphQL Adapter

Stellar-Drive auto-generates a full GraphQL API from your registered schemas. Queries, mutations, bulk operations, and real-time subscriptions are all derived at startup — no SDL files to maintain.

## Enabling GraphQL

```yaml
# stellar.yaml
graphql:
  enabled: true
  path: /graphql        # default
  playground: false     # enable GraphiQL playground
```

Or via environment variable:

```bash
STELLAR_GRAPHQL_ENABLED=true stellar-drive run
```

The GraphQL endpoint accepts both `GET` and `POST` requests.

## Auto-Generated Schema

For each registered schema, Stellar-Drive generates:

### Object Type

User-defined fields plus 8 audit fields:

```graphql
type Pet {
  # User-defined fields
  name: String
  status: String
  age: Int

  # Audit fields (always present)
  id: String
  entity_id: String
  record_version: Int
  schema_version: String
  created_at: DateTime
  updated_at: DateTime
  deleted_at: DateTime
  etag: String
}
```

### Queries

```graphql
type Query {
  # Get a single entity by ID
  getPet(entityId: ID!): Pet

  # List entities with optional filtering, sorting, and pagination
  listPets(
    where: JSON       # QueryDSL filter (Hasura-style)
    sort: String      # e.g. "+name,-created_at"
    limit: Int        # page size (1-100, default 20)
    offset: Int       # page offset
  ): PetListResult!
}
```

### Mutations

```graphql
type Mutation {
  # Single operations
  createPet(input: JSON!): Pet!
  updatePet(entityId: ID!, input: JSON!): Pet!
  deletePet(entityId: ID!): Boolean!

  # Bulk operations
  bulkCreatePet(inputs: [JSON!]!): PetBulkResult!
  bulkUpdatePet(items: [JSON!]!): PetBulkResult!
  bulkDeletePet(ids: [ID!]!): Boolean!
}
```

### Subscriptions

```graphql
type Subscription {
  onPetCreated: Pet
  onPetUpdated: Pet
  onPetDeleted: Pet
}
```

### Result Types

```graphql
type PetListResult {
  items: [Pet!]!
  total: Int!
  hasMore: Boolean!
}

type PetBulkResult {
  items: [Pet!]!
  succeeded: Int!
  failed: Int!
}
```

## Type Mapping

JSON Schema types are mapped to GraphQL types:

| JSON Schema | Format | GraphQL Type |
|-------------|--------|-------------|
| `string` | — | `String` |
| `string` | `date-time` | `DateTime` |
| `integer` | — | `Int` |
| `number` | — | `Float` |
| `boolean` | — | `Boolean` |
| `array` | — | `[ItemType]` |
| `object` (with properties) | — | Named inline type |
| `object` (no properties) | — | `JSON` |

The `JSON` scalar is a pass-through type that accepts and returns arbitrary JSON values. It is used for dynamic objects and as the input type for mutations.

Nested objects with defined properties get their own named GraphQL type prefixed with `NestedObject_`:

```graphql
# From schema: { "address": { "type": "object", "properties": { "city": ... } } }
type NestedObject_address {
  city: String
  zip: String
}
```

## Usage Examples

### Query: Get by ID

```graphql
query {
  getPet(entityId: "abc-123") {
    name
    status
    entity_id
    record_version
    created_at
  }
}
```

### Query: List with Filters

```graphql
query {
  listPets(
    where: { status: { _eq: "available" }, age: { _gt: 2 } }
    sort: "-created_at"
    limit: 10
    offset: 0
  ) {
    items {
      name
      status
      age
      entity_id
    }
    total
    hasMore
  }
}
```

The `where` argument accepts the full [QueryDSL](query-dsl.md) filter syntax as a JSON object.

### Mutation: Create

```graphql
mutation {
  createPet(input: {
    name: "Buddy"
    status: "available"
    age: 3
  }) {
    entity_id
    name
    record_version
    created_at
  }
}
```

### Mutation: Update

```graphql
mutation {
  updatePet(
    entityId: "abc-123"
    input: { status: "sold" }
  ) {
    entity_id
    status
    record_version
    updated_at
  }
}
```

### Mutation: Delete

```graphql
mutation {
  deletePet(entityId: "abc-123")
}
```

Returns `true` on success.

### Mutation: Bulk Create

```graphql
mutation {
  bulkCreatePet(inputs: [
    { name: "Buddy", status: "available", age: 3 },
    { name: "Max", status: "pending", age: 1 },
    { name: "Luna", status: "available", age: 5 }
  ]) {
    items {
      entity_id
      name
    }
    succeeded
    failed
  }
}
```

### Mutation: Bulk Update

```graphql
mutation {
  bulkUpdatePet(items: [
    { entity_id: "abc-123", data: { status: "sold" } },
    { entity_id: "def-456", data: { status: "pending" } }
  ]) {
    items {
      entity_id
      status
      record_version
    }
    succeeded
    failed
  }
}
```

Note: bulk update items require `entity_id` and `data` as nested keys.

### Mutation: Bulk Delete

```graphql
mutation {
  bulkDeletePet(ids: ["abc-123", "def-456", "ghi-789"])
}
```

## Subscriptions

Subscriptions use the internal event bus to deliver real-time updates. Three subscription fields are generated per schema for create, update, and delete events.

### How Subscriptions Work

1. When a client subscribes, a buffered channel (capacity 16) is created.
2. An event handler is registered on the bus for the corresponding event type.
3. When a matching event fires, the document is sent to the channel.
4. If the channel is full (slow consumer), the event is **dropped** with a warning log.
5. When the client disconnects, the channel is closed.

### Subscription Example

```graphql
subscription {
  onPetCreated {
    entity_id
    name
    status
    created_at
  }
}
```

This subscription fires whenever any pet is created, returning the full document.

```graphql
subscription {
  onPetUpdated {
    entity_id
    name
    status
    record_version
    updated_at
  }
}
```

### Transport Considerations

The GraphQL subscription fields are wired internally via the event bus. To expose them to clients, a WebSocket or Server-Sent Events (SSE) transport layer is needed. The current HTTP handler supports standard `GET`/`POST` query execution but does not include a WebSocket upgrade path. For production real-time use, integrate a WebSocket transport or use the [streaming adapter](streaming.md) for server-side event delivery.

## HTTP Request Format

### POST (recommended)

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query { listPets(limit: 5) { items { name status } total } }",
    "variables": {},
    "operationName": null
  }'
```

### GET

```bash
curl "http://localhost:8080/graphql?query=\{listPets(limit:5)\{items\{name\}total\}\}"
```

Variables can be passed as a JSON-encoded query parameter:

```
/graphql?query=...&variables={"status":"available"}&operationName=GetPets
```

## Response Format

Standard GraphQL response envelope:

```json
{
  "data": {
    "listPets": {
      "items": [
        { "name": "Buddy", "status": "available" }
      ],
      "total": 1,
      "hasMore": false
    }
  }
}
```

### Errors

```json
{
  "data": null,
  "errors": [
    {
      "message": "pet with id \"xyz\" not found",
      "locations": [{ "line": 2, "column": 3 }],
      "path": ["getPet"]
    }
  ]
}
```

HTTP status is `200` for successful queries (even with partial errors). Only returns `400` when `data` is nil and errors are present.

## Resolvers and the Service Layer

All GraphQL resolvers delegate to the same `port.Service` interface used by REST. This means:

- The same validation, event hooks, and business logic apply to both APIs.
- FunctionRegistry overrides with `Channel: "graphql"` scope target GraphQL only.
- FunctionRegistry overrides with an empty channel scope apply to both REST and GraphQL.

```go
// Guard that only applies to GraphQL
funcReg.RegisterGuard("pet", registry.OpCreate,
    registry.Scope{Channel: "graphql"},
    func(ctx context.Context, r *http.Request) error {
        // GraphQL-specific access control
        return nil
    },
)
```

## Multi-Schema Example

With schemas for `pet`, `order`, and `user` registered:

```graphql
type Query {
  getPet(entityId: ID!): Pet
  listPets(where: JSON, sort: String, limit: Int, offset: Int): PetListResult!
  getOrder(entityId: ID!): Order
  listOrders(where: JSON, sort: String, limit: Int, offset: Int): OrderListResult!
  getUser(entityId: ID!): User
  listUsers(where: JSON, sort: String, limit: Int, offset: Int): UserListResult!
}

type Mutation {
  createPet(input: JSON!): Pet!
  updatePet(entityId: ID!, input: JSON!): Pet!
  deletePet(entityId: ID!): Boolean!
  bulkCreatePet(inputs: [JSON!]!): PetBulkResult!
  # ... same pattern for order and user
}

type Subscription {
  onPetCreated: Pet
  onPetUpdated: Pet
  onPetDeleted: Pet
  onOrderCreated: Order
  onOrderUpdated: Order
  onOrderDeleted: Order
  # ... same pattern for user
}
```

## Related Guides

- [REST API Reference](api-reference.md) — REST endpoint reference
- [QueryDSL](query-dsl.md) — filter syntax used in `where` arguments
- [Schema Lifecycle](schema-lifecycle.md) — how schemas become API endpoints
- [Events](events.md) — event system powering subscriptions
- [Streaming](streaming.md) — external event delivery via message brokers
