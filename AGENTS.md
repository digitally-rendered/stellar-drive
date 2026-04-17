# AGENTS.md

Agent-oriented map of stellar-drive. If you're an AI coding agent (or a human in a hurry) trying to figure out where to change things, start here. This is intentionally concrete: "to do X, edit Y, register in Z."

For deeper context read [CLAUDE.md](CLAUDE.md) (conventions, lint rules, layering) and the `docs/` tree.

## One-minute orientation

- **Module**: `github.com/digitally-rendered/stellar-drive`
- **Layering**: hexagonal. `pkg/core/` is the domain. `pkg/adapter/driving/` is inbound (REST, GraphQL). `pkg/adapter/driven/` is outbound (Mongo, SQL, policy, telemetry, streaming). `pkg/engine/` wires everything.
- **Golden rule**: `pkg/core/**` MUST NOT import from `pkg/adapter/**`. If you're adding something that needs both, put the interface in `pkg/core/port/` and the implementation in `pkg/adapter/driven/`.
- **Reference consumer**: [`examples/petstore/`](examples/petstore/) boots the full engine with every extension point wired. Clone its `main.go` when starting a new embedding.

## "How do I..." → "edit this, register here"

### Add a new schema

1. Drop a `name.schema.json` file in `schemas/` (or the `cfg.Schemas.Dir` your service points at).
2. Envelope format: see `docs/schema-envelopes.md`. Required: `name`, `version`, `schema` (JSON Schema body).
3. If you want typed Go wrappers: `stellar generate --schemas schemas --output generated`.
4. If you also want editable hook stubs: add `--overrides`. Generated files: `generated/<name>/<name>_overrides.go`. Safe to re-run; existing stubs are preserved.

### Add a guard, validator, or transform

Edit the registration in your `main.go` (see [`examples/petstore/main.go`](examples/petstore/main.go)):

```go
funcReg := registry.NewFunctionRegistry()
funcReg.RegisterGuard(schemaName, registry.OpCreate, registry.Scope{}, guardFn)
funcReg.RegisterValidator(schemaName, registry.OpUpdate, registry.Scope{}, validatorFn)
funcReg.RegisterTransform(schemaName, registry.OpCreate, registry.Scope{}, transformFn)
eng := engine.New(cfg, engine.WithFunctionRegistry(funcReg))
```

Pipeline execution order: **handler-override → guards → validators → transforms → service call**. Full walkthrough: `docs/registry.md`.

### Replace a CRUD handler entirely

```go
funcReg.RegisterHandler("pet", registry.OpList, registry.Scope{}, myHandler)
```

When a handler resolves, guards/validators/transforms for the **same** operation are bypassed. To keep them, call `funcReg.ResolveGuards(...)` yourself.

### Add a non-CRUD endpoint (webhook, search, report)

```go
eng := engine.New(cfg, engine.WithRoutes(func(r chi.Router) {
    r.Post("/pets/_search", petSearchHandler)
    r.Mount("/webhooks", webhookRouter())
}))
```

Custom routes are mounted under `cfg.Server.APIPrefix` (default `/api/v1`) before the schema-driven routes, so they cannot be shadowed. Full guide: `docs/custom-endpoints.md`.

### Subscribe to a lifecycle event

```go
bus := event.NewBus()
bus.Subscribe(event.PostCreate, "pet", handler)
eng := engine.New(cfg, engine.WithEventBus(bus))
```

Ordering and failure semantics: `docs/events.md`. Pre-events can abort the operation; post-events cannot.

### Add HTTP middleware

```go
eng := engine.New(cfg, engine.WithMiddleware(metricsMiddleware, tracingMiddleware))
```

Runs **after** the built-in stack (recovery, request ID, logging, security headers, CORS, rate limit). Your middleware sees a request that has already passed those.

### Add a new storage backend

1. Create `pkg/adapter/driven/<backend>/repository.go`. Implement `port.Repository` (`pkg/core/port/repository.go`).
2. Register it in the engine wiring at `pkg/engine/engine.go` — follow the Mongo and SQL precedents.
3. Update `pkg/config/` to accept backend-specific config.
4. Add tests under the same package, with a `//go:build integration` tag for anything that needs a real database.

### Change the default middleware stack

`pkg/engine/engine.go:buildMiddleware()` is the single assembly point. Order matters — recovery must be outermost, policy must be innermost (after auth/logging). Every entry is opt-in through `cfg.Middleware.*` except the three always-on ones (Recovery, RequestID, Logging).

### Add a new CLI subcommand

