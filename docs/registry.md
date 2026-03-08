# Function Registry

The function registry allows you to register custom business logic -- guards, validators, transforms, and complete handler overrides -- keyed by schema name and operation. This enables per-schema, per-version, and per-channel customisation of the default CRUD behaviour without modifying the core service layer.

All registry types and the `FunctionRegistry` struct live in `pkg/core/registry/`.

## Overview

The registry stores four kinds of override functions:

| Kind | Type | Purpose | Pipeline Effect |
|------|------|---------|-----------------|
| **HandlerFunc** | `func(w http.ResponseWriter, r *http.Request)` | Complete HTTP handler override | Bypasses the entire service pipeline |
| **GuardFunc** | `func(ctx context.Context, r *http.Request) error` | Access control check | First error short-circuits; denies the request |
| **ValidatorFunc** | `func(ctx context.Context, schemaName string, data map[string]any) error` | Input validation before schema validation | Error aborts the operation |
| **TransformFunc** | `func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error)` | Data mutation/enrichment before repository | Returned map replaces original data |

Each override is registered for a specific schema name and operation, with an optional `Scope` that constrains matching to a version, channel, or both.

## Operations

The registry defines five CRUD operations:

| Constant | Value | HTTP Method + Route |
|----------|-------|---------------------|
| `registry.OpCreate` | `"create"` | `POST /{schema}/` |
| `registry.OpGet` | `"get"` | `GET /{schema}/{id}` |
| `registry.OpList` | `"list"` | `GET /{schema}/` |
| `registry.OpUpdate` | `"update"` | `PATCH /{schema}/{id}` |
| `registry.OpDelete` | `"delete"` | `DELETE /{schema}/{id}` |

## Scope and Specificity

Every registered function is paired with a `Scope` that qualifies when it applies:

```go
type Scope struct {
    Version string // Schema version constraint (empty = any version)
    Channel string // Transport channel constraint (empty = any channel)
}
```

When multiple registrations match a request, the registry resolves by **specificity scoring**. Higher scores take precedence:

| Specificity | Score | Version | Channel | Example |
|-------------|-------|---------|---------|---------|
| version + channel | 4 | set | set | v2.0.0 + rest |
| channel only | 3 | empty | set | any version + rest |
| version only | 2 | set | empty | v1.0.0 + any channel |
| universal | 1 | empty | empty | any version + any channel |

A scope that has a constraint which does **not** match the request is rejected entirely (score = -1) and is excluded from resolution.

### Specificity Example

```go
reg := registry.NewFunctionRegistry()

// Universal guard (score=1) -- fires for all schemas, versions, channels.
reg.RegisterGuard("pet", registry.OpCreate, registry.Scope{}, universalGuard)

// REST-only guard (score=3) -- fires only when channel is "rest".
reg.RegisterGuard("pet", registry.OpCreate, registry.Scope{Channel: "rest"}, restGuard)

// Version-specific guard (score=2) -- fires only for version "2.0.0".
reg.RegisterGuard("pet", registry.OpCreate, registry.Scope{Version: "2.0.0"}, v2Guard)

// Fully qualified guard (score=4) -- fires for version "2.0.0" via "rest".
reg.RegisterGuard("pet", registry.OpCreate, registry.Scope{Version: "2.0.0", Channel: "rest"}, v2RestGuard)
```

When resolving guards for a `pet` create via REST at version 2.0.0, all four guards match. They are returned ordered by decreasing specificity:

1. `v2RestGuard` (score 4)
2. `restGuard` (score 3)
3. `v2Guard` (score 2)
4. `universalGuard` (score 1)

## Construction

Create an empty registry:

```go
reg := registry.NewFunctionRegistry()
```

The registry is typically created once during engine startup and shared across the application.

## Registration Methods

### RegisterHandler

Registers a complete HTTP handler override for a schema and operation. When a handler resolves for a request, the default service pipeline is **bypassed entirely** -- no guards, validators, transforms, schema validation, or events fire.

```go
func (r *FunctionRegistry) RegisterHandler(
    schemaName string,
    op Operation,
    scope Scope,
    handler HandlerFunc,
)
```

Example:

```go
reg.RegisterHandler("pet", registry.OpList, registry.Scope{}, func(w http.ResponseWriter, r *http.Request) {
    // Complete custom implementation for listing pets.
    // The default CRUD pipeline is not invoked.
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"data": [], "total": 0}`))
})
```

### RegisterGuard

Registers an access control function. Multiple guards may be registered for the same schema and operation; they are all evaluated in specificity order and the **first error short-circuits** evaluation.

```go
func (r *FunctionRegistry) RegisterGuard(
    schemaName string,
    op Operation,
    scope Scope,
    guard GuardFunc,
)
```

Example:

```go
reg.RegisterGuard("pet", registry.OpDelete, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
    role := r.Header.Get("X-User-Role")
    if role != "admin" {
        return errors.Forbidden("only admins can delete pets")
    }
    return nil
})
```

### RegisterValidator

Registers a custom validation function that runs before JSON Schema validation. Multiple validators may be registered and are all executed in specificity order.

```go
func (r *FunctionRegistry) RegisterValidator(
    schemaName string,
    op Operation,
    scope Scope,
    validator ValidatorFunc,
)
```

Example:

```go
reg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
    name, _ := data["name"].(string)
    if len(name) < 2 {
        return errors.ValidationFailed(errors.ErrorDetail{
            Field:   "name",
            Message: "must be at least 2 characters",
            Code:    "min_length",
        })
    }
    return nil
})
```

### RegisterTransform

Registers a data mutation function that runs before the data is passed to the repository. The returned map **replaces** the original data. Returning nil is treated as an empty map.

```go
func (r *FunctionRegistry) RegisterTransform(
    schemaName string,
    op Operation,
    scope Scope,
    transform TransformFunc,
)
```

Example:

```go
reg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
    // Normalise the pet name to lowercase.
    if name, ok := data["name"].(string); ok {
        data["name"] = strings.ToLower(name)
    }
    // Add a default tag if none provided.
    if _, ok := data["tags"]; !ok {
        data["tags"] = []any{}
    }
    return data, nil
})
```

## Resolution Methods

Resolution methods retrieve the most appropriate overrides for a given schema, operation, version, and channel combination.

### ResolveHandler

Returns the single most specific matching handler. If no handler is registered, the second return value is `false`.

```go
handler, ok := reg.ResolveHandler("pet", registry.OpList, "1.0.0", "rest")
if ok {
    // Use the custom handler; skip default pipeline.
    handler(w, r)
    return
}
```

### ResolveGuards

Returns all matching guards ordered from most to least specific. All guards should be evaluated; the first error denies the request.

```go
guards := reg.ResolveGuards("pet", registry.OpCreate, "1.0.0", "rest")
for _, guard := range guards {
    if err := guard(ctx, r); err != nil {
        // Deny the request with the guard's error.
        return err
    }
}
```

### ResolveValidators

Returns all matching validators ordered from most to least specific.

```go
validators := reg.ResolveValidators("pet", registry.OpCreate, "1.0.0", "rest")
for _, validate := range validators {
    if err := validate(ctx, "pet", data); err != nil {
        return err
    }
}
```

### ResolveTransforms

Returns all matching transforms ordered from most to least specific. Each transform's output feeds into the next.

```go
transforms := reg.ResolveTransforms("pet", registry.OpCreate, "1.0.0", "rest")
for _, transform := range transforms {
    var err error
    data, err = transform(ctx, "pet", data)
    if err != nil {
        return err
    }
}
```

## Thread Safety

`FunctionRegistry` uses `sync.RWMutex` internally. All registration and resolution methods are safe for concurrent use. Registration acquires a write lock; resolution acquires a read lock.

## Execution Pipeline

When the service layer processes a request, the function registry is consulted in this order:

```
1. ResolveHandler   -- if found, bypass everything below; call handler directly
2. ResolveGuards    -- evaluate all; first error aborts
3. ResolveValidators -- evaluate all; first error aborts
4. Schema validation -- JSON Schema validation against the envelope
5. ResolveTransforms -- apply all; mutate data before persistence
6. Repository call   -- delegate to the storage adapter
```

This pipeline runs inside the `GenericCRUDService` for every CRUD operation.

## Complete Example

Putting it all together -- a pet API with admin-only deletes, name normalisation, and a custom list handler:

```go
package main

import (
    "context"
    "net/http"
    "strings"

    "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
    "github.com/digitally-rendered/stellar-drive/pkg/core/registry"
)

func registerPetOverrides(reg *registry.FunctionRegistry) {
    // Guard: only admins can delete pets.
    reg.RegisterGuard("pet", registry.OpDelete, registry.Scope{}, adminOnlyGuard)

    // Validator: pet names must be at least 2 characters.
    reg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, minNameLength(2))

    // Transform: normalise pet names to lowercase on create.
    reg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, normaliseName)

    // Handler: completely custom list implementation for v2.
    reg.RegisterHandler("pet", registry.OpList, registry.Scope{Version: "2.0.0"}, customPetList)
}

func adminOnlyGuard(ctx context.Context, r *http.Request) error {
    if r.Header.Get("X-User-Role") != "admin" {
        return errors.Forbidden("only admins can delete pets")
    }
    return nil
}

func minNameLength(min int) registry.ValidatorFunc {
    return func(ctx context.Context, schemaName string, data map[string]any) error {
        name, _ := data["name"].(string)
        if len(name) < min {
            return errors.ValidationFailed(errors.ErrorDetail{
                Field:   "name",
                Message: fmt.Sprintf("must be at least %d characters", min),
                Code:    "min_length",
            })
        }
        return nil
    }
}

func normaliseName(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
    if name, ok := data["name"].(string); ok {
        data["name"] = strings.ToLower(name)
    }
    return data, nil
}

func customPetList(w http.ResponseWriter, r *http.Request) {
    // Custom v2 list implementation with different response format.
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"pets": [], "count": 0, "api_version": "2.0.0"}`))
}
```

## Related Documentation

- **[Architecture](architecture.md)** -- Where the registry fits in the hexagonal design
- **[Events](events.md)** -- Event handlers complement registry functions (events fire after guards/validators)
- **[Container](container.md)** -- DI container for repositories, services, and HTTP handlers
- **[Errors](errors.md)** -- Domain errors returned by guards and validators
