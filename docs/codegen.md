# Code Generation

Stellar-drive includes a code generation pipeline that produces typed Go source files from JSON Schema definitions. Instead of working with `map[string]any` at every layer, generated code gives you compile-time type safety for models, repositories, services, and HTTP handlers -- while still coexisting with the dynamic, map-based runtime that handles schemas registered at startup or via the `/_schemas` API.

The codegen package lives in `pkg/codegen/`.

## Overview

The `Generator` struct orchestrates all generation. Given a schema registry populated with schema envelopes, it iterates every registered schema and produces a complete Go package for each one.

```go
gen := codegen.NewGenerator(registry, "./generated", "github.com/myorg/myproject")
if err := gen.Generate(); err != nil {
    log.Fatal(err)
}
```

For each schema named `pet`, this creates a directory `generated/pet/` containing:

| File | Contents |
|------|----------|
| `models.go` | Typed `PetDocument`, `PetCreate`, `PetUpdate` structs with JSON tags |
| `repository.go` | `PetRepository` wrapping `port.Repository` with typed methods |
| `service.go` | `PetService` wrapping `port.Service` with typed methods |
| `handler.go` | `PetHandler` providing typed Chi HTTP routes |
| `convert.go` | Helper functions for struct-to-map conversion |
| `schema.graphqls` | GraphQL SDL with types, inputs, queries, and mutations |

A separate `pkg/codegen/sdk/` package generates an OpenAPI 3.1 specification covering all schemas.

## CLI Usage

Generate code from the command line:

```bash
stellar-drive generate --schemas ./schemas --output ./generated
```

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--schemas` | `./schemas` | Directory containing schema envelope JSON files |
| `--output` | `./generated` | Output directory for generated packages |
| `--go` | `true` | Generate Go source files |
| `--openapi` | `true` | Generate OpenAPI 3.1 spec |
| `--graphql` | `true` | Generate GraphQL SDL per schema |

You can also generate a single schema:

```bash
stellar-drive generate --schemas ./schemas --output ./generated --schema pet
```

## Generated Artifacts

### models.go

For each schema, three struct variants are generated:

**Document** -- the fully persisted representation including all audit fields:

```go
type PetDocument struct {
    ID            string     `json:"id"`
    EntityID      string     `json:"entity_id"`
    RecordVersion int64      `json:"record_version"`
    SchemaVersion string     `json:"schema_version"`
    SchemaName    string     `json:"schema_name"`
    ETag          string     `json:"etag"`
    CreatedAt     time.Time  `json:"created_at"`
    UpdatedAt     time.Time  `json:"updated_at"`
    DeletedAt     *time.Time `json:"deleted_at,omitempty"`
    CreatedBy     string     `json:"created_by,omitempty"`
    UpdatedBy     string     `json:"updated_by,omitempty"`
    DeletedBy     string     `json:"deleted_by,omitempty"`
    Name          string     `json:"name"`
    PhotoUrls     []any      `json:"photo_urls"`
    Status        PetStatus  `json:"status,omitempty"`
}
```

**Create** -- fields accepted when creating a new record. Required fields are not tagged `omitempty`:

```go
type PetCreate struct {
    Name      string    `json:"name"`
    PhotoUrls []any     `json:"photo_urls"`
    Status    PetStatus `json:"status,omitempty"`
}
```

**Update** -- all fields are pointers, enabling PATCH semantics where only supplied fields are applied:

```go
type PetUpdate struct {
    Name      *string    `json:"name,omitempty"`
    PhotoUrls []any      `json:"photo_urls,omitempty"`
    Status    *PetStatus `json:"status,omitempty"`
}
```

When a schema field has an `enum` constraint, a named string type and constants are generated:

```go
type PetStatus string

const (
    PetStatusAvailable PetStatus = "available"
    PetStatusPending   PetStatus = "pending"
    PetStatusSold      PetStatus = "sold"
)
```

### repository.go

A typed repository wrapper around `port.Repository`. All methods accept typed structs and delegate to the underlying map-based interface:

```go
type PetRepository struct {
    inner      port.Repository
    schemaName string
}

func NewPetRepository(repo port.Repository) *PetRepository

