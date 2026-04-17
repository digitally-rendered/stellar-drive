# Petstore — stellar-drive reference example

A runnable end-to-end project showing how to embed `stellar-drive` as a library.

It boots the classic OpenAPI Petstore (pet, order, user, category, tag) as
versioned JSON-Schema-driven REST + GraphQL resources, and wires up every
extension point the framework exposes:

- **Guards** (`adminOnlyGuard` on pet delete)
- **Validators** (`minNameLengthValidator` on pet create/update)
- **Transforms** (`normaliseNameTransform` trims + lowercases names)
- **Event subscribers** (post-create, post-delete)
- **Custom middleware** (`requestCountingMiddleware`)

Look at `main.go` — every feature is wired explicitly, nothing hidden in
config. This is the shape of a real stellar-drive consumer.

## Running

Requires a MongoDB reachable at the URI in `stellar.yaml` (defaults to
`mongodb://localhost:27017`).

```bash
cd examples/petstore
go run .
```

Then:

```bash
# List pets (empty on first run).
curl http://127.0.0.1:8080/api/v1/pets

# Create a pet (name gets trimmed + lowercased by the transform).
curl -X POST http://127.0.0.1:8080/api/v1/pets \
  -H 'Content-Type: application/json' \
  -d '{"name":"  Rex  ","photo_urls":["https://example.com/rex.jpg"]}'

# Delete requires the admin role — this fails:
curl -X DELETE http://127.0.0.1:8080/api/v1/pets/<entity_id>

# This succeeds:
curl -X DELETE -H 'X-Role: admin' http://127.0.0.1:8080/api/v1/pets/<entity_id>
```

GraphQL is enabled at `/graphql`. Schema management lives under
`/api/v1/_schemas`.

## Optional: typed codegen

To generate typed Go wrappers (repository, service, handler, models) alongside
the runtime-loaded schemas:

```bash
stellar generate --schemas ./schemas --output ./generated
```

Or with editable overrides stubs:

```bash
stellar generate --schemas ./schemas --output ./generated --overrides
```

Each stub file (`generated/<schema>/<schema>_overrides.go`) ships with empty
guard / validator / transform / handler functions that you fill in and register
via the generated `Register<Type>Overrides(reg)` helper. Existing stubs are
preserved across regeneration.
