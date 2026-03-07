# Stellar-Drive

Go port of the slip-stream Python framework. JSON Schema-driven hexagonal backend framework with runtime schema management.

## Architecture

**Hexagonal (Ports & Adapters)**:
- `pkg/core/` — Domain logic. MUST NOT import from `pkg/adapter/`.
- `pkg/adapter/driving/` — Inbound adapters (REST, GraphQL). Import from core.
- `pkg/adapter/driven/` — Outbound adapters (MongoDB, SQL, streaming, telemetry, policy). Import from core.
- `pkg/engine/` — Composition root. Wires everything together.
- `pkg/codegen/` — Code generation pipeline (JSON Schema → Go source, OpenAPI, SDK).
- `pkg/mcp/` — MCP server (Model Context Protocol, stdio JSON-RPC 2.0).
- `pkg/testing/sdtest/` — Test utilities (MemoryRepository, TestEngine, DataGenerator, LifecycleRunner).
- `internal/` — Private utilities, not importable by external consumers.

## Go Conventions

- **Interfaces**: Accept interfaces, return structs. No "I" prefix. Place interfaces in the consumer package or in `core/port/`.
- **Errors**: Wrap with `fmt.Errorf("operation: %w", err)`. Define sentinel errors in `core/errors/`. Use error types for structured errors. Always check error returns — `errcheck` is enforced by CI.
- **Context**: First parameter of every public function. Propagate via `context.Context`.
- **Options**: Use functional options pattern (`type Option func(*Config)`) for configurable constructors.
- **Naming**: Files are `snake_case.go`. Exports are `PascalCase`. Acronyms are all-caps (`ID`, `URL`, `HTTP`).
- **Packages**: One responsibility per package. No `utils` or `helpers` packages — use specific names (`stringutil`, `timeutil`).
- **Middleware**: `func(http.Handler) http.Handler` — Chi/stdlib compatible.
- **Testing**: Table-driven tests. `testify/assert` and `testify/require` for assertions. Build tags for integration tests (`//go:build integration`).
- **Concurrency**: Protect shared state with `sync.RWMutex`. Prefer channels for coordination, mutexes for state.

## Code Quality Rules

These rules are enforced by CI (golangci-lint v2) and must be followed:

- **Format**: All Go files must be `gofmt` formatted. Run `gofmt -w .` before committing.
- **Error checking**: All error returns must be handled. For deferred Close calls use `defer func() { _ = x.Close() }()`. For CLI fmt output use `_, _ = fmt.Fprintf(...)`.
- **No unused code**: Remove unused functions, variables, struct fields, and imports. CI flags these via `unused` and `ineffassign`.
- **Static analysis**: `govet` and `staticcheck` are enforced. Follow their suggestions (e.g., use `fmt.Fprintf` instead of `WriteString(Sprintf(...))`).
- **Lint before pushing**: Run `make lint` locally or ensure CI passes.

## Package Dependency Rules (Build Order)

```
Level 0: internal/stringutil, internal/timeutil, internal/jsonutil, internal/ctxkey, pkg/core/errors
Level 1: pkg/core/model, pkg/core/query, pkg/core/event
Level 2: pkg/core/schema, pkg/core/port
Level 3: pkg/core/service, pkg/core/registry, pkg/core/container, pkg/core/storage
Level 4: pkg/config, pkg/adapter/driven/*
Level 5: pkg/adapter/driving/*, pkg/codegen, pkg/mcp
Level 6: pkg/engine, pkg/testing/sdtest
Level 7: cmd/
```

No circular imports. Run `go vet ./...` to verify.

## Key Patterns

- **Schema Envelope**: Schemas are wrapped in envelopes with version metadata. Version lives in the envelope body, NOT in URLs.
- **Append-Only Versioning**: Every write creates a new document version. Never mutate. Soft deletes via tombstone.
- **4-Layer DI Container**: Resolution order: custom override → schema-specific → default → auto-generated.
- **QueryDSL**: Hasura-style safe query language (`_eq`, `_gt`, `_in`, `_and`, etc.) converted to MongoDB aggregation or SQL WHERE.
- **ETag Conditional Requests**: `If-None-Match` → 304, `If-Match` → 412 via context key propagation through `internal/ctxkey`.
- **FunctionRegistry Pipeline**: Handler override → Guards → Validators → Transforms → Service call.
- **Per-Schema Storage Routing**: Schemas can be individually mapped to `mongo` or `sql` backends via config.

## Agent Personas

### Architect
Focus: Hexagonal boundaries, port/adapter design, package structure, dependency direction.
Rules: Core must not import adapters. Adapters depend on ports. Engine wires everything.

### Schema Engine
Focus: Schema registry, JSON Schema parsing, envelope management, variant generation (Document/Create/Update), `$ref` resolution, hot-reload watcher, distributed schema sync.

### Persistence
Focus: MongoDB/SQL adapters, append-only versioned CRUD, aggregation pipelines, index management, query translation, change streams, transactions, migrations.

### API Surface
Focus: REST handlers, Chi middleware chain (recovery, request ID, logging, security headers, CORS, rate limit, ETag, cache, content negotiation, policy), GraphQL resolvers/subscriptions, schema management API, bulk operations, response envelopes.

### Codegen
Focus: JSON Schema → Go struct generation via jennifer, template pipeline, OpenAPI spec generation, SDK client generation.

### Infrastructure
Focus: CLI commands (Cobra), config loading (Viper), engine lifecycle, graceful shutdown, MCP server, telemetry instrumentation, streaming adapters.

### Quality
Focus: Table-driven tests, testcontainers integration tests, golden file tests for codegen, benchmarks, golangci-lint v2. All error returns must be checked. All files must be gofmt'd.

## Project Layout

```
cmd/stellar-drive/    Entry point (package main)
cmd/                  Cobra CLI commands (package cmd)
pkg/                  Public library code
internal/             Private utilities
docs/                 Documentation
schemas/              Example JSON Schema files
.github/workflows/    CI/CD (lint, test, build, coverage)
```

## Commands

```bash
make build        # go build ./...
make install      # go build -o stellar-drive ./cmd/stellar-drive
make test         # go test ./... -short
make test-int     # go test ./... -tags=integration
make lint         # golangci-lint run ./...
make vet          # go vet ./...
make generate     # go generate ./...
make coverage     # generate coverage report (coverage.html)
make tidy         # go mod tidy
```

## CI Pipeline

GitHub Actions (`.github/workflows/ci.yml`) runs on every PR to `main`:
1. **Lint** — golangci-lint v2 (errcheck, govet, staticcheck, unused, ineffassign, gofmt)
2. **Test** — `go vet` + `go test ./... -short -count=1 -race`
3. **Coverage** — test with `-coverprofile` + artifact upload
4. **Build** — `go build ./...` (depends on lint + test passing)
