# API Reference

This document describes every HTTP endpoint exposed by a stellar-drive server, including the schema management API, entity CRUD endpoints, health probes, audit trail, and GraphQL.

## Base URL

All entity and schema management endpoints are prefixed with the value of `server.api_prefix` from `stellar.yaml`. The default is `/api/v1`. Health and readiness probes are mounted at the root, outside the API prefix.

## Response Envelopes

All responses follow a consistent envelope structure.

**Success:** `{"success": true, "data": { ... }}`

**List:** `{"success": true, "data": [...], "meta": {"total": 42, "has_more": true, "cursor": "..."}}`

**Error:**

```json
{
  "success": false,
  "error": {
    "code": "validation_failed",
    "message": "input validation failed",
    "details": [{"field": "email", "message": "must be a valid email", "code": "format"}]
  }
}
```

### HTTP Status Codes

| Domain Error | HTTP Status |
|-------------|-------------|
| `bad_request` | 400 |
| `unauthorized` | 401 |
| `permission_denied` | 403 |
| `not_found` | 404 |
| `conflict` | 409 |
| `validation_failed` | 422 |
| `internal` | 500 |

---

## Schema Management API

Endpoints live under `{api_prefix}/_schemas`. Version information is in the request/response body (the schema envelope), never in the URL.

### POST /_schemas -- Register Schema

Register a new schema at runtime. The server creates CRUD routes immediately.

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d @schemas/pet.schema.json
```

**Response (201 Created):** The registered schema envelope wrapped in a success response.

**Errors:** `400` -- Invalid envelope format or validation failure.

### GET /_schemas -- List All Schemas

Returns the latest version of every registered schema.

```bash
curl http://localhost:8080/api/v1/_schemas
```

### GET /_schemas/{name} -- Get Schema by Name

Returns a specific schema. Optionally specify a version via query parameter or header.

```bash
curl http://localhost:8080/api/v1/_schemas/pet
curl http://localhost:8080/api/v1/_schemas/pet?version=1.2.0
curl -H "X-Schema-Version: 1.2.0" http://localhost:8080/api/v1/_schemas/pet
```

**Errors:** `404` -- Schema not found.

### PUT /_schemas/{name} -- Update Schema

Re-registers a schema with updated fields. If the version in the envelope is newer, the schema pointer is updated. Also mounts CRUD routes if not already present.

```bash
curl -X PUT http://localhost:8080/api/v1/_schemas/pet \
  -H "Content-Type: application/json" \
  -d '{"name":"pet","version":"2.0.0","storage":"mongo","collection":"pets","schema":{...}}'
```

**Errors:** `400` -- Invalid envelope.

### DELETE /_schemas/{name} -- Deactivate Schema

Removes the schema from the in-memory registry and the persistent store.

```bash
curl -X DELETE http://localhost:8080/api/v1/_schemas/pet
```

**Response:** `204 No Content`. **Errors:** `404` -- Schema not found.

---

## Entity CRUD API

For every registered schema, stellar-drive auto-generates five CRUD endpoints under `{api_prefix}/{schemaName}/`.

### POST /{schema}/ -- Create Document (201 Created)

```bash
curl -X POST http://localhost:8080/api/v1/pet/ \
  -H "Content-Type: application/json" \
  -d '{"name":"Buddy","photo_urls":["https://example.com/buddy.jpg"],"status":"available"}'
```

Response `data` includes `id`, `entity_id`, `record_version` (1), `schema_version`, `created_at`, `updated_at`, and all user fields.

**Errors:** `400` -- Malformed JSON. `422` -- Validation failure.

### GET /{schema}/ -- List Documents (200 OK)

Returns a paginated list with `meta.total` and `meta.has_more`. Supports query parameters (see below).

```bash
curl http://localhost:8080/api/v1/pet/
```

### GET /{schema}/{entityID} -- Get Single Document (200 OK)

Retrieves a single document. Supports field projection via `?fields=`.

```bash
curl http://localhost:8080/api/v1/pet/a1b2c3d4-e5f6-7890-abcd-ef1234567890
curl "http://localhost:8080/api/v1/pet/a1b2c3d4?fields=name,status"
```

**Errors:** `400` -- Missing entity ID. `404` -- Not found.

### PATCH /{schema}/{entityID} -- Partial Update (200 OK)

Applies a partial update. Only supplied fields are changed. A new version is created (append-only).

```bash
curl -X PATCH http://localhost:8080/api/v1/pet/a1b2c3d4 \
  -H "Content-Type: application/json" \
  -d '{"status":"sold"}'