1. Create `cmd/<name>.go` with a Cobra `*cobra.Command`.
2. Register it in an `init()` with `rootCmd.AddCommand(...)`. Existing subcommands: `run`, `generate`, `init`, `schema`, `migrate`, `mcp`.

### Add a new MCP tool

Edit `pkg/mcp/`. Tools are registered in a single table; mirror the existing entries (name, description, input schema, handler). The MCP server is started via `stellar mcp`.

### Add a code-generated artefact per schema

`pkg/codegen/` is the pipeline. Every generator is a function `Generate<Thing>(def, pkgName, modulePath) (string, error)` that returns Go source, and `Generator.GenerateSchema` in `generator.go` is the composition point. Add your generator there.

Existing generators:
- `models.go` — `<Type>Document`, `<Type>Create`, `<Type>Update` (+ enum types)
- `repository.go` — `<Type>Repository` typed wrapper around `port.Repository`
- `service.go` — `<Type>Service` typed wrapper around `port.Service`
- `handler.go` — `<Type>Handler` with typed Routes()
- `convert.go` — JSON-based struct ↔ `map[string]any` helpers
- `overrides.go` — editable `<name>_overrides.go` hook stubs (opt-in via `--overrides`)

### Add a read-time schema-version migration

1. Define the migration in your consumer code or in the framework depending on scope.
2. Register it in the migration registry: see `docs/schema-lifecycle.md` and `pkg/core/schema/migration/` (if present in your tree).
3. Migrations run automatically on repository read when `doc.schema_version != registry.CurrentVersion(name)`. Write path is unchanged — documents are always written at the current version.

### Write a test

- **Unit tests** live beside the code. Table-driven. Use `testify/require` for setup and `testify/assert` for assertions.
- **Integration tests** carry `//go:build integration` and hit real dependencies (Mongo via testcontainers, SQL via in-memory SQLite where possible).
- **HTTP-layer tests** use `pkg/testing/sdtest.TestEngine` — it wires a real engine with the in-memory repository so hooks are exercised end-to-end.
- Run: `make test` (unit only), `make test-int` (integration).

## Files that almost always need updating together

| When you change... | Also update... |
|---|---|
| `pkg/core/port/repository.go` | Every adapter in `pkg/adapter/driven/*/repository.go` and `pkg/testing/sdtest/memory_repository.go` |
| `pkg/core/service/` service surface | The typed service template in `pkg/codegen/service.go` and the service tests |
| Envelope schema (`pkg/core/schema/types.go`) | `docs/schema-envelopes.md`, `pkg/core/schema/validation.go`, and any CLI that parses envelopes |
| CLI flag on `run` / `generate` / `migrate` | `docs/getting-started.md` and the relevant docs/*.md that documents the command |
| New `engine.Option` | `docs/embedding.md` (list of extension points) and `examples/petstore/main.go` if it's important enough to demo |

## Lint & test invariants the CI will fail on

- Every error return must be checked. Use `_ = f.Close()` in deferred `Close()` calls and `_, _ = fmt.Fprintf(os.Stdout, ...)` for CLI output.
- No unused vars/fields/imports/functions.
- `gofmt -w .` clean before commit. No exceptions.
- No circular imports. `core/**` must not import `adapter/**`.
- `go vet` + `staticcheck` must be clean.

## Starting points for common PR types

- **Bug fix in the CRUD pipeline**: look first at `pkg/core/service/crud.go` (the event-firing wrapper) and the resolve functions in `pkg/core/registry/resolution.go`.
- **Storage-layer bug**: `pkg/adapter/driven/mongo/repository.go` or `pkg/adapter/driven/sql/repository.go`. Both implement `port.Repository` with append-only versioning.
- **REST serialisation / content negotiation**: `pkg/adapter/driving/rest/`. Middleware lives in `middleware/`.
- **GraphQL**: `pkg/adapter/driving/graphql/` and the GraphQL codegen in `pkg/codegen/graphql.go`.
- **Codegen output wrong**: `pkg/codegen/*_test.go` uses golden files. Regenerate with `go test ./pkg/codegen/... -update` if a test supports it, otherwise update the golden manually.

## Related reading

- [CLAUDE.md](CLAUDE.md) — conventions, agent personas, CI rules
- [docs/architecture.md](docs/architecture.md) — request/response flow through the hexagon
- [docs/embedding.md](docs/embedding.md) — stellar-drive as a library
- [docs/custom-endpoints.md](docs/custom-endpoints.md) — handler overrides, custom routes, middleware
- [docs/registry.md](docs/registry.md) — guards/validators/transforms end-to-end
- [docs/events.md](docs/events.md) — event bus semantics and ordering
- [examples/petstore/](examples/petstore/) — runnable reference consumer