func (r *PetRepository) Create(ctx context.Context, input *PetCreate) (*model.Document, error)
func (r *PetRepository) FindByID(ctx context.Context, entityID string) (*model.Document, error)
func (r *PetRepository) List(ctx context.Context, q *query.Query) (*model.ListResult, error)
func (r *PetRepository) Update(ctx context.Context, entityID string, input *PetUpdate) (*model.Document, error)
func (r *PetRepository) Delete(ctx context.Context, entityID string) error
```

The `Create` and `Update` methods use the generated `convert.go` helpers to marshal typed structs into `map[string]any` before passing them to the underlying repository. This preserves `omitempty` semantics -- nil pointer fields in `PetUpdate` are excluded from the patch map.

### service.go

A typed service wrapper around `port.Service` with the same method signatures as the repository:

```go
type PetService struct {
    inner      port.Service
    schemaName string
}

func NewPetService(svc port.Service) *PetService

func (s *PetService) Create(ctx context.Context, input *PetCreate) (*model.Document, error)
func (s *PetService) FindByID(ctx context.Context, entityID string) (*model.Document, error)
func (s *PetService) List(ctx context.Context, q *query.Query) (*model.ListResult, error)
func (s *PetService) Update(ctx context.Context, entityID string, input *PetUpdate) (*model.Document, error)
func (s *PetService) Delete(ctx context.Context, entityID string) error
```

The service layer fires lifecycle events and validates against the schema before delegating to the repository. Using the typed wrapper means your application code works with `*PetCreate` instead of `map[string]any`.

### handler.go

A typed HTTP handler that mounts five CRUD routes on a Chi router:

```go
type PetHandler struct {
    svc *PetService
}

func NewPetHandler(svc *PetService) *PetHandler

func (h *PetHandler) Routes() chi.Router
func (h *PetHandler) Create(w http.ResponseWriter, r *http.Request)
func (h *PetHandler) List(w http.ResponseWriter, r *http.Request)
func (h *PetHandler) GetByID(w http.ResponseWriter, r *http.Request)
func (h *PetHandler) Update(w http.ResponseWriter, r *http.Request)
func (h *PetHandler) Delete(w http.ResponseWriter, r *http.Request)
```

Routes mounted by `Routes()`:

| Method | Path | Handler |
|--------|------|---------|
| POST | `/` | Create |
| GET | `/` | List |
| GET | `/{entityID}` | GetByID |
| PATCH | `/{entityID}` | Update |
| DELETE | `/{entityID}` | Delete |

### schema.graphqls

GraphQL Schema Definition Language for the schema, including:
- A document type with all audit and user-defined fields
- A `Create` input type (required fields marked with `!`)
- An `Update` input type (all fields optional)
- A `ListResult` type with pagination
- Query and Mutation type extensions

```graphql
type Pet {
  id: ID!
  entityId: String!
  recordVersion: Int!
  name: String!
  photoUrls: [String]!
  status: String
}

input PetCreate {
  name: String!
  photoUrls: [String]!
  status: String
}

input PetUpdate {
  name: String
  photoUrls: [String]
  status: String
}

type Query {
  pet(entityId: ID!): Pet
  pets(limit: Int, offset: Int, cursor: String): PetListResult!
}

type Mutation {
  createPet(input: PetCreate!): Pet!
  updatePet(entityId: ID!, input: PetUpdate!): Pet!
  deletePet(entityId: ID!): Boolean!
}
```

## OpenAPI 3.1 Spec Generation

The `pkg/codegen/sdk/openapi.go` package generates a complete OpenAPI 3.1 specification from all registered schemas:

```go
import "github.com/digitally-rendered/stellar-drive/pkg/codegen/sdk"

spec, err := sdk.GenerateOpenAPI(registry, "Petstore API", "1.0.0")
if err != nil {
    log.Fatal(err)
}

