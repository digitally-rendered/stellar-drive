# MCP Server

Stellar-Drive includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes your schema registry as AI-callable tools. This enables AI assistants like Claude to inspect, query, create, and validate schemas in your project.

## Starting the MCP Server

```bash
stellar-drive mcp
```

The server reads from **stdin** and writes to **stdout** using line-delimited JSON-RPC 2.0. It loads all schemas from the configured schema directory before entering the request loop.

Configuration is loaded from `stellar.yaml` if present (non-fatal if absent — falls back to defaults).

## Protocol

The MCP server implements the [MCP specification](https://spec.modelcontextprotocol.io/) with stdio transport:

- **Protocol version**: `2024-11-05`
- **Transport**: stdin/stdout, line-delimited JSON-RPC 2.0
- **Max message size**: 4 MiB (handles large schema payloads)
- **Capabilities**: `tools` (listChanged: false)

### Supported JSON-RPC Methods

| Method | Description |
|--------|-------------|
| `initialize` | Returns server info and capabilities |
| `initialized` | Acknowledgment notification (no response) |
| `ping` | Health check — returns `{}` |
| `tools/list` | Returns the manifest of all 11 tools |
| `tools/call` | Executes a tool by name with arguments |

## Integration with AI Assistants

### Claude Desktop / Claude Code

Add to your MCP configuration:

```json
{
  "mcpServers": {
    "stellar-drive": {
      "command": "stellar-drive",
      "args": ["mcp"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

### Custom Integration

Any MCP-compatible client can connect via stdio. The server reads one JSON-RPC message per line from stdin and writes one response per line to stdout.

## Tool Reference

### 1. `list_schemas`

Lists all registered schemas with summary metadata.

**Parameters**: None

**Returns**: JSON array of schema summaries.

```json
[
  {
    "name": "pet",
    "version": "1.0.0",
    "storage": "mongo",
    "description": "A pet in the store"
  },
  {
    "name": "order",
    "version": "1.0.0",
    "storage": "mongo",
    "description": "A store order"
  }
]
```

---

### 2. `get_schema`

Retrieves the full schema envelope for a named schema (latest version).

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `name` | string | Yes | Schema name |

**Returns**: Pretty-printed JSON of the complete `SchemaEnvelope` including the raw JSON Schema body, indexes, extensions, and metadata.

```json
{
  "name": "pet",
  "version": "1.0.0",
  "storage": "mongo",
  "schema": {
    "type": "object",
    "properties": {
      "name": { "type": "string" },
      "status": { "type": "string", "enum": ["available", "pending", "sold"] }
    },
    "required": ["name", "status"]
  },
  "indexes": [{ "fields": ["name"], "unique": true }]
}
```

---

### 3. `describe_entity`

Describes a schema's parsed fields with Go types, formats, and required status.

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `name` | string | Yes | Schema name |

**Returns**: Structured field listing.

```json
{
  "name": "pet",
  "version": "1.0.0",
  "description": "A pet in the store",
  "storage": "mongo",
  "required_fields": ["name", "status"],
  "fields": [
    { "name": "age", "json_type": "integer", "go_type": "int64", "required": false },
    { "name": "name", "json_type": "string", "go_type": "string", "required": true },
    { "name": "status", "json_type": "string", "go_type": "string", "required": true }
  ]
}
```

---

### 4. `list_versions`

Lists all registered versions of a specific schema.

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `name` | string | Yes | Schema name |

**Returns**: JSON array of version entries.

```json
[
  { "name": "pet", "version": "1.0.0" },
  { "name": "pet", "version": "2.0.0" }
]
```

---

### 5. `get_schema_dag`

Shows the `$ref` dependency graph across all schemas.

**Parameters**: None

**Returns**: JSON array of nodes with their outgoing references.

```json
[
  { "name": "order", "refs": ["pet#/definitions/PetRef", "user"] },
  { "name": "pet", "refs": ["category"] },
  { "name": "user", "refs": [] }
]
```

Useful for understanding schema dependencies before modifying or deleting schemas.

---

### 6. `get_topology`

Shows the storage backend mapping for every schema.

**Parameters**: None

**Returns**: JSON array showing which backend each schema uses.

```json
[
  { "schema": "pet", "version": "1.0.0", "storage": "mongo", "collection": "pets" },
  { "schema": "order", "version": "1.0.0", "storage": "sql", "collection": "orders" }
]
```

---

### 7. `query_rest_api`

Generates a curl command for a REST API call.

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `method` | string | Yes | HTTP method (GET, POST, PATCH, DELETE) |
| `path` | string | Yes | API path (e.g., `/pets`, `/pets/abc-123`) |
| `body` | string | No | Request body JSON |

**Returns**: A formatted curl command with headers. If the path matches a registered schema, includes schema metadata as comments.

```
curl -X POST http://localhost:8080/api/v1/pets \
  -H 'Content-Type: application/json' \
  -d '{"name":"Buddy","status":"available"}'

# Schema: pet (v1.0.0, storage: mongo)
# Fields: name (string, required), status (string, required), age (integer)
```

---

### 8. `query_graphql`

Lists all available GraphQL queries and mutations derived from registered schemas.

**Parameters**: None

**Returns**: JSON array of operations.

```json
[
  { "operation": "listPets", "type": "query" },
  { "operation": "getPet", "type": "query" },
  { "operation": "createPet", "type": "mutation" },
  { "operation": "updatePet", "type": "mutation" },
  { "operation": "deletePet", "type": "mutation" }
]
```

---

### 9. `create_schema`

Registers a new schema at runtime (in-memory only, not persisted to disk).

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `name` | string | Yes | Schema name |
| `version` | string | Yes | Schema version |
| `schema` | string | Yes | Full JSON `SchemaEnvelope` as a string |

**Returns**: Confirmation message with field count on success, or error text on failure.

```
Schema "product" registered successfully with 5 fields
```

The schema is immediately available to all other tools (`list_schemas`, `describe_entity`, etc.) for the duration of the MCP session.

**Example input**:
```json
{
  "name": "product",
  "version": "1.0.0",
  "schema": "{\"name\":\"product\",\"version\":\"1.0.0\",\"schema\":{\"type\":\"object\",\"properties\":{\"title\":{\"type\":\"string\"},\"price\":{\"type\":\"number\"}},\"required\":[\"title\",\"price\"]}}"
}
```

---

### 10. `validate_schemas`

Validates all registered schemas against the envelope validation rules.

**Parameters**: None

**Returns**: Multi-line report with OK/FAIL per schema and a summary.

```
OK   pet@1.0.0
OK   order@1.0.0
FAIL category@1.0.0: schema body is required

2 passed, 1 failed
```

Sets `isError: true` in the MCP result if any schema fails validation.

---

### 11. `generate_sdk`

Prints the CLI command to generate typed Go code and lists the artifacts that would be produced.

**Parameters**:

| Name | Type | Required | Description |
|------|------|:--------:|-------------|
| `output_dir` | string | No | Output directory (default: `generated`) |

**Returns**: Command and artifact listing.

```
Run the following command to generate typed Go code:

  stellar-drive generate --schemas ./schemas --output generated

This will generate the following artifacts per schema:
  - models.go      (Document, Create, Update structs + enums)
  - repository.go  (typed repository wrapper)
  - service.go     (typed service wrapper)
  - handler.go     (Chi HTTP handler with routes)
  - convert.go     (struct ↔ map conversion helpers)
  - schema.graphqls (GraphQL SDL)

Plus top-level:
  - openapi.json   (OpenAPI 3.1 specification)
```

## Server Construction (Programmatic)

For embedding the MCP server in a custom setup:

```go
import "github.com/digitally-rendered/stellar-drive/pkg/mcp"

server := mcp.NewServer(registry,
    mcp.WithBaseURL("http://localhost:8080"),
    mcp.WithAPIPrefix("/api/v1"),
    mcp.WithSchemaDir("./schemas"),
)

// Run the stdio loop (blocks until stdin closes or ctx cancels)
err := server.RunStdio(ctx)
```

### Server Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithBaseURL(url)` | `http://localhost:8080` | Base URL for generated curl commands |
| `WithAPIPrefix(prefix)` | `/api/v1` | API path prefix |
| `WithSchemaDir(dir)` | `./schemas` | Schema directory shown in `generate_sdk` output |

## Error Handling

MCP tools distinguish between two error levels:

1. **Protocol errors** — malformed JSON-RPC, unknown method, etc. These return JSON-RPC error responses with standard error codes (`-32700`, `-32601`, etc.).

2. **Tool errors** — schema not found, validation failures, etc. These return a successful JSON-RPC response with `isError: true` in the tool result content. This follows the MCP convention of separating transport errors from tool-level errors.

## Related Guides

- [Schema Envelopes](schema-envelopes.md) — envelope format used by `create_schema`
- [Dynamic Schemas](dynamic-schemas.md) — runtime schema management via REST API
- [Code Generation](codegen.md) — what `generate_sdk` produces
- [Configuration](configuration.md) — `stellar.yaml` reference