```

The `record_version` increments. **Errors:** `400`, `404`, `422`.

### DELETE /{schema}/{entityID} -- Soft Delete (204 No Content)

Creates a new version with `deleted_at` set. The document no longer appears in GET/LIST but history is preserved.

```bash
curl -X DELETE http://localhost:8080/api/v1/pet/a1b2c3d4
```

**Errors:** `400` -- Missing entity ID. `404` -- Not found.

---

## Query Parameters

All list endpoints accept query parameters for filtering, sorting, pagination, and projection.

| Parameter | Type | Description |
|-----------|------|-------------|
| `where` | JSON object | Hasura-style QueryDSL filter (see `docs/query-dsl.md`) |
| `sort` | string | Comma-separated fields; `-` prefix for descending, `+` or none for ascending |
| `limit` | integer | Page size (default 50) |
| `offset` | integer | Number of documents to skip |
| `cursor` | string | Opaque pagination cursor from `meta.cursor` |
| `fields` | string | Comma-separated field projection list |

Examples:

```bash
# Filter by status, sort by name, limit to 10
curl -G http://localhost:8080/api/v1/pet/ \
  --data-urlencode 'where={"status":{"_eq":"available"}}' \
  --data-urlencode 'sort=name' \
  --data-urlencode 'limit=10'

# Offset pagination
curl "http://localhost:8080/api/v1/pet/?limit=20&offset=40"

# Cursor pagination
curl "http://localhost:8080/api/v1/pet/?limit=20&cursor=eyJpZCI6IjY1YTFi..."

# Field projection
curl "http://localhost:8080/api/v1/pet/a1b2c3d4?fields=name,status,created_at"
```

---

## Health API

Mounted at the root path, outside the API prefix and middleware chain.

### GET /health -- Liveness Probe

Always returns 200 OK: `{"status":"healthy"}`.

### GET /ready -- Readiness Probe

Checks configured subsystems. Returns 200 when all pass, 503 when any fails.

```json
{"status":"ready","checks":{"database":true,"schemas":true}}
```

```json
{"status":"not_ready","checks":{"database":false,"schemas":true}}
```

Custom checks can be registered via `rest.WithReadinessCheck()`.

---

## Audit API

When audit is enabled (`audit.enabled: true`), two endpoints are mounted under `{api_prefix}/_audit`.

### GET /_audit/entity/{entityID} -- Entity Audit History

Returns all audit trail entries for a specific entity.

```bash
curl http://localhost:8080/api/v1/_audit/entity/a1b2c3d4
curl "http://localhost:8080/api/v1/_audit/entity/a1b2c3d4?schema=pet&limit=10"
```

### GET /_audit/user/{userID} -- User Activity Log

Returns all audit trail entries for a specific user.

```bash
curl http://localhost:8080/api/v1/_audit/user/user-123
curl "http://localhost:8080/api/v1/_audit/user/user-123?schema=pet&limit=50"
```

**Query Parameters:** `schema` (filter by schema name), `limit` (max entries to return).

---

## Content Negotiation

When enabled (`middleware.content_negotiation: true`), the server negotiates response format via the `Accept` header.

| Accept Header | Response Format |
|--------------|-----------------|
| `application/json` (default) | JSON |
| `application/yaml` | YAML |
| `application/xml` | XML |

```bash
curl -H "Accept: application/yaml" http://localhost:8080/api/v1/pet/
curl -H "Accept: application/xml" http://localhost:8080/api/v1/pet/
```

Request bodies can also be sent as YAML via `Content-Type: application/yaml`. The middleware converts them to JSON before reaching downstream handlers.

---

## GraphQL Endpoint

When enabled (`graphql.enabled: true`), mounted at the configured path (default `/graphql`).

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ pets(limit: 10) { items { id name status } total hasMore } }"}'
```

The GraphQL schema is dynamically generated from all registered schema definitions and includes query types, mutation types, input types, and list result types. See `docs/codegen.md` for the GraphQL SDL format.

---

## Request Headers

| Header | Purpose |
|--------|---------|
| `Content-Type` | Request body format (`application/json`, `application/yaml`) |
| `Accept` | Desired response format (`application/json`, `application/yaml`, `application/xml`) |
| `Authorization` | JWT Bearer token (`Bearer <token>`) when auth is enabled |
| `X-Request-ID` | Client-supplied request ID; echoed back in response |
| `X-Schema-Version` | Specify schema version for `GET /_schemas/{name}` |

## Related Documentation

- **[Getting Started](getting-started.md)** -- Quick start with curl examples
- **[QueryDSL](query-dsl.md)** -- Full operator reference for the `where` parameter
- **[Middleware](middleware.md)** -- Authentication, CORS, rate limiting
- **[Schema Envelopes](schema-envelopes.md)** -- Envelope format for the schema management API
- **[Configuration](configuration.md)** -- Server, auth, and middleware configuration
- **[Code Generation](codegen.md)** -- Generated OpenAPI spec and GraphQL SDL
