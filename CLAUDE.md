# Stellar-Drive

Go port of the slip-stream Python framework. JSON Schema-driven hexagonal backend framework with runtime schema management.

## Architecture

**Hexagonal (Ports & Adapters)**:
- `pkg/core/` — Domain logic. MUST NOT import from `pkg/adapter/`.
- `pkg/adapter/driving/` — Inbound adapters (REST, GraphQL). Import from core.
- `pkg/adapter/driven/` — Outbound adapters (MongoDB, SQL, streaming). Import from core.
- `pkg/engine/` — Composition root. Wires everything together.
- `pkg/codegen/` — Code generation pipeline (JSON Schema → Go source).
- `internal/` — Private utilities, not importable by external consumers.

## Go Conventions

- **Interfaces**: Accept interfaces, return structs. No "I" prefix. Place interfaces in the consumer package or in `core/port/`.
- **Errors**: Wrap with `fmt.Errorf("operation: %w", err)`. Define sentinel errors in `core/errors/`. Use error types for structured errors.
- **Context**: First parameter of every public function. Propagate via `context.Context`.
- **Options**: Use functional options pattern (`type Option func(*Config)`) for configurable constructors.
- **Naming**: Files are `snake_case.go`. Exports are `PascalCase`. Acronyms are all-caps (`ID`, `URL`, `HTTP`).
- **Packages**: One responsibility per package. No `utils` or `helpers` packages — use specific names (`stringutil`, `timeutil`).
- **Middleware**: `func(http.Handler) http.Handler` — Chi/stdlib compatible.
- **Testing**: Table-driven tests. `testify/assert` for assertions. Build tags for integration tests (`//go:build integration`).
- **Concurrency**: Protect shared state with `sync.RWMutex`. Prefer channels for coordination, mutexes for state.

## Package Dependency Rules (Build Order)

```
Level 0: internal/stringutil, internal/timeutil, internal/jsonutil, pkg/core/errors
Level 1: pkg/core/model, pkg/core/query, pkg/core/event
Level 2: pkg/core/schema, pkg/core/port
Level 3: pkg/core/service, pkg/core/registry, pkg/core/container
Level 4: pkg/config, pkg/adapter/driven/*
Level 5: pkg/adapter/driving/*, pkg/codegen
Level 6: pkg/engine
Level 7: cmd/
```

No circular imports. Run `go vet ./...` to verify.

## Key Patterns

- **Schema Envelope**: Schemas are wrapped in envelopes with version metadata. Version lives in the envelope body, NOT in URLs.
- **Append-Only Versioning**: Every write creates a new document version. Never mutate. Soft deletes via tombstone.
- **4-Layer DI Container**: Resolution order: custom override → schema-specific → default → auto-generated.
- **QueryDSL**: Hasura-style safe query language (`_eq`, `_gt`, `_in`, `_and`, etc.) converted to MongoDB aggregation or SQL WHERE.

## Agent Personas

### Architect
Focus: Hexagonal boundaries, port/adapter design, package structure, dependency direction.
Rules: Core must not import adapters. Adapters depend on ports. Engine wires everything.

### Schema Engine
Focus: Schema registry, JSON Schema parsing, envelope management, variant generation (Document/Create/Update), `$ref` resolution, distributed schema sync.

### Persistence
Focus: MongoDB/SQL adapters, append-only versioned CRUD, aggregation pipelines, index management, query translation, change streams.

### API Surface
Focus: REST handlers, Chi middleware chain, GraphQL resolvers, schema management API, content negotiation, response envelopes.

### Codegen
Focus: JSON Schema → Go struct generation via jennifer, template pipeline, OpenAPI spec generation, SDK generation.

### Infrastructure
Focus: CLI commands (Cobra), config loading (Viper), engine lifecycle, graceful shutdown, distributed sync, streaming adapters.

### Quality
Focus: Table-driven tests, testcontainers integration tests, golden file tests for codegen, benchmarks, golangci-lint.

## Commands

```bash
make build        # go build ./...
make test         # go test ./... -short
make test-int     # go test ./... -tags=integration
make lint         # golangci-lint run
make generate     # go generate ./...
```