jsonBytes, _ := sdk.MarshalJSON(spec)
os.WriteFile("openapi.json", jsonBytes, 0644)
```

The generated spec includes:
- **Component schemas** for each schema's Document, Create, Update, and ListResult variants
- **Path items** for collection routes (POST, GET) and entity routes (GET, PATCH, DELETE)
- **Parameters** for pagination (limit, offset, cursor) and entity ID path params
- **Request bodies** with JSON content type references
- **Response schemas** for success, error, and list responses

## Type Mapping

JSON Schema types are mapped to Go types as follows:

| JSON Schema Type | Format | Go Type |
|-----------------|--------|---------|
| `string` | (none) | `string` |
| `string` | `date-time` | `time.Time` |
| `string` | `email`, `uri`, etc. | `string` |
| `integer` | (any) | `int64` |
| `number` | (any) | `float64` |
| `boolean` | (any) | `bool` |
| `array` | (any) | `[]any` (or `[]T` if items are typed) |
| `object` | (any) | `map[string]any` |
| `string` with `enum` | (none) | Named `string` type with constants |

For the Update variant, all scalar types are wrapped in pointers (`*string`, `*int64`, etc.) to distinguish "not provided" from zero values. Slice and map types are not pointer-wrapped because their zero value (`nil`) already signals absence.

## Generated Code Conventions

- **Package naming**: Each schema gets its own package under the output directory. Package names are sanitized from the schema name (lowercase, alphanumeric, underscores only; hyphens and dots are stripped).
- **Type prefix**: The schema name in PascalCase is prefixed to all types. Schema `pet` produces `PetDocument`, `PetCreate`, `PetUpdate`, `PetRepository`, etc.
- **Import paths**: Generated code imports from the module path provided to the generator. Repository and service wrappers import `pkg/core/model`, `pkg/core/port`, and `pkg/core/query`.
- **DO NOT EDIT header**: All generated files include a `// Code generated by stellar-drive; DO NOT EDIT.` comment.
- **go/format**: Go source files are formatted with `go/format` before writing. If formatting fails, the unformatted source is written so you can inspect and debug.

## Using Generated Code with Dynamic Schemas

Generated code and dynamic runtime schemas coexist naturally. You can:

1. Generate typed code for known, stable schemas (e.g., `pet`, `user`, `order`).
2. Use the dynamic runtime for schemas registered via the `/_schemas` API at runtime.
3. Register generated handlers in the DI container for type-safe schemas while letting the `GenericHandler` handle dynamic ones.

```go
// Wire typed pet handler into the DI container.
petSvc := pet.NewPetService(genericService)
petHandler := pet.NewPetHandler(petSvc)
container.RegisterHandler("pet", petHandler.Routes())

// All other schemas use the default GenericHandler automatically.
```

## Programmatic Usage

Use the generator directly in Go code:

```go
package main

import (
    "log"

    "github.com/digitally-rendered/stellar-drive/pkg/codegen"
    "github.com/digitally-rendered/stellar-drive/pkg/codegen/sdk"
    "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

func main() {
    // Load schemas into a registry.
    reg := schema.NewRegistry()
    if err := schema.LoadFromDir(reg, "./schemas"); err != nil {
        log.Fatal(err)
    }

    // Generate Go packages for all schemas.
    gen := codegen.NewGenerator(reg, "./generated", "github.com/myorg/myapi")
    if err := gen.Generate(); err != nil {
        log.Fatal(err)
    }

    // Generate a single schema.
    if err := gen.GenerateSchema("pet"); err != nil {
        log.Fatal(err)
    }

    // Generate OpenAPI spec.
    spec, err := sdk.GenerateOpenAPI(reg, "My API", "1.0.0")
    if err != nil {
        log.Fatal(err)
    }
    jsonBytes, _ := sdk.MarshalJSON(spec)
    _ = os.WriteFile("openapi.json", jsonBytes, 0644)
}
```

## Generated Directory Structure

After running `stellar-drive generate` with the Petstore schemas:

```
generated/
├── pet/
│   ├── convert.go       # Struct-to-map conversion helpers
│   ├── models.go        # PetDocument, PetCreate, PetUpdate, PetStatus
│   ├── repository.go    # PetRepository
│   ├── service.go       # PetService
│   ├── handler.go       # PetHandler with Chi routes
│   └── schema.graphqls  # GraphQL SDL
├── order/
│   ├── convert.go
│   ├── models.go
│   ├── repository.go
│   ├── service.go
│   ├── handler.go
│   └── schema.graphqls
├── user/
│   ├── ...
├── category/
│   ├── ...
└── tag/
    ├── ...
```

## Related Documentation

- **[Architecture](architecture.md)** -- How generated code fits into the hexagonal architecture
- **[Schema Envelopes](schema-envelopes.md)** -- Envelope format that drives code generation
- **[Container](container.md)** -- Registering generated handlers and services in the DI container
- **[API Reference](api-reference.md)** -- REST and GraphQL endpoints produced by generated handlers
