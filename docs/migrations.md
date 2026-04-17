# Schema Migrations

Stellar-drive splits "migrations" into two layers. Make sure you know which one you need before wiring anything up.

| Layer | What it moves | CLI / package |
|---|---|---|
| **Storage migrations** | Tables, indexes, collections, DB-level constructs | `stellar migrate` / `pkg/adapter/driven/sql` |
| **Schema-version migrations** | Document payloads: renamed fields, new defaults, restructured data | `WithMigrations(...)` / `pkg/core/schema/migration` |

This guide is about the second layer.

## Why read-time migration?

Every `*model.Document` persisted by stellar-drive carries the `schema_version` it was written at. When the schema evolves — you rename `name` to `first_name`, add a required `region` default, split `address` into structured parts — existing documents on disk still hold the old shape. Rewriting the whole collection on every schema change is expensive and fragile.

Instead, register a per-(from, to) migration function. On read the framework checks the document's `schema_version` against the current version in the schema registry and walks the chain of registered steps, returning the document in the current shape. Writes always happen at the current version, so new data never accumulates on the old shape.

## Registering migrations

```go
import (
    "context"
    "github.com/digitally-rendered/stellar-drive/pkg/core/schema/migration"
    "github.com/digitally-rendered/stellar-drive/pkg/engine"
)

migrations := migration.NewRegistry()

// Pet v1 -> v2: split "name" into "first_name" + "last_name".
_ = migrations.Register("pet", "1.0.0", "2.0.0",
    func(_ context.Context, d map[string]any) (map[string]any, error) {
        if full, ok := d["name"].(string); ok {
            parts := strings.SplitN(full, " ", 2)
            d["first_name"] = parts[0]
            if len(parts) == 2 {
                d["last_name"] = parts[1]
            }
            delete(d, "name")
        }
        return d, nil
    })

// Pet v2 -> v3: add a default region.
_ = migrations.Register("pet", "2.0.0", "3.0.0",
    func(_ context.Context, d map[string]any) (map[string]any, error) {
        if _, ok := d["region"]; !ok {
            d["region"] = "unknown"
        }
        return d, nil
    })

eng := engine.New(cfg, engine.WithMigrations(migrations))
```

That's the whole wiring. Every read through the service layer applies the right chain; writes continue to be at the current version.

## Chain resolution

`Registry.Chain(schemaName, from, to)` walks the graph of registered edges. At every node it prefers the **largest step that does not overshoot** the target, so registering both a small and a large jump is safe:

```go
migrations.Register("pet", "1.0.0", "1.1.0", small)
migrations.Register("pet", "1.0.0", "2.0.0", bigJump) // preferred when target is 2.0.0
```

When both source and target parse as semver, semver ordering is used; otherwise the registry falls back to lexicographic comparison. Cycles are detected and rejected.

## Guarantees and limits

- Migrations run **on read only**. The write path still validates against the current schema version.
- When no migration is registered for a schema the decorator is a cheap pass-through.
- When a migration function returns an error the caller sees the error and `SchemaVersion` is **not** updated, so a retry of the same read re-runs the migration. Don't swallow errors inside a migration function unless you're certain of what you're covering.
- Migrations are applied to the document's `Data` map. Audit fields (`id`, `record_version`, `created_at`, etc.) are untouched.
- Bulk reads (`List`) migrate every item independently. There is no cross-item coordination.

## When not to use read-time migration

Use a storage-layer migration (or a one-off rewrite job) when:

- The new shape cannot be derived from the old payload without external data (you'd need a join).
- The old payload cannot be interpreted correctly at all (corruption, not evolution).
- Query filters depend on the new field layout (you can't filter on a field that exists only after migration — filter before migrating on the way out).

The read-time path is there for the common case of field-level schema evolution. Anything bigger than that deserves a proper data migration plan.

## Compacting at rest

When the migration chain grows long you'll want to rewrite old documents at the current version so future reads skip the chain entirely. That is not implemented in the framework today; `Registry.Apply` is the primitive you'd use to build it. A minimal compact loop:

```go
func Compact(ctx context.Context, repo port.Repository, reg *migration.Registry, schemaName, targetVersion string) error {
    result, err := repo.List(ctx, schemaName, nil)
    if err != nil {
        return err
    }
    for _, doc := range result.Items {
        if doc.SchemaVersion == targetVersion {
            continue
        }
        if err := reg.Apply(ctx, doc, targetVersion); err != nil {
            return err
        }
        if _, err := repo.Update(ctx, schemaName, doc.EntityID, doc.Data); err != nil {
            return err
        }
    }
    return nil
}
```

This trades a write amplification spike for a steady-state read path with no migration work. Run it during a maintenance window, not under load.

## Related docs

- [Schema Lifecycle](schema-lifecycle.md) — how envelopes and versions move through the registry
- [Embedding](embedding.md) — `engine.WithMigrations` in context with the other options
